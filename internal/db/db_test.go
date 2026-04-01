package db_test

import (
	"testing"
	"time"

	findb "github.com/arclighteng/fin-go/internal/db"
	"github.com/arclighteng/fin-go/internal/models"
)

// newTestDB opens an in-memory SQLite database, initialises the schema, and
// returns the *findb.DB. The caller is responsible for closing it.
func newTestDB(t *testing.T) *findb.DB {
	t.Helper()

	d, err := findb.Connect(":memory:")
	if err != nil {
		t.Fatalf("db.Connect: %v", err)
	}
	if err := d.Init(); err != nil {
		d.Close()
		t.Fatalf("Init: %v", err)
	}

	t.Cleanup(func() { d.Close() })
	return d
}

// sampleAccount returns a populated Account for test use.
func sampleAccount(id string) models.Account {
	return models.Account{
		AccountID:   id,
		Institution: "Test Bank",
		Name:        "Checking " + id,
		Type:        "checking",
		Currency:    "USD",
	}
}

// sampleTransaction returns a posted transaction for test use.
func sampleTransaction(accountID, srcID, fingerprint string, amountCents int64) models.Transaction {
	return models.Transaction{
		AccountID:   accountID,
		PostedAt:    time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
		AmountCents: amountCents,
		Currency:    "USD",
		Description: "Test purchase",
		Merchant:    "Acme Corp",
		SourceTxnID: srcID,
		Fingerprint: fingerprint,
		Pending:     false,
	}
}

// ---- Tests -----------------------------------------------------------------

func TestInit_CreatesSchema(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)

	// Verify key tables exist by querying them.
	tables := []string{"accounts", "transactions", "runs", "alert_actions"}
	for _, tbl := range tables {
		var count int
		err := d.QueryRow("SELECT COUNT(*) FROM " + tbl).Scan(&count)
		if err != nil {
			t.Errorf("table %q not accessible after Init: %v", tbl, err)
		}
	}
}

func TestInit_Idempotent(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	// Calling Init a second time should not fail (IF EXISTS guards).
	if err := d.Init(); err != nil {
		t.Errorf("second Init call failed: %v", err)
	}
}

func TestUpsertAccounts_Insert(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)

	accounts := []models.Account{
		sampleAccount("acc-1"),
		sampleAccount("acc-2"),
	}
	if err := d.UpsertAccounts(accounts); err != nil {
		t.Fatalf("UpsertAccounts: %v", err)
	}

	got, err := d.GetAccounts()
	if err != nil {
		t.Fatalf("GetAccounts: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("GetAccounts returned %d accounts, want 2", len(got))
	}
}

func TestUpsertAccounts_Update(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)

	acc := sampleAccount("acc-upd")
	if err := d.UpsertAccounts([]models.Account{acc}); err != nil {
		t.Fatalf("initial UpsertAccounts: %v", err)
	}

	// Update the name.
	acc.Name = "Updated Name"
	if err := d.UpsertAccounts([]models.Account{acc}); err != nil {
		t.Fatalf("update UpsertAccounts: %v", err)
	}

	got, err := d.GetAccounts()
	if err != nil {
		t.Fatalf("GetAccounts: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 account, got %d", len(got))
	}
	if got[0].Name != "Updated Name" {
		t.Errorf("Name = %q, want %q", got[0].Name, "Updated Name")
	}
}

func TestUpsertAccounts_Empty(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	if err := d.UpsertAccounts(nil); err != nil {
		t.Errorf("UpsertAccounts(nil) unexpected error: %v", err)
	}
}

