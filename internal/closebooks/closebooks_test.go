package closebooks

import (
	"database/sql"
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

func sampleTotals() PeriodTotals {
	return PeriodTotals{
		IncomeCents:             500000,
		FixedObligationsCents:   120000,
		VariableEssentialsCents: 80000,
		DiscretionaryCents:      60000,
		OneOffsCents:            10000,
		RefundsCents:            5000,
		CreditsOtherCents:       0,
		TransfersInCents:        0,
		TransfersOutCents:       0,
		ReportHash:              "abc123",
		SnapshotID:              "snap-1",
		TransactionCount:        42,
	}
}

// TestEnsureSchema verifies that EnsureSchema creates the required tables.
func TestEnsureSchema(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	// All three tables should be queryable.
	for _, table := range []string{"closed_periods", "post_close_adjustments", "statement_matches"} {
		var n int
		err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n)
		if err != nil {
			t.Errorf("table %q not created: %v", table, err)
		}
	}

	// Idempotent.
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
}

// TestClosePeriod verifies that ClosePeriod persists a closed period record.
func TestClosePeriod(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	start := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)
	totals := sampleTotals()

	cp, err := ClosePeriod(db, start, end, totals, CloseOptions{ClosedBy: "alice", Notes: "end of month"})
	if err != nil {
		t.Fatalf("ClosePeriod: %v", err)
	}

	if cp.ID <= 0 {
		t.Errorf("ID: want positive, got %d", cp.ID)
	}
	if cp.Status != "closed" {
		t.Errorf("Status: want %q, got %q", "closed", cp.Status)
	}
	if cp.ClosedBy != "alice" {
		t.Errorf("ClosedBy: want %q, got %q", "alice", cp.ClosedBy)
	}
	if cp.Notes != "end of month" {
		t.Errorf("Notes: want %q, got %q", "end of month", cp.Notes)
	}
	if cp.IncomeCents != totals.IncomeCents {
		t.Errorf("IncomeCents: want %d, got %d", totals.IncomeCents, cp.IncomeCents)
	}
	if cp.TransactionCount != totals.TransactionCount {
		t.Errorf("TransactionCount: want %d, got %d", totals.TransactionCount, cp.TransactionCount)
	}
	if cp.ReportHash != totals.ReportHash {
		t.Errorf("ReportHash: want %q, got %q", totals.ReportHash, cp.ReportHash)
	}
}

// TestClosePeriodSupersedes verifies that re-closing the same range marks the old record superseded.
func TestClosePeriodSupersedes(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	start := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)

	first, err := ClosePeriod(db, start, end, sampleTotals(), CloseOptions{})
	if err != nil {
		t.Fatalf("first ClosePeriod: %v", err)
	}

	newTotals := sampleTotals()
	newTotals.IncomeCents = 600000
	newTotals.SnapshotID = "snap-2"
	second, err := ClosePeriod(db, start, end, newTotals, CloseOptions{})
	if err != nil {
		t.Fatalf("second ClosePeriod: %v", err)
	}

	if second.ID == first.ID {
		t.Error("second close should create a new record, not reuse the first")
	}
	if second.IncomeCents != 600000 {
		t.Errorf("IncomeCents: want 600000, got %d", second.IncomeCents)
	}

	// Old record should be superseded.
	var status string
	err = db.QueryRow("SELECT status FROM closed_periods WHERE id = ?", first.ID).Scan(&status)
	if err != nil {
		t.Fatalf("query old status: %v", err)
	}
	if status != "superseded" {
		t.Errorf("old record status: want %q, got %q", "superseded", status)
	}
}

// TestListClosedPeriods verifies that ListClosedPeriods returns all active closed periods.
func TestListClosedPeriods(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	// Initially empty.
	periods, err := ListClosedPeriods(db)
	if err != nil {
		t.Fatalf("ListClosedPeriods (empty): %v", err)
	}
	if len(periods) != 0 {
		t.Fatalf("want 0 periods, got %d", len(periods))
	}

	// Add two periods.
	for i := 1; i <= 2; i++ {
		start := time.Date(2025, time.Month(i), 1, 0, 0, 0, 0, time.UTC)
		end := start.AddDate(0, 1, 0)
		if _, err := ClosePeriod(db, start, end, sampleTotals(), CloseOptions{}); err != nil {
			t.Fatalf("ClosePeriod %d: %v", i, err)
		}
	}

	periods, err = ListClosedPeriods(db)
	if err != nil {
		t.Fatalf("ListClosedPeriods: %v", err)
	}
	if len(periods) != 2 {
		t.Errorf("want 2 periods, got %d", len(periods))
	}
	for _, p := range periods {
		if p.Status != "closed" {
			t.Errorf("Status: want %q, got %q", "closed", p.Status)
		}
	}
}

