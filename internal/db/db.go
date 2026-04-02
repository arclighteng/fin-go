package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/arclighteng/fin-go/internal/audit"
	"github.com/arclighteng/fin-go/internal/closebooks"
	"github.com/arclighteng/fin-go/internal/models"
	"github.com/arclighteng/fin-go/internal/reconciliation"
	_ "modernc.org/sqlite"
)

// DB wraps a sql.DB with fin-specific operations.
type DB struct {
	db *sql.DB
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

// Underlying returns the raw *sql.DB for callers that require direct access
// (e.g., sub-module schema initializers). Prefer wrapper methods where possible.
func (d *DB) Underlying() *sql.DB {
	return d.db
}

// Connect opens (or creates) a SQLite database at the given path.
func Connect(dbPath string) (*DB, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	isNew := false
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		isNew = true
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Single connection for SQLite
	db.SetMaxOpenConns(1)

	// Harden file permissions on new databases (Unix only)
	if isNew && runtime.GOOS != "windows" {
		os.Chmod(dbPath, 0600)
	}

	return &DB{db: db}, nil
}

// Init creates all tables, runs migrations, and initializes sub-module schemas.
func (d *DB) Init() error {
	if _, err := d.db.Exec(schemaDDL); err != nil {
		return fmt.Errorf("init schema: %w", err)
	}

	// Migrations: each migration is tracked in schema_versions so it runs
	// exactly once, regardless of how many times Init is called.
	migrations := []struct {
		version int
		sql     string
	}{
		{1, "ALTER TABLE transactions ADD COLUMN pending INTEGER NOT NULL DEFAULT 0"},
		{2, "ALTER TABLE commitments ADD COLUMN direction TEXT NOT NULL DEFAULT 'expense'"},
	}

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	defer tx.Rollback()

	for _, m := range migrations {
		var count int
		if err := tx.QueryRow("SELECT COUNT(*) FROM schema_versions WHERE version = ?", m.version).Scan(&count); err != nil {
			return fmt.Errorf("check migration version %d: %w", m.version, err)
		}
		if count > 0 {
			continue // already applied
		}
		if _, execErr := tx.Exec(m.sql); execErr != nil {
			// SQLite does not support ADD COLUMN IF NOT EXISTS. A column that
			// already exists (because the DDL was updated) causes a "duplicate
			// column name" error. Treat that as a no-op so that fresh in-memory
			// databases (used in tests) and upgraded production databases both
			// work without special-casing.
			errMsg := execErr.Error()
			if strings.Contains(errMsg, "duplicate column name") {
				// Column already present — mark the migration as applied and continue.
			} else {
				return fmt.Errorf("apply migration %d: %w", m.version, execErr)
			}
		}
		if _, err := tx.Exec(
			"INSERT INTO schema_versions(version, applied_at) VALUES (?, ?)",
			m.version, utcNowISO(),
		); err != nil {
			return fmt.Errorf("record migration %d: %w", m.version, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}

	// Initialize sub-module schemas (idempotent CREATE IF NOT EXISTS statements).
	if err := ensureAuditSchema(d.db); err != nil {
		return fmt.Errorf("audit schema: %w", err)
	}
	if err := ensureClosebooksSchema(d.db); err != nil {
		return fmt.Errorf("closebooks schema: %w", err)
	}
	if err := ensureReconciliationSchema(d.db); err != nil {
		return fmt.Errorf("reconciliation schema: %w", err)
	}

	return nil
}

func utcNowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// UpsertAccounts inserts or updates accounts.
func (d *DB) UpsertAccounts(accounts []models.Account) error {
	now := utcNowISO()
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO accounts(account_id, institution, name, type, currency, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id) DO UPDATE SET
		  institution=excluded.institution,
		  name=excluded.name,
		  type=excluded.type,
		  currency=excluded.currency,
		  last_seen_at=excluded.last_seen_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, a := range accounts {
		_, err := stmt.Exec(a.AccountID, a.Institution, a.Name, a.Type, a.Currency, now)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// UpsertTransactions inserts or updates transactions. Returns (inserted, updated).
func (d *DB) UpsertTransactions(txns []models.Transaction) (int, int, error) {
	now := utcNowISO()
	inserted, updated := 0, 0

	dbTx, err := d.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer dbTx.Rollback()

	for _, t := range txns {
		pendingInt := 0
		if t.Pending {
			pendingInt = 1
		}
		postedAt := t.PostedAt.Format("2006-01-02")

		if t.SourceTxnID != "" {
			// Try update first
			res, err := dbTx.Exec(`
				UPDATE transactions
				SET posted_at=?, amount_cents=?, currency=?, description=?, merchant=?,
				    fingerprint=?, pending=?, updated_at=?
				WHERE account_id=? AND source_txn_id=?
				AND (
				    posted_at <> ?
				    OR amount_cents <> ?
				    OR currency <> ?
				    OR COALESCE(description, '') <> COALESCE(?, '')
				    OR COALESCE(merchant, '') <> COALESCE(?, '')
				    OR fingerprint <> ?
				    OR pending <> ?
				)`,
				postedAt, t.AmountCents, t.Currency, t.Description, t.Merchant,
				t.Fingerprint, pendingInt, now, t.AccountID, t.SourceTxnID,
				postedAt, t.AmountCents, t.Currency, t.Description, t.Merchant, t.Fingerprint, pendingInt,
			)
			if err != nil {
				return 0, 0, err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				updated++
				continue
			}

			// Insert if not updated
			res, err = dbTx.Exec(`
				INSERT OR IGNORE INTO transactions(
				  account_id, posted_at, amount_cents, currency, description, merchant,
				  source_txn_id, fingerprint, pending, created_at, updated_at
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				t.AccountID, postedAt, t.AmountCents, t.Currency,
				t.Description, t.Merchant, t.SourceTxnID, t.Fingerprint, pendingInt, now, now,
			)
			if err != nil {
				return 0, 0, err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				inserted++
			}
		} else {
			res, err := dbTx.Exec(`
				INSERT OR IGNORE INTO transactions(
				  account_id, posted_at, amount_cents, currency, description, merchant,
				  source_txn_id, fingerprint, pending, created_at, updated_at
				) VALUES (?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?)`,
				t.AccountID, postedAt, t.AmountCents, t.Currency,
				t.Description, t.Merchant, t.Fingerprint, pendingInt, now, now,
			)
			if err != nil {
				return 0, 0, err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				inserted++
			}
		}
	}

	if err := dbTx.Commit(); err != nil {
		return 0, 0, err
	}
	return inserted, updated, nil
}

// RecordRun logs a sync run.
func (d *DB) RecordRun(lookbackDays, fetched, ins, upd int) error {
	_, err := d.db.Exec(
		"INSERT INTO runs(ran_at, lookback_days, txns_fetched, txns_inserted, txns_updated) VALUES (?, ?, ?, ?, ?)",
		utcNowISO(), lookbackDays, fetched, ins, upd,
	)
	return err
}

// GetAccounts returns all accounts.
func (d *DB) GetAccounts() ([]models.Account, error) {
	rows, err := d.db.Query("SELECT account_id, institution, name, type, currency FROM accounts ORDER BY institution, name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []models.Account
	for rows.Next() {
		var a models.Account
		var typ sql.NullString
		if err := rows.Scan(&a.AccountID, &a.Institution, &a.Name, &typ, &a.Currency); err != nil {
			return nil, err
		}
		a.Type = typ.String
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

// GetTransactions returns transactions in a date range [start, endExclusive).
func (d *DB) GetTransactions(start, endExclusive time.Time) ([]models.Transaction, error) {
	rows, err := d.db.Query(`
		SELECT account_id, posted_at, amount_cents, currency, description, merchant,
		       source_txn_id, fingerprint, pending
		FROM transactions
		WHERE posted_at >= ? AND posted_at < ?
		ORDER BY posted_at DESC`,
		start.Format("2006-01-02"), endExclusive.Format("2006-01-02"),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanTransactions(rows)
}

// RecentRuns returns the most recent sync runs.
func (d *DB) RecentRuns(limit int) ([]models.SyncRun, error) {
	rows, err := d.db.Query(
		"SELECT id, ran_at, lookback_days, txns_fetched, txns_inserted, txns_updated FROM runs ORDER BY ran_at DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []models.SyncRun
	for rows.Next() {
		var r models.SyncRun
		var ranAt string
		if err := rows.Scan(&r.ID, &ranAt, &r.LookbackDays, &r.TxnsFetched, &r.TxnsInserted, &r.TxnsUpdated); err != nil {
			return nil, err
		}
		r.RanAt, _ = time.Parse(time.RFC3339, ranAt)
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// RunsInLast24Hours counts sync runs in the last 24 hours.
func (d *DB) RunsInLast24Hours() (int, error) {
	cutoff := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM runs WHERE ran_at >= ?", cutoff).Scan(&count)
	return count, err
}

// SaveAlertAction upserts an alert action.
func (d *DB) SaveAlertAction(aa models.AlertAction) error {
	now := utcNowISO()
	_, err := d.db.Exec(`
		INSERT INTO alert_actions(alert_key, action, merchant_norm, pattern_type, notes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(alert_key) DO UPDATE SET
		  action=excluded.action,
		  notes=excluded.notes,
		  updated_at=excluded.updated_at`,
		aa.AlertKey, aa.Action, aa.MerchantNorm, aa.PatternType, aa.Notes, now, now,
	)
	return err
}

// GetAlertActions returns all alert actions as a map.
func (d *DB) GetAlertActions() (map[string]string, error) {
	rows, err := d.db.Query("SELECT alert_key, action FROM alert_actions")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var key, action string
		if err := rows.Scan(&key, &action); err != nil {
			return nil, err
		}
		result[key] = action
	}
	return result, rows.Err()
}

func scanTransactions(rows *sql.Rows) ([]models.Transaction, error) {
	var txns []models.Transaction
	for rows.Next() {
		var t models.Transaction
		var postedAt string
		var desc, merchant, srcTxnID sql.NullString
		var pending int
		if err := rows.Scan(&t.AccountID, &postedAt, &t.AmountCents, &t.Currency,
			&desc, &merchant, &srcTxnID, &t.Fingerprint, &pending); err != nil {
			return nil, err
		}
		t.PostedAt, _ = time.Parse("2006-01-02", postedAt)
		t.Description = desc.String
		t.Merchant = merchant.String
		t.SourceTxnID = srcTxnID.String
		t.Pending = pending == 1
		txns = append(txns, t)
	}
	return txns, rows.Err()
}

// ---------------------------------------------------------------------------
// sql.DB pass-through methods
//
// These allow server-layer code to issue ad-hoc queries without reaching into
// the unexported db field directly. They mirror the sql.DB signatures exactly.
// ---------------------------------------------------------------------------

// Query executes a query that returns rows.
func (d *DB) Query(query string, args ...any) (*sql.Rows, error) {
	return d.db.Query(query, args...)
}

// QueryRow executes a query that returns at most one row.
func (d *DB) QueryRow(query string, args ...any) *sql.Row {
	return d.db.QueryRow(query, args...)
}

// Exec executes a query that doesn't return rows.
func (d *DB) Exec(query string, args ...any) (sql.Result, error) {
	return d.db.Exec(query, args...)
}

// ---------------------------------------------------------------------------
// Sub-module schema bridges
// ---------------------------------------------------------------------------

// ensureAuditSchema delegates to the audit package's EnsureSchema.
func ensureAuditSchema(sqlDB *sql.DB) error {
	return audit.EnsureSchema(sqlDB)
}

// ensureClosebooksSchema delegates to the closebooks package's EnsureSchema.
func ensureClosebooksSchema(sqlDB *sql.DB) error {
	return closebooks.EnsureSchema(sqlDB)
}

// ensureReconciliationSchema delegates to the reconciliation package's EnsureSchema.
func ensureReconciliationSchema(sqlDB *sql.DB) error {
	return reconciliation.EnsureSchema(sqlDB)
}
