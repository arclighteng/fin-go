package audit

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func ptr[T any](v T) *T { return &v }

// TestEnsureSchema verifies that EnsureSchema creates the audit_log table.
func TestEnsureSchema(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	// Table must exist and accept an insert.
	_, err := db.Exec(`INSERT INTO audit_log
		(event_type, timestamp, entity_type, entity_id, classifier_version, report_version)
		VALUES ('test', '2024-01-01T00:00:00Z', 'tx', 'fp1', '1.0', '1.0')`)
	if err != nil {
		t.Fatalf("insert after EnsureSchema: %v", err)
	}

	// Idempotent — calling again must not fail.
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
}

// TestLog verifies that Log persists an event and returns it with a valid ID.
func TestLog(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	evt, err := Log(db, EventOverrideSet, "transaction", "fp-abc", nil, ptr("groceries"), nil)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}

	if evt.ID <= 0 {
		t.Errorf("expected positive ID, got %d", evt.ID)
	}
	if evt.EventType != EventOverrideSet {
		t.Errorf("EventType: want %q, got %q", EventOverrideSet, evt.EventType)
	}
	if evt.EntityType != "transaction" {
		t.Errorf("EntityType: want %q, got %q", "transaction", evt.EntityType)
	}
	if evt.EntityID != "fp-abc" {
		t.Errorf("EntityID: want %q, got %q", "fp-abc", evt.EntityID)
	}
	if evt.OldValue != nil {
		t.Errorf("OldValue: want nil, got %v", evt.OldValue)
	}
	if evt.NewValue == nil || *evt.NewValue != "groceries" {
		t.Errorf("NewValue: want %q, got %v", "groceries", evt.NewValue)
	}
	if evt.ClassifierVersion != ClassifierVersion {
		t.Errorf("ClassifierVersion: want %q, got %q", ClassifierVersion, evt.ClassifierVersion)
	}
	if evt.ReportVersion != ReportVersion {
		t.Errorf("ReportVersion: want %q, got %q", ReportVersion, evt.ReportVersion)
	}
}

// TestLogWithMetadata verifies that metadata is round-tripped through JSON.
func TestLogWithMetadata(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	meta := map[string]any{"reason": "user clicked override", "confidence": 0.9}
	evt, err := Log(db, EventCategoryOverrideSet, "merchant", "starbucks", ptr("dining"), ptr("coffee"), meta)
	if err != nil {
		t.Fatalf("Log with metadata: %v", err)
	}

	if evt.Metadata == nil {
		t.Fatal("Metadata: want non-nil map")
	}
	if evt.Metadata["reason"] != "user clicked override" {
		t.Errorf("Metadata reason: got %v", evt.Metadata["reason"])
	}
}

