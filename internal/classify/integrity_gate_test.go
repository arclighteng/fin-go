package classify_test

import (
	"testing"
	"time"

	"github.com/arclighteng/fin-go/internal/classify"
	"github.com/arclighteng/fin-go/internal/db"
	"github.com/arclighteng/fin-go/internal/models"
	"github.com/arclighteng/fin-go/internal/reconciliation"
)

// TestIsActionable_ReconciliationFailedSuppressesAdvice is the core ADA-111
// boundary proof: a statement that does not reconcile drops below "Excellent"
// and suppresses recommendations, where the old `>= 0.8` gate left it actionable.
func TestIsActionable_ReconciliationFailedSuppressesAdvice(t *testing.T) {
	ir := classify.IntegrityReport{
		Flags:                    []classify.IntegrityFlag{classify.FlagReconciliationFailed},
		ReconciliationDeltaCents: 5000,
	}

	score := ir.Score()
	// FlagReconciliationFailed carries a 0.20 penalty → score ~0.80.
	if score < 0.79 || score > 0.81 {
		t.Fatalf("Score = %v, want ~0.80", score)
	}
	// "Excellent" is score >= 0.95; a reconciliation failure must fall below it.
	if score >= 0.95 {
		t.Fatalf("reconciliation failure should drop below Excellent, got score %v", score)
	}
	// And recommendations must be suppressed (was actionable at old >= 0.8 gate).
	if ir.IsActionable() {
		t.Fatal("IsActionable must be false when a statement does not reconcile")
	}
}

// TestIsActionable_ExactBoundaryNotActionable proves the strict threshold: a
// report sitting at exactly 0.80 (two 0.10 penalties, no critical flag) is no
// longer actionable.
func TestIsActionable_ExactBoundaryNotActionable(t *testing.T) {
	ir := classify.IntegrityReport{
		Flags: []classify.IntegrityFlag{classify.FlagUnclassifiedCredit, classify.FlagPendingInTotals},
	}
	score := ir.Score() // 1.0 - 0.10 - 0.10 = 0.80
	if score < 0.79 || score > 0.81 {
		t.Fatalf("Score = %v, want ~0.80", score)
	}
	if ir.IsActionable() {
		t.Fatal("score exactly 0.80 must not be actionable under the strict > 0.8 gate")
	}
}

// TestIsActionable_CleanReportActionable guards against over-gating: a clean
// report is still actionable.
func TestIsActionable_CleanReportActionable(t *testing.T) {
	ir := classify.IntegrityReport{}
	if !ir.IsActionable() {
		t.Fatal("a clean report (score 1.0) must remain actionable")
	}
}

// TestReportPeriod_SetsReconciliationFlag is the ADA-111 integration proof: an
// unresolved reconciliation discrepancy in the database causes the canonical
// report to raise FlagReconciliationFailed and suppress recommendations.
func TestReportPeriod_SetsReconciliationFlag(t *testing.T) {
	database, err := db.Connect(":memory:")
	if err != nil {
		t.Fatalf("db.Connect: %v", err)
	}
	defer database.Close()
	if err := database.Init(); err != nil {
		t.Fatalf("database.Init: %v", err)
	}

	if err := database.UpsertAccounts([]models.Account{
		{AccountID: "acct-1", Institution: "Bank", Name: "Checking", Type: "checking", Currency: "USD"},
	}); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	if _, _, err := database.UpsertTransactions([]models.Transaction{
		{AccountID: "acct-1", PostedAt: time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC), AmountCents: -4500, Currency: "USD", Description: "COFFEE", Merchant: "COFFEE", Fingerprint: "fp-1"},
	}); err != nil {
		t.Fatalf("seed transaction: %v", err)
	}

	udb := database.Underlying()
	if err := reconciliation.EnsureSchema(udb); err != nil {
		t.Fatalf("ensure reconciliation schema: %v", err)
	}
	if _, err := udb.Exec(
		`INSERT INTO reconciliation_events
		 (account_id, statement_date, statement_balance_cents, calculated_balance_cents, delta_cents, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"acct-1", "2026-04-30", 100000, 95000, 5000, "discrepancy",
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		t.Fatalf("insert discrepancy: %v", err)
	}

	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	report := classify.ReportPeriod(udb, start, end, false, nil)

	found := false
	for _, f := range report.Integrity.Flags {
		if f == classify.FlagReconciliationFailed {
			found = true
		}
	}
	if !found {
		t.Fatalf("ReportPeriod did not raise FlagReconciliationFailed; flags=%v", report.Integrity.Flags)
	}
	if report.Integrity.ReconciliationDeltaCents != 5000 {
		t.Errorf("ReconciliationDeltaCents = %d, want 5000", report.Integrity.ReconciliationDeltaCents)
	}
	if report.Integrity.Score() >= 0.95 {
		t.Errorf("score should drop below Excellent, got %v", report.Integrity.Score())
	}
	if report.Integrity.IsActionable() {
		t.Error("recommendations must be suppressed while a statement does not reconcile")
	}
}
