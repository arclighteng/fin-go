package reconciliation

import (
	"database/sql"
	"fmt"
)

const schemaDDL = `
CREATE TABLE IF NOT EXISTS reconciliation_events (
    id                        INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id                TEXT    NOT NULL,
    statement_date            TEXT    NOT NULL,
    statement_balance_cents   INTEGER NOT NULL,
    calculated_balance_cents  INTEGER NOT NULL,
    delta_cents               INTEGER NOT NULL,
    status                    TEXT    NOT NULL DEFAULT 'pending',
    notes                     TEXT,
    created_at                TEXT    NOT NULL,
    resolved_at               TEXT,
    UNIQUE(account_id, statement_date)
);

CREATE INDEX IF NOT EXISTS idx_recon_account
    ON reconciliation_events(account_id);
`

// EnsureSchema creates the reconciliation_events table and its index if they
// do not already exist. It is safe to call on every startup (idempotent).
func EnsureSchema(db *sql.DB) error {
	if _, err := db.Exec(schemaDDL); err != nil {
		return fmt.Errorf("reconciliation: ensure schema: %w", err)
	}
	return nil
}
