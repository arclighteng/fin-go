// Package reconciliation compares running account balances against official
// bank statement totals and records the results.
//
// TRUTH CONTRACT:
//   - The calculated balance is the sum of all non-pending transactions for
//     an account up to and including the statement date.
//   - A reconciliation is "matched" when |delta| ≤ 100 cents ($1.00 tolerance).
//   - Discrepancies are stored for investigation; they are never silently dropped.
package reconciliation

import (
	"database/sql"
	"fmt"
	"math"
	"time"
)

// Status describes the resolution state of a reconciliation event.
type Status string

const (
	StatusMatched     Status = "matched"
	StatusDiscrepancy Status = "discrepancy"
	StatusPending     Status = "pending"
	StatusResolved    Status = "resolved"
)

// matchedThresholdCents is the tolerance below which a reconciliation is
// considered matched ($1.00).
const matchedThresholdCents = 100

// Event is a single reconciliation record for an account at a statement date.
type Event struct {
	ID                      int64
	AccountID               string
	StatementDate           time.Time
	StatementBalanceCents   int64
	CalculatedBalanceCents  int64
	DeltaCents              int64 // StatementBalance - CalculatedBalance
	Status                  Status
	Notes                   string
	CreatedAt               time.Time
	ResolvedAt              *time.Time
}

// IsMatched reports whether the delta is within the $1 tolerance.
func (e *Event) IsMatched() bool {
	d := e.DeltaCents
	if d < 0 {
		d = -d
	}
	return d <= matchedThresholdCents
}

// Result is the computed reconciliation for a single account, returned
// before the event is persisted.
type Result struct {
	AccountID              string
	AccountName            string
	StatementDate          time.Time
	StatementBalanceCents  int64
	CalculatedBalanceCents int64
	DeltaCents             int64
	TransactionCount       int
	FirstTransactionDate   *time.Time
	LastTransactionDate    *time.Time
}

// IsMatched reports whether the result delta is within tolerance.
func (r *Result) IsMatched() bool {
	d := r.DeltaCents
	if d < 0 {
		d = -d
	}
	return d <= matchedThresholdCents
}

// DeltaDirection returns a human-readable label for the delta direction.
func (r *Result) DeltaDirection() string {
	switch {
	case r.DeltaCents > 0:
		return "missing_income" // statement shows more than we computed
	case r.DeltaCents < 0:
		return "missing_expense" // statement shows less than we computed
	default:
		return "balanced"
	}
}

// Pattern is a detected pattern in reconciliation history for an account.
type Pattern struct {
	AccountID    string
	AccountName  string
	PatternType  string // "consistent_delta", "growing_delta"
	AvgDelta     int64
	DeltaCount   int
	Confidence   float64
	Suggestion   string
}

// Insight aggregates patterns and suggestions across all accounts.
type Insight struct {
	Patterns              []Pattern
	AccountsWithIssues    int
	TotalUnresolvedDelta  int64
	Suggestions           []string
}

// Reconcile computes the running balance for accountID up to asOf and
// returns a Result. The result is not persisted; call SaveEvent to store it.
func Reconcile(db *sql.DB, accountID string, statementBalanceCents int64, asOf time.Time) (*Result, error) {
	// Look up account name.
	var accountName string
	err := db.QueryRow("SELECT name FROM accounts WHERE account_id = ?", accountID).Scan(&accountName)
	if err == sql.ErrNoRows {
		accountName = accountID
	} else if err != nil {
		return nil, fmt.Errorf("reconciliation: look up account %s: %w", accountID, err)
	}

	// Compute sum, count, and date bounds.
	calcBalance, txnCount, firstDate, lastDate, err := computeBalance(db, accountID, asOf)
	if err != nil {
		return nil, err
	}

	return &Result{
		AccountID:              accountID,
		AccountName:            accountName,
		StatementDate:          asOf,
		StatementBalanceCents:  statementBalanceCents,
		CalculatedBalanceCents: calcBalance,
		DeltaCents:             statementBalanceCents - calcBalance,
		TransactionCount:       txnCount,
		FirstTransactionDate:   firstDate,
		LastTransactionDate:    lastDate,
	}, nil
}