func TestGetAccounts_OrderedByInstitutionName(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)

	accounts := []models.Account{
		{AccountID: "z", Institution: "Zeta Bank", Name: "Savings", Type: "savings", Currency: "USD"},
		{AccountID: "a", Institution: "Alpha Bank", Name: "Checking", Type: "checking", Currency: "USD"},
	}
	if err := d.UpsertAccounts(accounts); err != nil {
		t.Fatalf("UpsertAccounts: %v", err)
	}

	got, err := d.GetAccounts()
	if err != nil {
		t.Fatalf("GetAccounts: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
	// Alpha Bank should come first.
	if got[0].Institution != "Alpha Bank" {
		t.Errorf("got[0].Institution = %q, want Alpha Bank", got[0].Institution)
	}
}

func TestUpsertTransactions_Insert(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	if err := d.UpsertAccounts([]models.Account{sampleAccount("acc-1")}); err != nil {
		t.Fatalf("UpsertAccounts: %v", err)
	}

	txns := []models.Transaction{
		sampleTransaction("acc-1", "src-1", "fp-1", -1000),
		sampleTransaction("acc-1", "src-2", "fp-2", -2000),
	}
	ins, upd, err := d.UpsertTransactions(txns)
	if err != nil {
		t.Fatalf("UpsertTransactions: %v", err)
	}
	if ins != 2 {
		t.Errorf("inserted = %d, want 2", ins)
	}
	if upd != 0 {
		t.Errorf("updated = %d, want 0", upd)
	}
}

func TestUpsertTransactions_Duplicate(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	if err := d.UpsertAccounts([]models.Account{sampleAccount("acc-1")}); err != nil {
		t.Fatalf("UpsertAccounts: %v", err)
	}

	txn := sampleTransaction("acc-1", "src-dup", "fp-dup", -500)
	// Insert once.
	ins, _, err := d.UpsertTransactions([]models.Transaction{txn})
	if err != nil || ins != 1 {
		t.Fatalf("first insert: ins=%d err=%v", ins, err)
	}
	// Insert same transaction again — should be a no-op (same data).
	ins2, upd2, err := d.UpsertTransactions([]models.Transaction{txn})
	if err != nil {
		t.Fatalf("second insert: %v", err)
	}
	if ins2 != 0 || upd2 != 0 {
		t.Errorf("duplicate insert: ins=%d upd=%d, want both 0", ins2, upd2)
	}
}

func TestUpsertTransactions_Update(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	if err := d.UpsertAccounts([]models.Account{sampleAccount("acc-1")}); err != nil {
		t.Fatalf("UpsertAccounts: %v", err)
	}

	txn := sampleTransaction("acc-1", "src-upd", "fp-upd", -1000)
	if _, _, err := d.UpsertTransactions([]models.Transaction{txn}); err != nil {
		t.Fatalf("initial insert: %v", err)
	}

	// Change the amount — same source_txn_id but different amount_cents.
	txn.AmountCents = -9999
	_, upd, err := d.UpsertTransactions([]models.Transaction{txn})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd != 1 {
		t.Errorf("updated = %d, want 1", upd)
	}
}

func TestUpsertTransactions_NoSourceTxnID(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	if err := d.UpsertAccounts([]models.Account{sampleAccount("acc-1")}); err != nil {
		t.Fatalf("UpsertAccounts: %v", err)
	}

	txn := models.Transaction{
		AccountID:   "acc-1",
		PostedAt:    time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
		AmountCents: -750,
		Currency:    "USD",
		Description: "Fallback fingerprint",
		Fingerprint: "fp-no-src",
		// SourceTxnID intentionally empty
	}
	ins, _, err := d.UpsertTransactions([]models.Transaction{txn})
	if err != nil {
		t.Fatalf("UpsertTransactions: %v", err)
	}
	if ins != 1 {
		t.Errorf("ins = %d, want 1", ins)
	}
}

func TestGetTransactions_DateRangeFiltering(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	if err := d.UpsertAccounts([]models.Account{sampleAccount("acc-1")}); err != nil {
		t.Fatalf("UpsertAccounts: %v", err)
	}

	txns := []models.Transaction{
		{AccountID: "acc-1", PostedAt: time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC), AmountCents: -100, Currency: "USD", Fingerprint: "fp-jan"},
		{AccountID: "acc-1", PostedAt: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC), AmountCents: -200, Currency: "USD", Fingerprint: "fp-mar"},
		{AccountID: "acc-1", PostedAt: time.Date(2024, 5, 20, 0, 0, 0, 0, time.UTC), AmountCents: -300, Currency: "USD", Fingerprint: "fp-may"},
	}
	if _, _, err := d.UpsertTransactions(txns); err != nil {
		t.Fatalf("UpsertTransactions: %v", err)
	}

	// Range covers only March.
	start := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)
	got, err := d.GetTransactions(start, end)
	if err != nil {
		t.Fatalf("GetTransactions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("GetTransactions returned %d txns, want 1", len(got))
	}
	if got[0].Fingerprint != "fp-mar" {
		t.Errorf("unexpected fingerprint %q", got[0].Fingerprint)
	}
}

func TestGetTransactions_EndIsExclusive(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	if err := d.UpsertAccounts([]models.Account{sampleAccount("acc-1")}); err != nil {
		t.Fatalf("UpsertAccounts: %v", err)
	}

	txn := models.Transaction{
		AccountID:   "acc-1",
		PostedAt:    time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC),
		AmountCents: -100,
		Currency:    "USD",
		Fingerprint: "fp-boundary",
	}
	if _, _, err := d.UpsertTransactions([]models.Transaction{txn}); err != nil {
		t.Fatalf("UpsertTransactions: %v", err)
	}

	// End date is 2024-04-01 — the transaction is on that date, so it should NOT appear.
	start := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)
	got, err := d.GetTransactions(start, end)
	if err != nil {
		t.Fatalf("GetTransactions: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("GetTransactions: end is exclusive but returned %d txns, want 0", len(got))
	}
}