// TestListClosedPeriodsExcludesSuperseded verifies that superseded records are not returned.
func TestListClosedPeriodsExcludesSuperseded(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	start := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)

	// Close once, then re-close (supersedes the first).
	_, _ = ClosePeriod(db, start, end, sampleTotals(), CloseOptions{})
	_, _ = ClosePeriod(db, start, end, sampleTotals(), CloseOptions{})

	periods, err := ListClosedPeriods(db)
	if err != nil {
		t.Fatalf("ListClosedPeriods: %v", err)
	}
	// Only the active record should appear.
	if len(periods) != 1 {
		t.Errorf("want 1 active period, got %d", len(periods))
	}
}

// TestGetClosedPeriod verifies that GetClosedPeriod returns the matching active record.
func TestGetClosedPeriod(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	start := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)
	_, err := ClosePeriod(db, start, end, sampleTotals(), CloseOptions{Notes: "q1"})
	if err != nil {
		t.Fatalf("ClosePeriod: %v", err)
	}

	cp, err := GetClosedPeriod(db, start, end, nil)
	if err != nil {
		t.Fatalf("GetClosedPeriod: %v", err)
	}
	if cp == nil {
		t.Fatal("GetClosedPeriod: want non-nil, got nil")
	}
	if cp.Status != "closed" {
		t.Errorf("Status: want %q, got %q", "closed", cp.Status)
	}
}

// TestGetClosedPeriodNotFound verifies that GetClosedPeriod returns nil when no match.
func TestGetClosedPeriodNotFound(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	start := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)

	cp, err := GetClosedPeriod(db, start, end, nil)
	if err != nil {
		t.Fatalf("GetClosedPeriod: %v", err)
	}
	if cp != nil {
		t.Errorf("want nil for missing period, got %+v", cp)
	}
}

// TestClosedPeriodComputedFields verifies TotalExpensesCents and NetCents.
func TestClosedPeriodComputedFields(t *testing.T) {
	t.Parallel()
	cp := &ClosedPeriod{
		IncomeCents:             500000,
		FixedObligationsCents:   100000,
		VariableEssentialsCents: 80000,
		DiscretionaryCents:      60000,
		OneOffsCents:            10000,
		RefundsCents:            5000,
	}

	wantExpenses := int64(100000 + 80000 + 60000 + 10000)
	if got := cp.TotalExpensesCents(); got != wantExpenses {
		t.Errorf("TotalExpensesCents: want %d, got %d", wantExpenses, got)
	}

	wantNet := cp.IncomeCents + cp.RefundsCents - wantExpenses
	if got := cp.NetCents(); got != wantNet {
		t.Errorf("NetCents: want %d, got %d", wantNet, got)
	}
}

// TestAdjustmentSummaryHelpers verifies HasPending and NetChangeCents.
func TestAdjustmentSummaryHelpers(t *testing.T) {
	t.Parallel()
	period := ClosedPeriod{
		IncomeCents:         500000,
		DiscretionaryCents:  100000,
	}
	amtCents := int64(25000)
	summary := &AdjustmentSummary{
		Period: period,
		PendingAdjustments: []PostCloseAdjustment{
			{AmountCents: &amtCents},
		},
		TotalAdjustmentCents: amtCents,
		AdjustedNetCents:     period.NetCents() + amtCents,
	}

	if !summary.HasPending() {
		t.Error("HasPending: want true")
	}
	if summary.NetChangeCents() != amtCents {
		t.Errorf("NetChangeCents: want %d, got %d", amtCents, summary.NetChangeCents())
	}

	empty := &AdjustmentSummary{Period: period}
	if empty.HasPending() {
		t.Error("HasPending on empty: want false")
	}
}

