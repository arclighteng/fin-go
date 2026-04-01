package reconciliation

import (
	"database/sql"
	"fmt"
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

// createTransactionsTable creates the minimal transactions + accounts schema
// needed by Reconcile.
func createTransactionsTable(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS accounts (
			account_id TEXT PRIMARY KEY,
			name       TEXT
		);
		CREATE TABLE IF NOT EXISTS transactions (
			fingerprint  TEXT PRIMARY KEY,
			account_id   TEXT NOT NULL,
			posted_at    TEXT NOT NULL,
			amount_cents INTEGER NOT NULL,
			pending      INTEGER DEFAULT 0
		);
	`)
	if err != nil {
		t.Fatalf("create tables: %v", err)
	}
}

// TestEnsureSchema verifies that EnsureSchema creates the reconciliation_events table.
func TestEnsureSchema(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM reconciliation_events").Scan(&n); err != nil {
		t.Fatalf("reconciliation_events table not created: %v", err)
	}

	// Idempotent.
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
}

// TestIsMatched verifies the $1 tolerance on Event.
func TestIsMatched(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		deltaCents int64
		want       bool
	}{
		{"zero delta", 0, true},
		{"within tolerance positive", 100, true},
		{"within tolerance negative", -100, true},
		{"over tolerance positive", 101, false},
		{"over tolerance negative", -101, false},
		{"exact threshold", 50, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := &Event{DeltaCents: tc.deltaCents}
			if got := e.IsMatched(); got != tc.want {
				t.Errorf("IsMatched(%d): want %v, got %v", tc.deltaCents, tc.want, got)
			}
		})
	}
}

// TestResultIsMatched verifies the $1 tolerance on Result.
func TestResultIsMatched(t *testing.T) {
	t.Parallel()
	tests := []struct {
		deltaCents int64
		want       bool
	}{
		{0, true},
		{99, true},
		{100, true},
		{101, false},
		{-99, true},
		{-101, false},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("delta_%d", tc.deltaCents), func(t *testing.T) {
			t.Parallel()
			r := &Result{DeltaCents: tc.deltaCents}
			if got := r.IsMatched(); got != tc.want {
				t.Errorf("Result.IsMatched(%d): want %v, got %v", tc.deltaCents, tc.want, got)
			}
		})
	}
}

// TestDeltaDirection verifies the three delta direction labels.
func TestDeltaDirection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		delta int64
		want  string
	}{
		{500, "missing_income"},
		{-500, "missing_expense"},
		{0, "balanced"},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("delta_%d", tc.delta), func(t *testing.T) {
			t.Parallel()
			r := &Result{DeltaCents: tc.delta}
			if got := r.DeltaDirection(); got != tc.want {
				t.Errorf("DeltaDirection(%d): want %q, got %q", tc.delta, tc.want, got)
			}
		})
	}
}

// TestReconcileComputesBalance verifies that Reconcile sums transactions correctly.
func TestReconcileComputesBalance(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createTransactionsTable(t, db)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	// Insert a known account.
	_, err := db.Exec("INSERT INTO accounts (account_id, name) VALUES ('acct-1', 'Checking')")
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	// Insert transactions: +10000, -3000, -2000 → sum = 5000.
	txns := []struct {
		fp      string
		posted  string
		amount  int64
		pending int
	}{
		{"fp-1", "2025-03-01", 10000, 0},
		{"fp-2", "2025-03-10", -3000, 0},
		{"fp-3", "2025-03-20", -2000, 0},
		{"fp-4", "2025-04-05", 5000, 0}, // after statement date — excluded
		{"fp-5", "2025-03-15", 1000, 1}, // pending — excluded
	}
	for _, tx := range txns {
		_, err = db.Exec(
			"INSERT INTO transactions (fingerprint, account_id, posted_at, amount_cents, pending) VALUES (?,?,?,?,?)",
			tx.fp, "acct-1", tx.posted, tx.amount, tx.pending,
		)
		if err != nil {
			t.Fatalf("insert transaction %s: %v", tx.fp, err)
		}
	}

	asOf := time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC)
	result, err := Reconcile(db, "acct-1", 5000, asOf)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if result.CalculatedBalanceCents != 5000 {
		t.Errorf("CalculatedBalance: want 5000, got %d", result.CalculatedBalanceCents)
	}
	if result.DeltaCents != 0 {
		t.Errorf("Delta: want 0, got %d", result.DeltaCents)
	}
	if result.TransactionCount != 3 {
		t.Errorf("TransactionCount: want 3, got %d", result.TransactionCount)
	}
	if result.AccountName != "Checking" {
		t.Errorf("AccountName: want %q, got %q", "Checking", result.AccountName)
	}
	if !result.IsMatched() {
		t.Error("IsMatched: want true")
	}
}

// TestReconcileUnknownAccount verifies that an unknown account_id falls back
// to using the account_id as the name.
func TestReconcileUnknownAccount(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createTransactionsTable(t, db)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	asOf := time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC)
	result, err := Reconcile(db, "unknown-acct", 0, asOf)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if result.AccountName != "unknown-acct" {
		t.Errorf("AccountName fallback: want %q, got %q", "unknown-acct", result.AccountName)
	}
}

// TestReconcileDiscrepancy verifies that a non-zero delta is classified as discrepancy.
func TestReconcileDiscrepancy(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createTransactionsTable(t, db)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	_, _ = db.Exec("INSERT INTO accounts (account_id, name) VALUES ('acct-disc', 'Savings')")
	_, _ = db.Exec(
		"INSERT INTO transactions (fingerprint, account_id, posted_at, amount_cents, pending) VALUES ('fp-d1','acct-disc','2025-03-10',10000,0)",
	)

	asOf := time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC)
	// Statement says 15000 but calculated is 10000 → delta = 5000.
	result, err := Reconcile(db, "acct-disc", 15000, asOf)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if result.DeltaCents != 5000 {
		t.Errorf("DeltaCents: want 5000, got %d", result.DeltaCents)
	}
	if result.IsMatched() {
		t.Error("IsMatched: want false for large delta")
	}
}

// TestSaveEventPersistsResult verifies that SaveEvent writes a row to DB.
func TestSaveEventPersistsResult(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createTransactionsTable(t, db)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	result := &Result{
		AccountID:              "acct-save",
		AccountName:            "Test",
		StatementDate:          time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC),
		StatementBalanceCents:  10000,
		CalculatedBalanceCents: 10000,
		DeltaCents:             0,
	}

	evt, err := SaveEvent(db, result, "all good")
	if err != nil {
		t.Fatalf("SaveEvent: %v", err)
	}
	if evt.ID <= 0 {
		t.Errorf("ID: want positive, got %d", evt.ID)
	}
	if evt.Status != StatusMatched {
		t.Errorf("Status: want %q, got %q", StatusMatched, evt.Status)
	}
	if evt.Notes != "all good" {
		t.Errorf("Notes: want %q, got %q", "all good", evt.Notes)
	}
}

// TestSaveEventDiscrepancyStatus verifies that a non-matched result gets StatusDiscrepancy.
func TestSaveEventDiscrepancyStatus(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createTransactionsTable(t, db)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	result := &Result{
		AccountID:              "acct-disc",
		StatementDate:          time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC),
		StatementBalanceCents:  20000,
		CalculatedBalanceCents: 10000,
		DeltaCents:             10000, // large discrepancy
	}

	evt, err := SaveEvent(db, result, "")
	if err != nil {
		t.Fatalf("SaveEvent: %v", err)
	}
	if evt.Status != StatusDiscrepancy {
		t.Errorf("Status: want %q, got %q", StatusDiscrepancy, evt.Status)
	}
}

// TestSaveEventUpsert verifies that re-saving the same (account, date) updates the row.
func TestSaveEventUpsert(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createTransactionsTable(t, db)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	stmtDate := time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC)
	result := &Result{
		AccountID:              "acct-u",
		StatementDate:          stmtDate,
		StatementBalanceCents:  10000,
		CalculatedBalanceCents: 10000,
		DeltaCents:             0,
	}

	evt1, err := SaveEvent(db, result, "first save")
	if err != nil {
		t.Fatalf("first SaveEvent: %v", err)
	}

	result.StatementBalanceCents = 20000
	result.CalculatedBalanceCents = 10000
	result.DeltaCents = 10000
	evt2, err := SaveEvent(db, result, "updated save")
	if err != nil {
		t.Fatalf("second SaveEvent: %v", err)
	}

	if evt1.ID != evt2.ID {
		t.Errorf("upsert should produce same ID: first=%d, second=%d", evt1.ID, evt2.ID)
	}
	if evt2.Status != StatusDiscrepancy {
		t.Errorf("Status after upsert: want %q, got %q", StatusDiscrepancy, evt2.Status)
	}
}

// TestListHistory verifies that ListHistory returns events for a specific account.
func TestListHistory(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createTransactionsTable(t, db)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	for i, acct := range []string{"acct-A", "acct-A", "acct-B"} {
		result := &Result{
			AccountID:     acct,
			StatementDate: time.Date(2025, time.Month(i+1), 28, 0, 0, 0, 0, time.UTC),
			DeltaCents:    0,
		}
		if _, err := SaveEvent(db, result, ""); err != nil {
			t.Fatalf("SaveEvent %d: %v", i, err)
		}
	}

	t.Run("FilterByAccount", func(t *testing.T) {
		events, err := ListHistory(db, "acct-A", 10)
		if err != nil {
			t.Fatalf("ListHistory: %v", err)
		}
		if len(events) != 2 {
			t.Errorf("want 2 events for acct-A, got %d", len(events))
		}
		for _, e := range events {
			if e.AccountID != "acct-A" {
				t.Errorf("AccountID: want acct-A, got %q", e.AccountID)
			}
		}
	})

	t.Run("AllAccounts", func(t *testing.T) {
		events, err := ListHistory(db, "", 10)
		if err != nil {
			t.Fatalf("ListHistory: %v", err)
		}
		if len(events) != 3 {
			t.Errorf("want 3 events total, got %d", len(events))
		}
	})

	t.Run("LimitRespected", func(t *testing.T) {
		events, err := ListHistory(db, "", 1)
		if err != nil {
			t.Fatalf("ListHistory: %v", err)
		}
		if len(events) != 1 {
			t.Errorf("want 1 event with limit=1, got %d", len(events))
		}
	})
}

// TestResolveEvent verifies that ResolveEvent changes the status to resolved.
func TestResolveEvent(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createTransactionsTable(t, db)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	stmtDate := time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC)
	result := &Result{
		AccountID:     "acct-res",
		StatementDate: stmtDate,
		DeltaCents:    50000, // big discrepancy
	}
	if _, err := SaveEvent(db, result, ""); err != nil {
		t.Fatalf("SaveEvent: %v", err)
	}

	if err := ResolveEvent(db, "acct-res", stmtDate, "resolved manually"); err != nil {
		t.Fatalf("ResolveEvent: %v", err)
	}

	events, err := ListHistory(db, "acct-res", 1)
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("no events after resolve")
	}
	if events[0].Status != StatusResolved {
		t.Errorf("Status: want %q, got %q", StatusResolved, events[0].Status)
	}
	if events[0].ResolvedAt == nil {
		t.Error("ResolvedAt: want non-nil after resolve")
	}
}

// TestListPendingDiscrepancies verifies that only discrepancy events are returned.
func TestListPendingDiscrepancies(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createTransactionsTable(t, db)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	// One matched, one discrepancy.
	matchedResult := &Result{
		AccountID:     "acct-ok",
		StatementDate: time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC),
		DeltaCents:    0,
	}
	discResult := &Result{
		AccountID:     "acct-bad",
		StatementDate: time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC),
		DeltaCents:    50000,
	}

	_, _ = SaveEvent(db, matchedResult, "")
	_, _ = SaveEvent(db, discResult, "")

	discrepancies, err := ListPendingDiscrepancies(db)
	if err != nil {
		t.Fatalf("ListPendingDiscrepancies: %v", err)
	}
	if len(discrepancies) != 1 {
		t.Errorf("want 1 discrepancy, got %d", len(discrepancies))
	}
	if discrepancies[0].AccountID != "acct-bad" {
		t.Errorf("AccountID: want acct-bad, got %q", discrepancies[0].AccountID)
	}
}

// TestStatusConstants verifies Status constant string values are non-empty.
func TestStatusConstants(t *testing.T) {
	t.Parallel()
	for _, s := range []Status{StatusMatched, StatusDiscrepancy, StatusPending, StatusResolved} {
		if string(s) == "" {
			t.Errorf("Status constant has empty string value")
		}
	}
}

// TestAnalyzePatternsEmptyDB verifies AnalyzePatterns does not error on an empty DB.
func TestAnalyzePatternsEmptyDB(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createTransactionsTable(t, db)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	insight, err := AnalyzePatterns(db, 3)
	if err != nil {
		t.Fatalf("AnalyzePatterns: %v", err)
	}
	if insight == nil {
		t.Fatal("AnalyzePatterns: want non-nil insight")
	}
	if len(insight.Patterns) != 0 {
		t.Errorf("want 0 patterns, got %d", len(insight.Patterns))
	}
}