func TestRecordRun_AndRunsInLast24Hours(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)

	count, err := d.RunsInLast24Hours()
	if err != nil {
		t.Fatalf("RunsInLast24Hours (empty): %v", err)
	}
	if count != 0 {
		t.Errorf("RunsInLast24Hours = %d, want 0 on empty DB", count)
	}

	if err := d.RecordRun(30, 100, 95, 5); err != nil {
		t.Fatalf("RecordRun: %v", err)
	}

	count, err = d.RunsInLast24Hours()
	if err != nil {
		t.Fatalf("RunsInLast24Hours: %v", err)
	}
	if count != 1 {
		t.Errorf("RunsInLast24Hours = %d, want 1", count)
	}
}

func TestRecordRun_MultipleCounted(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)

	for i := 0; i < 3; i++ {
		if err := d.RecordRun(7, 10, 10, 0); err != nil {
			t.Fatalf("RecordRun %d: %v", i, err)
		}
	}

	count, err := d.RunsInLast24Hours()
	if err != nil {
		t.Fatalf("RunsInLast24Hours: %v", err)
	}
	if count != 3 {
		t.Errorf("RunsInLast24Hours = %d, want 3", count)
	}
}

func TestRecentRuns(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)

	for i := 0; i < 5; i++ {
		if err := d.RecordRun(30, i*10, i*10, 0); err != nil {
			t.Fatalf("RecordRun: %v", err)
		}
	}

	runs, err := d.RecentRuns(3)
	if err != nil {
		t.Fatalf("RecentRuns: %v", err)
	}
	if len(runs) != 3 {
		t.Errorf("RecentRuns(3) returned %d, want 3", len(runs))
	}
}

func TestSaveAlertAction_InsertAndGet(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)

	aa := models.AlertAction{
		AlertKey:    "sub:netflix",
		Action:      "dismiss",
		MerchantNorm: "netflix",
		PatternType: "subscription",
		Notes:       "expected charge",
	}
	if err := d.SaveAlertAction(aa); err != nil {
		t.Fatalf("SaveAlertAction: %v", err)
	}

	actions, err := d.GetAlertActions()
	if err != nil {
		t.Fatalf("GetAlertActions: %v", err)
	}
	action, ok := actions["sub:netflix"]
	if !ok {
		t.Fatal("GetAlertActions: key 'sub:netflix' not found")
	}
	if action != "dismiss" {
		t.Errorf("action = %q, want %q", action, "dismiss")
	}
}

func TestSaveAlertAction_Upsert(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)

	aa := models.AlertAction{AlertKey: "sub:spotify", Action: "snooze", MerchantNorm: "spotify", PatternType: "subscription"}
	if err := d.SaveAlertAction(aa); err != nil {
		t.Fatalf("initial SaveAlertAction: %v", err)
	}

	// Update the action.
	aa.Action = "dismiss"
	if err := d.SaveAlertAction(aa); err != nil {
		t.Fatalf("upsert SaveAlertAction: %v", err)
	}

	actions, err := d.GetAlertActions()
	if err != nil {
		t.Fatalf("GetAlertActions: %v", err)
	}
	if actions["sub:spotify"] != "dismiss" {
		t.Errorf("action after upsert = %q, want dismiss", actions["sub:spotify"])
	}
	// Should still be exactly one record for that key.
	count := 0
	for k := range actions {
		if k == "sub:spotify" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 record for key, got %d", count)
	}
}

func TestTransactionCount(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	if err := d.UpsertAccounts([]models.Account{sampleAccount("acc-1")}); err != nil {
		t.Fatalf("UpsertAccounts: %v", err)
	}

	n, err := d.TransactionCount()
	if err != nil {
		t.Fatalf("TransactionCount: %v", err)
	}
	if n != 0 {
		t.Errorf("TransactionCount on empty DB = %d, want 0", n)
	}

	txns := []models.Transaction{
		sampleTransaction("acc-1", "s1", "fp1", -100),
		sampleTransaction("acc-1", "s2", "fp2", -200),
	}
	if _, _, err := d.UpsertTransactions(txns); err != nil {
		t.Fatalf("UpsertTransactions: %v", err)
	}

	n, err = d.TransactionCount()
	if err != nil {
		t.Fatalf("TransactionCount: %v", err)
	}
	if n != 2 {
		t.Errorf("TransactionCount = %d, want 2", n)
	}
}