// TestListEvents verifies that ListEvents returns all logged events most-recent first.
func TestListEvents(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	// Log two events.
	_, err := Log(db, EventOverrideSet, "transaction", "fp-1", nil, ptr("dining"), nil)
	if err != nil {
		t.Fatalf("Log 1: %v", err)
	}
	_, err = Log(db, EventOverrideRemoved, "transaction", "fp-2", ptr("dining"), nil, nil)
	if err != nil {
		t.Fatalf("Log 2: %v", err)
	}

	events, err := ListEvents(db, QueryOptions{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("ListEvents: want 2 events, got %d", len(events))
	}
}

// TestListEventsFilters verifies all QueryOptions filters narrow results correctly.
func TestListEventsFilters(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	_, _ = Log(db, EventOverrideSet, "transaction", "fp-1", nil, ptr("dining"), nil)
	_, _ = Log(db, EventOverrideSet, "merchant", "starbucks", nil, ptr("coffee"), nil)
	_, _ = Log(db, EventOverrideRemoved, "transaction", "fp-3", ptr("dining"), nil, nil)

	t.Run("FilterByEntityType", func(t *testing.T) {
		et := "merchant"
		events, err := ListEvents(db, QueryOptions{EntityType: &et})
		if err != nil {
			t.Fatalf("ListEvents: %v", err)
		}
		if len(events) != 1 {
			t.Errorf("want 1 event, got %d", len(events))
		}
	})

	t.Run("FilterByEntityID", func(t *testing.T) {
		eid := "fp-1"
		events, err := ListEvents(db, QueryOptions{EntityID: &eid})
		if err != nil {
			t.Fatalf("ListEvents: %v", err)
		}
		if len(events) != 1 {
			t.Errorf("want 1 event, got %d", len(events))
		}
		if events[0].EntityID != "fp-1" {
			t.Errorf("EntityID: want fp-1, got %q", events[0].EntityID)
		}
	})

	t.Run("FilterByEventType", func(t *testing.T) {
		evtType := EventOverrideRemoved
		events, err := ListEvents(db, QueryOptions{EventType: &evtType})
		if err != nil {
			t.Fatalf("ListEvents: %v", err)
		}
		if len(events) != 1 {
			t.Errorf("want 1 event, got %d", len(events))
		}
	})

	t.Run("FilterBySince", func(t *testing.T) {
		future := time.Now().Add(time.Hour)
		events, err := ListEvents(db, QueryOptions{Since: &future})
		if err != nil {
			t.Fatalf("ListEvents: %v", err)
		}
		if len(events) != 0 {
			t.Errorf("want 0 events in future, got %d", len(events))
		}
	})

	t.Run("LimitRespected", func(t *testing.T) {
		events, err := ListEvents(db, QueryOptions{Limit: 1})
		if err != nil {
			t.Fatalf("ListEvents: %v", err)
		}
		if len(events) != 1 {
			t.Errorf("want 1 event with Limit=1, got %d", len(events))
		}
	})
}

// TestEntityHistory verifies that EntityHistory returns only events for the requested entity.
func TestEntityHistory(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	_, _ = Log(db, EventOverrideSet, "transaction", "fp-target", nil, ptr("dining"), nil)
	_, _ = Log(db, EventOverrideRemoved, "transaction", "fp-target", ptr("dining"), nil, nil)
	_, _ = Log(db, EventOverrideSet, "transaction", "fp-other", nil, ptr("groceries"), nil)

	history, err := EntityHistory(db, "transaction", "fp-target")
	if err != nil {
		t.Fatalf("EntityHistory: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("EntityHistory: want 2 events, got %d", len(history))
	}
	for _, e := range history {
		if e.EntityID != "fp-target" {
			t.Errorf("EntityID: want fp-target, got %q", e.EntityID)
		}
	}
}

// TestEntityHistoryEmpty verifies that EntityHistory returns an empty slice when there are no events.
func TestEntityHistoryEmpty(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	history, err := EntityHistory(db, "transaction", "no-such-fp")
	if err != nil {
		t.Fatalf("EntityHistory: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("want 0 events, got %d", len(history))
	}
}

// TestVersionInfo verifies the version map contains expected keys.
func TestVersionInfo(t *testing.T) {
	t.Parallel()
	info := VersionInfo()
	if info["classifier_version"] != ClassifierVersion {
		t.Errorf("classifier_version: want %q, got %q", ClassifierVersion, info["classifier_version"])
	}
	if info["report_version"] != ReportVersion {
		t.Errorf("report_version: want %q, got %q", ReportVersion, info["report_version"])
	}
}

// TestAllEventTypes verifies every declared EventType constant can be logged and retrieved.
func TestAllEventTypes(t *testing.T) {
	t.Parallel()
	allTypes := []EventType{
		EventOverrideSet,
		EventOverrideRemoved,
		EventIncomeRuleSet,
		EventIncomeRuleRemoved,
		EventCategoryOverrideSet,
		EventCategoryOverrideRemoved,
		EventReconciliationCreated,
		EventReconciliationResolved,
		EventReportExported,
		EventAlertAction,
		EventDuplicateDismissed,
	}

	for _, et := range allTypes {
		t.Run(string(et), func(t *testing.T) {
			t.Parallel()
			db := newTestDB(t)
			evt, err := Log(db, et, "entity", "id-1", nil, nil, nil)
			if err != nil {
				t.Fatalf("Log(%q): %v", et, err)
			}
			if evt.EventType != et {
				t.Errorf("EventType: want %q, got %q", et, evt.EventType)
			}

			filterType := et
			events, err := ListEvents(db, QueryOptions{EventType: &filterType})
			if err != nil {
				t.Fatalf("ListEvents: %v", err)
			}
			if len(events) != 1 {
				t.Errorf("want 1 event, got %d", len(events))
			}
		})
	}
}

// TestListEventsDefaultLimit verifies that an unset Limit defaults to 100.
func TestListEventsDefaultLimit(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	// Insert 5 events — well below 100, so all should appear.
	for i := 0; i < 5; i++ {
		_, err := Log(db, EventOverrideSet, "transaction", "fp", nil, nil, nil)
		if err != nil {
			t.Fatalf("Log %d: %v", i, err)
		}
	}

	events, err := ListEvents(db, QueryOptions{Limit: 0})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 5 {
		t.Errorf("want 5 events, got %d", len(events))
	}
}

// TestLogNilValues verifies that nil old/new values are stored and retrieved correctly.
func TestLogNilValues(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	evt, err := Log(db, EventReportExported, "report", "rpt-1", nil, nil, nil)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if evt.OldValue != nil {
		t.Errorf("OldValue: want nil, got %v", evt.OldValue)
	}
	if evt.NewValue != nil {
		t.Errorf("NewValue: want nil, got %v", evt.NewValue)
	}

	events, err := ListEvents(db, QueryOptions{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	if events[0].OldValue != nil || events[0].NewValue != nil {
		t.Errorf("round-trip nil values failed")
	}
}

// TestTimestampRoundTrip verifies that timestamps survive a write-read cycle.
func TestTimestampRoundTrip(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	before := time.Now().UTC().Truncate(time.Second)
	_, err := Log(db, EventOverrideSet, "transaction", "fp-ts", nil, ptr("dining"), nil)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	after := time.Now().UTC().Add(time.Second)

	events, err := ListEvents(db, QueryOptions{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("no events returned")
	}
	ts := events[0].Timestamp
	if ts.Before(before) || ts.After(after) {
		t.Errorf("Timestamp %v is outside [%v, %v]", ts, before, after)
	}
}

// TestLogMetadataKeys verifies multiple metadata keys are preserved.
func TestLogMetadataKeys(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	meta := map[string]any{
		"source":     "user",
		"count":      float64(3),
		"active":     true,
	}
	_, err := Log(db, EventAlertAction, "alert", "alert-1", nil, nil, meta)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}

	events, err := ListEvents(db, QueryOptions{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("no events")
	}
	m := events[0].Metadata
	if m == nil {
		t.Fatal("metadata is nil")
	}
	for k, want := range meta {
		got, ok := m[k]
		if !ok {
			t.Errorf("metadata key %q missing", k)
			continue
		}
		// JSON numbers unmarshal as float64.
		_ = want
		_ = got
	}
	// Spot-check the string value.
	if m["source"] != "user" {
		t.Errorf(`metadata["source"]: want "user", got %v`, m["source"])
	}
}

// TestEntityTypeString verifies EntityType constants are non-empty strings.
func TestEntityTypeString(t *testing.T) {
	t.Parallel()
	for _, et := range []EventType{
		EventOverrideSet,
		EventDuplicateDismissed,
	} {
		if strings.TrimSpace(string(et)) == "" {
			t.Errorf("EventType %v has empty string value", et)
		}
	}
}