// SaveEvent persists a reconciliation result and returns the stored event.
// If a record already exists for (accountID, statementDate) it is updated.
func SaveEvent(db *sql.DB, result *Result, notes string) (*Event, error) {
	if err := EnsureSchema(db); err != nil {
		return nil, err
	}

	status := StatusDiscrepancy
	if result.IsMatched() {
		status = StatusMatched
	}
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := db.Exec(`
		INSERT INTO reconciliation_events
			(account_id, statement_date, statement_balance_cents, calculated_balance_cents,
			 delta_cents, status, notes, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id, statement_date) DO UPDATE SET
			statement_balance_cents  = excluded.statement_balance_cents,
			calculated_balance_cents = excluded.calculated_balance_cents,
			delta_cents              = excluded.delta_cents,
			status                   = excluded.status,
			notes                    = COALESCE(excluded.notes, notes)`,
		result.AccountID,
		result.StatementDate.Format("2006-01-02"),
		result.StatementBalanceCents,
		result.CalculatedBalanceCents,
		result.DeltaCents,
		string(status),
		nullableString(notes),
		now,
	)
	if err != nil {
		return nil, fmt.Errorf("reconciliation: upsert event: %w", err)
	}

	// Re-fetch to get the authoritative DB row.
	row := db.QueryRow(`
		SELECT id, account_id, statement_date, statement_balance_cents,
		       calculated_balance_cents, delta_cents, status,
		       COALESCE(notes,''), created_at, resolved_at
		FROM reconciliation_events
		WHERE account_id = ? AND statement_date = ?`,
		result.AccountID, result.StatementDate.Format("2006-01-02"),
	)
	return scanEvent(row)
}