// TestDetectPostCloseAdjustments verifies that new transactions after close are detected.
func TestDetectPostCloseAdjustments(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	// Minimal transactions table.
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS transactions (
		fingerprint TEXT PRIMARY KEY,
		account_id  TEXT,
		posted_at   TEXT,
		amount_cents INTEGER,
		merchant    TEXT,
		description TEXT,
		pending     INTEGER DEFAULT 0
	)`)
	if err != nil {
		t.Fatalf("create transactions: %v", err)
	}

	start := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)

	// Close the period with 0 transactions.
	totals := sampleTotals()
	totals.TransactionCount = 0
	cp, err := ClosePeriod(db, start, end, totals, CloseOptions{})
	if err != nil {
		t.Fatalf("ClosePeriod: %v", err)
	}

	// No transactions yet — no adjustments expected.
	adjs, err := DetectPostCloseAdjustments(db, *cp)
	if err != nil {
		t.Fatalf("DetectPostCloseAdjustments (empty): %v", err)
	}
	if len(adjs) != 0 {
		t.Errorf("want 0 adjustments, got %d", len(adjs))
	}

	// Insert a transaction that falls within the closed period.
	_, err = db.Exec(`INSERT INTO transactions (fingerprint, account_id, posted_at, amount_cents, merchant, pending)
		VALUES ('fp-new', 'acct-1', '2025-03-15', -5000, 'coffee shop', 0)`)
	if err != nil {
		t.Fatalf("insert transaction: %v", err)
	}

	adjs, err = DetectPostCloseAdjustments(db, *cp)
	if err != nil {
		t.Fatalf("DetectPostCloseAdjustments (with txn): %v", err)
	}
	if len(adjs) != 1 {
		t.Fatalf("want 1 adjustment, got %d", len(adjs))
	}
	if adjs[0].Fingerprint != "fp-new" {
		t.Errorf("Fingerprint: want fp-new, got %q", adjs[0].Fingerprint)
	}
	if adjs[0].AdjustmentType != "new_txn" {
		t.Errorf("AdjustmentType: want new_txn, got %q", adjs[0].AdjustmentType)
	}
	if adjs[0].Status != "pending" {
		t.Errorf("Status: want pending, got %q", adjs[0].Status)
	}
}

// TestDetectPostCloseAdjustmentsNoChangeWhenCountMatches verifies that equal
// transaction counts do not trigger adjustment detection.
func TestDetectPostCloseAdjustmentsNoChangeWhenCountMatches(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS transactions (
		fingerprint TEXT PRIMARY KEY,
		account_id  TEXT,
		posted_at   TEXT,
		amount_cents INTEGER,
		merchant    TEXT,
		description TEXT,
		pending     INTEGER DEFAULT 0
	)`)
	if err != nil {
		t.Fatalf("create transactions: %v", err)
	}

	// Insert one transaction.
	_, err = db.Exec(`INSERT INTO transactions (fingerprint, account_id, posted_at, amount_cents, merchant, pending)
		VALUES ('fp-existing', 'acct-1', '2025-03-15', -5000, 'grocery', 0)`)
	if err != nil {
		t.Fatalf("insert transaction: %v", err)
	}

	start := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)

	// Close the period reflecting the 1 existing transaction.
	totals := sampleTotals()
	totals.TransactionCount = 1
	cp, err := ClosePeriod(db, start, end, totals, CloseOptions{})
	if err != nil {
		t.Fatalf("ClosePeriod: %v", err)
	}

	// Count matches snapshot — no adjustments expected.
	adjs, err := DetectPostCloseAdjustments(db, *cp)
	if err != nil {
		t.Fatalf("DetectPostCloseAdjustments: %v", err)
	}
	if len(adjs) != 0 {
		t.Errorf("want 0 adjustments when counts match, got %d", len(adjs))
	}
}

