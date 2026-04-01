package closebooks

import (
	"database/sql"
	"fmt"
)

const schemaDDL = `
CREATE TABLE IF NOT EXISTS closed_periods (
    id                         INTEGER PRIMARY KEY AUTOINCREMENT,
    start_date                 TEXT    NOT NULL,
    end_date                   TEXT    NOT NULL,
    closed_at                  TEXT    NOT NULL,
    closed_by                  TEXT,

    -- Snapshot of canonical totals at close time (all values in cents)
    income_cents               INTEGER NOT NULL,
    fixed_obligations_cents    INTEGER NOT NULL,
    variable_essentials_cents  INTEGER NOT NULL,
    discretionary_cents        INTEGER NOT NULL,
    one_offs_cents             INTEGER NOT NULL,
    refunds_cents              INTEGER NOT NULL,
    credits_other_cents        INTEGER NOT NULL,
    transfers_in_cents         INTEGER NOT NULL,
    transfers_out_cents        INTEGER NOT NULL,

    -- Integrity snapshot
    report_hash                TEXT    NOT NULL,
    snapshot_id                TEXT    NOT NULL,
    transaction_count          INTEGER NOT NULL,

    -- Optional JSON array of account IDs; NULL means all accounts
    account_filter             TEXT,

    -- "closed" or "superseded"
    status                     TEXT    NOT NULL DEFAULT 'closed',
    notes                      TEXT,

    UNIQUE(start_date, end_date, account_filter)
);

CREATE TABLE IF NOT EXISTS post_close_adjustments (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    closed_period_id  INTEGER NOT NULL REFERENCES closed_periods(id),
    fingerprint       TEXT    NOT NULL,
    detected_at       TEXT    NOT NULL,
    adjustment_type   TEXT    NOT NULL,  -- 'new_txn', 'modified_txn', 'deleted_txn'

    -- Transaction snapshot at detection time
    posted_at         TEXT,
    amount_cents      INTEGER,
    merchant_norm     TEXT,
    description       TEXT,

    -- Resolution: "pending", "acknowledged", "incorporated"
    status            TEXT    NOT NULL DEFAULT 'pending',
    resolved_at       TEXT,
    resolved_by       TEXT,
    resolution_notes  TEXT,

    UNIQUE(closed_period_id, fingerprint)
);

CREATE TABLE IF NOT EXISTS statement_matches (
    id                         INTEGER PRIMARY KEY AUTOINCREMENT,
    statement_id               INTEGER,
    fingerprint                TEXT    NOT NULL UNIQUE,
    matched_at                 TEXT    NOT NULL,
    matched_by                 TEXT,
    confidence                 TEXT    NOT NULL DEFAULT 'user_confirmed',

    statement_date             TEXT,
    statement_amount_cents     INTEGER,
    statement_description      TEXT
);

CREATE INDEX IF NOT EXISTS idx_closed_periods_dates
    ON closed_periods(start_date, end_date);

CREATE INDEX IF NOT EXISTS idx_adjustments_period
    ON post_close_adjustments(closed_period_id, status);
`

// EnsureSchema creates the close-the-books tables if they do not already
// exist. It is safe to call on every startup (idempotent).
func EnsureSchema(db *sql.DB) error {
	if _, err := db.Exec(schemaDDL); err != nil {
		return fmt.Errorf("closebooks: ensure schema: %w", err)
	}
	return nil
}