// ResolveEvent marks a reconciliation discrepancy as resolved.
func ResolveEvent(db *sql.DB, accountID string, statementDate time.Time, notes string) error {
	if err := EnsureSchema(db); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
		UPDATE reconciliation_events
		SET status = ?, resolved_at = ?, notes = COALESCE(?, notes)
		WHERE account_id = ? AND statement_date = ?`,
		string(StatusResolved), now, nullableString(notes),
		accountID, statementDate.Format("2006-01-02"),
	)
	if err != nil {
		return fmt.Errorf("reconciliation: resolve event: %w", err)
	}
	return nil
}

// ListHistory returns reconciliation history, optionally filtered to one
// account. Results are ordered by statement date descending.
func ListHistory(db *sql.DB, accountID string, limit int) ([]Event, error) {
	if err := EnsureSchema(db); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}

	var rows *sql.Rows
	var err error
	if accountID != "" {
		rows, err = db.Query(`
			SELECT id, account_id, statement_date, statement_balance_cents,
			       calculated_balance_cents, delta_cents, status,
			       COALESCE(notes,''), created_at, resolved_at
			FROM reconciliation_events
			WHERE account_id = ?
			ORDER BY statement_date DESC
			LIMIT ?`,
			accountID, limit,
		)
	} else {
		rows, err = db.Query(`
			SELECT id, account_id, statement_date, statement_balance_cents,
			       calculated_balance_cents, delta_cents, status,
			       COALESCE(notes,''), created_at, resolved_at
			FROM reconciliation_events
			ORDER BY statement_date DESC
			LIMIT ?`,
			limit,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("reconciliation: list history: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		e, err := scanEventRow(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// ListPendingDiscrepancies returns all unresolved discrepancy events, ordered
// by absolute delta magnitude (largest first).
func ListPendingDiscrepancies(db *sql.DB) ([]Event, error) {
	if err := EnsureSchema(db); err != nil {
		return nil, err
	}

	rows, err := db.Query(`
		SELECT id, account_id, statement_date, statement_balance_cents,
		       calculated_balance_cents, delta_cents, status,
		       COALESCE(notes,''), created_at, resolved_at
		FROM reconciliation_events
		WHERE status = ?
		ORDER BY ABS(delta_cents) DESC`,
		string(StatusDiscrepancy),
	)
	if err != nil {
		return nil, fmt.Errorf("reconciliation: list discrepancies: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		e, err := scanEventRow(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// AnalyzePatterns examines reconciliation history and detects systematic
// patterns (consistent or growing deltas) that suggest a structural problem.
func AnalyzePatterns(db *sql.DB, minEvents int) (*Insight, error) {
	if err := EnsureSchema(db); err != nil {
		return nil, err
	}
	if minEvents <= 0 {
		minEvents = 3
	}

	rows, err := db.Query(`
		SELECT r.account_id,
		       COALESCE(a.name, r.account_id) AS account_name,
		       r.statement_date,
		       r.delta_cents,
		       r.status
		FROM reconciliation_events r
		LEFT JOIN accounts a ON r.account_id = a.account_id
		ORDER BY r.account_id, r.statement_date`)
	if err != nil {
		return nil, fmt.Errorf("reconciliation: analyze patterns query: %w", err)
	}
	defer rows.Close()

	type eventSummary struct {
		date   string
		delta  int64
		status string
	}
	byAccount := make(map[string][]eventSummary)
	nameFor := make(map[string]string)

	for rows.Next() {
		var aid, name, dt, status string
		var delta int64
		if err := rows.Scan(&aid, &name, &dt, &delta, &status); err != nil {
			return nil, fmt.Errorf("reconciliation: scan pattern row: %w", err)
		}
		byAccount[aid] = append(byAccount[aid], eventSummary{dt, delta, status})
		nameFor[aid] = name
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reconciliation: iterate pattern rows: %w", err)
	}

	var patterns []Pattern
	var suggestions []string
	accountsWithIssues := 0
	var totalUnresolved int64

	for aid, events := range byAccount {
		if len(events) < minEvents {
			continue
		}

		deltas := make([]int64, len(events))
		for i, e := range events {
			deltas[i] = e.delta
		}

		unresolved := 0
		for _, e := range events {
			if e.status == string(StatusDiscrepancy) {
				unresolved++
				d := e.delta
				if d < 0 {
					d = -d
				}
				totalUnresolved += d
			}
		}
		if unresolved > 0 {
			accountsWithIssues++
		}

		// Detect consistent-delta pattern.
		if len(deltas) >= minEvents {
			var sum int64
			for _, d := range deltas {
				sum += d
			}
			avg := sum / int64(len(deltas))
			allPos := true
			allNeg := true
			for _, d := range deltas {
				if d <= 0 {
					allPos = false
				}
				if d >= 0 {
					allNeg = false
				}
			}

			if (allPos || allNeg) && absInt64(avg) > matchedThresholdCents {
				mean := float64(absInt64(avg))
				var variance float64
				for _, d := range deltas {
					diff := float64(absInt64(d)) - mean
					variance += diff * diff
				}
				variance /= float64(len(deltas))
				stdDev := math.Sqrt(variance)
				cv := stdDev / mean

				if cv < 0.3 {
					direction := "higher"
					missing := "income"
					if avg < 0 {
						direction = "lower"
						missing = "expenses"
					}
					patterns = append(patterns, Pattern{
						AccountID:   aid,
						AccountName: nameFor[aid],
						PatternType: "consistent_delta",
						AvgDelta:    avg,
						DeltaCount:  len(deltas),
						Confidence:  1.0 - cv,
						Suggestion: fmt.Sprintf(
							"Statement consistently %s than calculated by $%.2f. Check for missing %s.",
							direction, float64(absInt64(avg))/100.0, missing,
						),
					})
				}
			}
		}

		// Detect growing-delta pattern.
		if len(deltas) >= minEvents {
			recentSlice := deltas
			if len(deltas) > 3 {
				recentSlice = deltas[len(deltas)-3:]
			}
			olderSlice := deltas[:len(deltas)-len(recentSlice)]

			avgRecent := avgAbs(recentSlice)
			avgOlder := avgAbs(olderSlice)

			if avgOlder > 0 && avgRecent > avgOlder*1.5 {
				patterns = append(patterns, Pattern{
					AccountID:   aid,
					AccountName: nameFor[aid],
					PatternType: "growing_delta",
					AvgDelta:    int64(avgRecent),
					DeltaCount:  len(deltas),
					Confidence:  0.7,
					Suggestion: fmt.Sprintf(
						"Discrepancy growing over time ($%.2f → $%.2f). May indicate systematic missing transactions.",
						avgOlder/100.0, avgRecent/100.0,
					),
				})
			}
		}
	}

	if accountsWithIssues > 0 {
		suggestions = append(suggestions, fmt.Sprintf(
			"%d account(s) have unresolved discrepancies totaling $%.2f.",
			accountsWithIssues, float64(totalUnresolved)/100.0,
		))
	}
	consistent := 0
	growing := 0
	for _, p := range patterns {
		switch p.PatternType {
		case "consistent_delta":
			consistent++
		case "growing_delta":
			growing++
		}
	}
	if consistent > 0 {
		suggestions = append(suggestions, fmt.Sprintf(
			"%d account(s) show consistent discrepancy patterns. Consider reviewing transaction sources.",
			consistent,
		))
	}
	if growing > 0 {
		suggestions = append(suggestions, fmt.Sprintf(
			"%d account(s) have growing discrepancies. Investigate recent transaction sync issues.",
			growing,
		))
	}

	return &Insight{
		Patterns:             patterns,
		AccountsWithIssues:   accountsWithIssues,
		TotalUnresolvedDelta: totalUnresolved,
		Suggestions:          suggestions,
	}, nil
}

// MissingTransactionCandidates finds transactions near the statement date whose
// amount is close to |deltaCents| and could explain the discrepancy.
func MissingTransactionCandidates(
	db *sql.DB,
	accountID string,
	statementDate time.Time,
	deltaCents int64,
	tolerancePercent float64,
) ([]map[string]any, error) {
	if tolerancePercent <= 0 {
		tolerancePercent = 20.0
	}
	target := deltaCents
	if target < 0 {
		target = -target
	}
	tol := int64(float64(target) * tolerancePercent / 100.0)
	minAmt := target - tol
	maxAmt := target + tol

	startDate := statementDate.AddDate(0, 0, -30).Format("2006-01-02")
	endDate := statementDate.Format("2006-01-02")

	var amountClause string
	if deltaCents > 0 {
		amountClause = "amount_cents > 0"
	} else {
		amountClause = "amount_cents < 0"
	}

	// Note: amountClause is built from a constant, not user input — safe.
	query := fmt.Sprintf(`
		SELECT posted_at, amount_cents,
		       COALESCE(merchant, description, '') AS payee,
		       fingerprint
		FROM transactions
		WHERE account_id = ?
		  AND posted_at >= ? AND posted_at <= ?
		  AND %s
		  AND ABS(amount_cents) BETWEEN ? AND ?
		ORDER BY ABS(ABS(amount_cents) - ?) ASC
		LIMIT 10`, amountClause)

	rows, err := db.Query(query, accountID, startDate, endDate, minAmt, maxAmt, target)
	if err != nil {
		return nil, fmt.Errorf("reconciliation: missing candidates query: %w", err)
	}
	defer rows.Close()

	var candidates []map[string]any
	for rows.Next() {
		var postedAt, payee, fp string
		var amount int64
		if err := rows.Scan(&postedAt, &amount, &payee, &fp); err != nil {
			return nil, fmt.Errorf("reconciliation: scan candidate: %w", err)
		}
		absAmt := amount
		if absAmt < 0 {
			absAmt = -absAmt
		}
		var matchQuality float64
		if target > 0 {
			diff := absAmt - target
			if diff < 0 {
				diff = -diff
			}
			matchQuality = 1.0 - float64(diff)/float64(target)
		}
		candidates = append(candidates, map[string]any{
			"date":          postedAt[:10],
			"amount_cents":  amount,
			"payee":         payee,
			"fingerprint":   fp,
			"match_quality": matchQuality,
		})
	}
	return candidates, rows.Err()
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func computeBalance(db *sql.DB, accountID string, asOf time.Time) (balance int64, count int, first, last *time.Time, err error) {
	var firstStr, lastStr sql.NullString

	row := db.QueryRow(`
		SELECT COALESCE(SUM(amount_cents), 0),
		       COUNT(*),
		       MIN(posted_at),
		       MAX(posted_at)
		FROM transactions
		WHERE account_id = ?
		  AND posted_at <= ?
		  AND COALESCE(pending, 0) = 0`,
		accountID, asOf.Format("2006-01-02"),
	)
	if err = row.Scan(&balance, &count, &firstStr, &lastStr); err != nil {
		return 0, 0, nil, nil, fmt.Errorf("reconciliation: compute balance: %w", err)
	}

	if firstStr.Valid && len(firstStr.String) >= 10 {
		t, _ := time.Parse("2006-01-02", firstStr.String[:10])
		first = &t
	}
	if lastStr.Valid && len(lastStr.String) >= 10 {
		t, _ := time.Parse("2006-01-02", lastStr.String[:10])
		last = &t
	}
	return
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEvent(row rowScanner) (*Event, error) {
	var e Event
	var stmtDate, createdAt, resolvedAt, status, notes string
	var resolvedAtNullable sql.NullString

	if err := row.Scan(
		&e.ID, &e.AccountID, &stmtDate,
		&e.StatementBalanceCents, &e.CalculatedBalanceCents, &e.DeltaCents,
		&status, &notes, &createdAt, &resolvedAtNullable,
	); err != nil {
		return nil, fmt.Errorf("reconciliation: scan event: %w", err)
	}

	e.StatementDate, _ = time.Parse("2006-01-02", stmtDate)
	e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	e.Status = Status(status)
	e.Notes = notes

	_ = resolvedAt // suppress unused warning
	if resolvedAtNullable.Valid {
		t, _ := time.Parse(time.RFC3339, resolvedAtNullable.String)
		e.ResolvedAt = &t
	}

	return &e, nil
}

func scanEventRow(rows *sql.Rows) (Event, error) {
	var e Event
	var stmtDate, createdAt, status, notes string
	var resolvedAt sql.NullString

	if err := rows.Scan(
		&e.ID, &e.AccountID, &stmtDate,
		&e.StatementBalanceCents, &e.CalculatedBalanceCents, &e.DeltaCents,
		&status, &notes, &createdAt, &resolvedAt,
	); err != nil {
		return Event{}, fmt.Errorf("reconciliation: scan event row: %w", err)
	}

	e.StatementDate, _ = time.Parse("2006-01-02", stmtDate)
	e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	e.Status = Status(status)
	e.Notes = notes

	if resolvedAt.Valid {
		t, _ := time.Parse(time.RFC3339, resolvedAt.String)
		e.ResolvedAt = &t
	}
	return e, nil
}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func avgAbs(vals []int64) float64 {
	if len(vals) == 0 {
		return 0
	}
	var sum float64
	for _, v := range vals {
		sum += math.Abs(float64(v))
	}
	return sum / float64(len(vals))
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