// TestClosePeriodWithAccountFilter verifies that account filters are persisted and round-trip correctly.
func TestClosePeriodWithAccountFilter(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	start := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)
	filter := []string{"acct-2", "acct-1"} // unsorted on input

	cp, err := ClosePeriod(db, start, end, sampleTotals(), CloseOptions{AccountFilter: filter})
	if err != nil {
		t.Fatalf("ClosePeriod: %v", err)
	}
	if len(cp.AccountFilter) != 2 {
		t.Fatalf("AccountFilter length: want 2, got %d", len(cp.AccountFilter))
	}

	// Round-trip via GetClosedPeriod.
	fetched, err := GetClosedPeriod(db, start, end, filter)
	if err != nil {
		t.Fatalf("GetClosedPeriod: %v", err)
	}
	if fetched == nil {
		t.Fatal("GetClosedPeriod: want non-nil")
	}
	if len(fetched.AccountFilter) != 2 {
		t.Errorf("fetched AccountFilter: want 2 items, got %d", len(fetched.AccountFilter))
	}
}

// TestSaveAndGetMatchedFingerprints verifies statement match persistence.
func TestSaveAndGetMatchedFingerprints(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	fps, err := GetMatchedFingerprints(db)
	if err != nil {
		t.Fatalf("GetMatchedFingerprints (empty): %v", err)
	}
	if len(fps) != 0 {
		t.Errorf("want 0 fingerprints, got %d", len(fps))
	}

	stmtDate := time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC)
	amt := int64(5000)
	desc := "STARBUCKS"
	err = SaveStatementMatch(db, "fp-sm-1", &stmtDate, &amt, &desc, "alice", "user_confirmed")
	if err != nil {
		t.Fatalf("SaveStatementMatch: %v", err)
	}

	fps, err = GetMatchedFingerprints(db)
	if err != nil {
		t.Fatalf("GetMatchedFingerprints: %v", err)
	}
	if _, ok := fps["fp-sm-1"]; !ok {
		t.Error("fp-sm-1 not found in matched fingerprints")
	}
}

// TestSaveStatementMatchUpsert verifies that saving the same fingerprint twice updates in place.
func TestSaveStatementMatchUpsert(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	for i := 0; i < 2; i++ {
		err := SaveStatementMatch(db, "fp-upsert", nil, nil, nil, "", "")
		if err != nil {
			t.Fatalf("SaveStatementMatch iteration %d: %v", i, err)
		}
	}

	fps, err := GetMatchedFingerprints(db)
	if err != nil {
		t.Fatalf("GetMatchedFingerprints: %v", err)
	}
	if len(fps) != 1 {
		t.Errorf("want exactly 1 fingerprint after upsert, got %d", len(fps))
	}
}

// TestAcknowledgeAdjustment verifies that AcknowledgeAdjustment changes the status.
func TestAcknowledgeAdjustment(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS transactions (
		fingerprint TEXT PRIMARY KEY,
		account_id  TEXT,
		posted_at   TEXT,
		amount_cents INTEGER,
		merchant    TEXT,
		description TEXT,
		pending     INTEGER DEFAULT 0
	)`)
	if err != nil {
		t.Fatalf("create transactions: %v", err)
	}

	// Create a closed period with 0 transactions (so detection fires).
	start := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)
	totals := sampleTotals()
	totals.TransactionCount = 0
	cp, err := ClosePeriod(db, start, end, totals, CloseOptions{})
	if err != nil {
		t.Fatalf("ClosePeriod: %v", err)
	}

	// Insert a post-close transaction.
	_, _ = db.Exec(`INSERT INTO transactions (fingerprint, posted_at, amount_cents, pending)
		VALUES ('fp-ack', '2025-03-20', -1000, 0)`)

	adjs, err := DetectPostCloseAdjustments(db, *cp)
	if err != nil {
		t.Fatalf("DetectPostCloseAdjustments: %v", err)
	}
	if len(adjs) == 0 {
		t.Fatal("expected at least one adjustment")
	}

	adjID := adjs[0].ID
	if err := AcknowledgeAdjustment(db, adjID, "bob", "reviewed and accepted"); err != nil {
		t.Fatalf("AcknowledgeAdjustment: %v", err)
	}

	// Pending list should now be empty.
	pending, err := ListPendingAdjustments(db, cp.ID)
	if err != nil {
		t.Fatalf("ListPendingAdjustments: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("want 0 pending adjustments after acknowledge, got %d", len(pending))
	}
}
