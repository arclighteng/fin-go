// Package audit provides an audit log for user-initiated changes to
// classifications, overrides, and reconciliation events.
//
// Every change that affects how transactions are classified or reported
// is logged with a timestamp, classifier version, and before/after values
// so that reports can be reproduced and discrepancies investigated.
package audit

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Classifier and report versions mirrored from the Python source.
const (
	ClassifierVersion = "2.0.0"
	ReportVersion     = "2.0.0"
)

// EventType represents the kind of auditable action that was performed.
type EventType string

const (
	EventOverrideSet              EventType = "override_set"
	EventOverrideRemoved          EventType = "override_removed"
	EventIncomeRuleSet            EventType = "income_rule_set"
	EventIncomeRuleRemoved        EventType = "income_rule_removed"
	EventCategoryOverrideSet      EventType = "category_override_set"
	EventCategoryOverrideRemoved  EventType = "category_override_removed"
	EventReconciliationCreated    EventType = "reconciliation_created"
	EventReconciliationResolved   EventType = "reconciliation_resolved"
	EventReportExported           EventType = "report_exported"
	EventAlertAction              EventType = "alert_action"
	EventDuplicateDismissed       EventType = "duplicate_dismissed"
)

// Event is a single entry in the audit log.
type Event struct {
	ID                int64
	EventType         EventType
	Timestamp         time.Time
	EntityType        string // e.g. "transaction", "merchant", "account"
	EntityID          string // e.g. fingerprint, merchant_norm, account_id
	OldValue          *string
	NewValue          *string
	Metadata          map[string]any
	ClassifierVersion string
	ReportVersion     string
}

// QueryOptions filters for ListEvents.
type QueryOptions struct {
	EntityType *string
	EntityID   *string
	EventType  *EventType
	Since      *time.Time
	Limit      int // 0 means default of 100
}

// EnsureSchema creates the audit_log table and its indexes if they do not
// already exist. It is safe to call on every startup (idempotent).
func EnsureSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS audit_log (
			id                 INTEGER PRIMARY KEY AUTOINCREMENT,
			event_type         TEXT    NOT NULL,
			timestamp          TEXT    NOT NULL,
			entity_type        TEXT    NOT NULL,
			entity_id          TEXT    NOT NULL,
			old_value          TEXT,
			new_value          TEXT,
			metadata           TEXT,
			classifier_version TEXT    NOT NULL,
			report_version     TEXT    NOT NULL
		);

		CREATE INDEX IF NOT EXISTS idx_audit_timestamp
			ON audit_log(timestamp DESC);

		CREATE INDEX IF NOT EXISTS idx_audit_entity
			ON audit_log(entity_type, entity_id);
	`)
	if err != nil {
		return fmt.Errorf("audit: ensure schema: %w", err)
	}
	return nil
}

// Log records an audit event and returns the persisted entry.
func Log(
	db *sql.DB,
	eventType EventType,
	entityType, entityID string,
	oldValue, newValue *string,
	metadata map[string]any,
) (*Event, error) {
	if err := EnsureSchema(db); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	nowISO := now.Format(time.RFC3339)

	var metaJSON *string
	if metadata != nil {
		b, err := json.Marshal(metadata)
		if err != nil {
			return nil, fmt.Errorf("audit: marshal metadata: %w", err)
		}
		s := string(b)
		metaJSON = &s
	}

	res, err := db.Exec(`
		INSERT INTO audit_log
			(event_type, timestamp, entity_type, entity_id, old_value, new_value,
			 metadata, classifier_version, report_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(eventType), nowISO, entityType, entityID,
		oldValue, newValue, metaJSON,
		ClassifierVersion, ReportVersion,
	)
	if err != nil {
		return nil, fmt.Errorf("audit: insert event: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("audit: get last insert id: %w", err)
	}

	return &Event{
		ID:                id,
		EventType:         eventType,
		Timestamp:         now,
		EntityType:        entityType,
		EntityID:          entityID,
		OldValue:          oldValue,
		NewValue:          newValue,
		Metadata:          metadata,
		ClassifierVersion: ClassifierVersion,
		ReportVersion:     ReportVersion,
	}, nil
}

// ListEvents queries the audit log with optional filters.
// Results are returned most-recent first.
func ListEvents(db *sql.DB, opts QueryOptions) ([]Event, error) {
	if err := EnsureSchema(db); err != nil {
		return nil, err
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}

	query := `SELECT id, event_type, timestamp, entity_type, entity_id,
	                  old_value, new_value, metadata, classifier_version, report_version
	           FROM audit_log WHERE 1=1`
	args := make([]any, 0, 5)

	if opts.EntityType != nil {
		query += " AND entity_type = ?"
		args = append(args, *opts.EntityType)
	}
	if opts.EntityID != nil {
		query += " AND entity_id = ?"
		args = append(args, *opts.EntityID)
	}
	if opts.EventType != nil {
		query += " AND event_type = ?"
		args = append(args, string(*opts.EventType))
	}
	if opts.Since != nil {
		query += " AND timestamp >= ?"
		args = append(args, opts.Since.UTC().Format(time.RFC3339))
	}
	query += " ORDER BY timestamp DESC LIMIT ?"
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("audit: query events: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// EntityHistory returns the complete audit trail for a single entity.
func EntityHistory(db *sql.DB, entityType, entityID string) ([]Event, error) {
	return ListEvents(db, QueryOptions{
		EntityType: &entityType,
		EntityID:   &entityID,
		Limit:      1000,
	})
}

// VersionInfo returns the classifier and report versions embedded in this build.
func VersionInfo() map[string]string {
	return map[string]string{
		"classifier_version": ClassifierVersion,
		"report_version":     ReportVersion,
	}
}

// scanEvent reads a single row from a query over audit_log.
func scanEvent(rows *sql.Rows) (Event, error) {
	var (
		e         Event
		ts        string
		oldVal    sql.NullString
		newVal    sql.NullString
		metaStr   sql.NullString
		evtType   string
	)

	if err := rows.Scan(
		&e.ID, &evtType, &ts, &e.EntityType, &e.EntityID,
		&oldVal, &newVal, &metaStr,
		&e.ClassifierVersion, &e.ReportVersion,
	); err != nil {
		return Event{}, fmt.Errorf("audit: scan row: %w", err)
	}

	e.EventType = EventType(evtType)

	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		// Fall back to a looser parse for rows written without timezone suffix.
		t, err = time.Parse("2006-01-02T15:04:05", ts)
		if err != nil {
			return Event{}, fmt.Errorf("audit: parse timestamp %q: %w", ts, err)
		}
		t = t.UTC()
	}
	e.Timestamp = t

	if oldVal.Valid {
		s := oldVal.String
		e.OldValue = &s
	}
	if newVal.Valid {
		s := newVal.String
		e.NewValue = &s
	}
	if metaStr.Valid && metaStr.String != "" {
		if err := json.Unmarshal([]byte(metaStr.String), &e.Metadata); err != nil {
			return Event{}, fmt.Errorf("audit: unmarshal metadata: %w", err)
		}
	}

	return e, nil
}
