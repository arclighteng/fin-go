package classify_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/arclighteng/fin-go/internal/classify"
	"github.com/arclighteng/fin-go/internal/db"
)

// newRefundDB opens an in-memory SQLite database initialised with the fin
// schema and returns the raw *sql.DB that DetectRefundMatches requires.
func newRefundDB(t *testing.T) *sql.DB {
	t.Helper()

	database, err := db.Connect(":memory:")
	if err != nil {
		t.Fatalf("db.Connect: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	if err := database.Init(); err != nil {
		t.Fatalf("database.Init: %v", err)
	}

	return database.Underlying()
}

// insertRefundTxn inserts a transaction for refund-matching tests.
func insertRefundTxn(t *testing.T, sqlDB *sql.DB, fingerprint, accountID string, postedAt time.Time, amountCents int64, merchant string) {
	t.Helper()

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := sqlDB.Exec(`
		INSERT INTO transactions(account_id, posted_at, amount_cents, currency, description, merchant,
		  source_txn_id, fingerprint, pending, created_at, updated_at)
		VALUES (?, ?, ?, 'USD', ?, ?, NULL, ?, 0, ?, ?)`,
		accountID,
		postedAt.Format("2006-01-02"),
		amountCents,
		merchant,
		merchant,
		fingerprint,
		now, now,
	)
	if err != nil {
		t.Fatalf("insertRefundTxn %q: %v", fingerprint, err)
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestDetectRefundMatches_ExactMatch(t *testing.T) {
	t.Parallel()

	sqlDB := newRefundDB(t)

	// Expense three days ago; refund today for the exact same amount.
	today := time.Date(2024, 4, 10, 0, 0, 0, 0, time.UTC)
	threeDaysAgo := today.AddDate(0, 0, -3)

	insertRefundTxn(t, sqlDB, "fp-expense", "acct-a", threeDaysAgo, -4999, "amazon")
	insertRefundTxn(t, sqlDB, "fp-refund", "acct-a", today, 4999, "amazon refund")

	result := classify.DetectRefundMatches(sqlDB,
		today.AddDate(0, 0, -1), today.AddDate(0, 0, 1),
		90, 5.0,
	)

	if len(result.MatchedRefunds) != 1 {
		t.Fatalf("exact match: want 1 matched refund, got %d", len(result.MatchedRefunds))
	}
	m := result.MatchedRefunds[0]
	if m.RefundFingerprint != "fp-refund" {
		t.Errorf("refund fingerprint: want fp-refund, got %q", m.RefundFingerprint)
	}
	if m.ExpenseFingerprint != "fp-expense" {
		t.Errorf("expense fingerprint: want fp-expense, got %q", m.ExpenseFingerprint)
	}
	if !m.IsFullRefund() {
		t.Errorf("IsFullRefund: want true (refund=%d, expense=%d)",
			m.RefundAmountCents, m.ExpenseAmountCents)
	}
}

func TestDetectRefundMatches_PartialRefund(t *testing.T) {
	t.Parallel()

	sqlDB := newRefundDB(t)

	today := time.Date(2024, 4, 10, 0, 0, 0, 0, time.UTC)
	fiveDaysAgo := today.AddDate(0, 0, -5)

	// Expense of $100, refund of $30 (partial).
	insertRefundTxn(t, sqlDB, "fp-expense", "acct-a", fiveDaysAgo, -10000, "target")
	insertRefundTxn(t, sqlDB, "fp-refund", "acct-a", today, 3000, "target refund")

	result := classify.DetectRefundMatches(sqlDB,
		today.AddDate(0, 0, -1), today.AddDate(0, 0, 1),
		90, 5.0,
	)

	if len(result.MatchedRefunds) != 1 {
		t.Fatalf("partial refund: want 1 match, got %d", len(result.MatchedRefunds))
	}
	m := result.MatchedRefunds[0]
	if !m.IsPartialRefund() {
		t.Errorf("IsPartialRefund: want true (refund=%d, expense=%d)",
			m.RefundAmountCents, m.ExpenseAmountCents)
	}
	if m.IsFullRefund() {
		t.Errorf("IsFullRefund: want false for partial refund")
	}
}

func TestDetectRefundMatches_NoExpense(t *testing.T) {
	t.Parallel()

	sqlDB := newRefundDB(t)

	// A credit with a refund keyword but no matching expense in the DB.
	today := time.Date(2024, 4, 10, 0, 0, 0, 0, time.UTC)
	insertRefundTxn(t, sqlDB, "fp-orphan-refund", "acct-a", today, 5000, "refund")

	result := classify.DetectRefundMatches(sqlDB,
		today.AddDate(0, 0, -1), today.AddDate(0, 0, 1),
		90, 5.0,
	)

	if len(result.MatchedRefunds) != 0 {
		t.Errorf("no expense: want 0 matched refunds, got %d", len(result.MatchedRefunds))
	}
	// The orphan refund should appear in UnmatchedRefunds because it has a
	// refund keyword but no expense to pair with.
	if len(result.UnmatchedRefunds) != 1 {
		t.Errorf("no expense: want 1 unmatched refund fingerprint, got %d", len(result.UnmatchedRefunds))
	}
	if result.UnmatchedRefunds[0] != "fp-orphan-refund" {
		t.Errorf("unmatched refund: want fp-orphan-refund, got %q", result.UnmatchedRefunds[0])
	}
}

func TestDetectRefundMatches_TooOld(t *testing.T) {
	t.Parallel()

	sqlDB := newRefundDB(t)

	// Expense is 120 days before the refund window -- outside the 90-day lookback.
	today := time.Date(2024, 4, 10, 0, 0, 0, 0, time.UTC)
	tooOld := today.AddDate(0, 0, -120)

	insertRefundTxn(t, sqlDB, "fp-old-expense", "acct-a", tooOld, -8000, "walmart")
	insertRefundTxn(t, sqlDB, "fp-refund", "acct-a", today, 8000, "walmart refund")

	result := classify.DetectRefundMatches(sqlDB,
		today.AddDate(0, 0, -1), today.AddDate(0, 0, 1),
		90, 5.0,
	)

	// The expense is beyond the lookback window; no match should form.
	if len(result.MatchedRefunds) != 0 {
		t.Errorf("too-old expense: want 0 matches, got %d", len(result.MatchedRefunds))
	}
}

func TestDetectRefundMatches_MultipleRefunds(t *testing.T) {
	t.Parallel()

	sqlDB := newRefundDB(t)

	// One expense, two refunds arriving on different days.
	// Each refund is for a partial amount and has a refund keyword.
	// The first (better-scoring) refund should be matched; the second
	// should be unmatched because the expense is already consumed.
	today := time.Date(2024, 4, 10, 0, 0, 0, 0, time.UTC)
	yesterday := today.AddDate(0, 0, -1)
	tenDaysAgo := today.AddDate(0, 0, -10)

	insertRefundTxn(t, sqlDB, "fp-expense", "acct-a", tenDaysAgo, -20000, "apple store")
	insertRefundTxn(t, sqlDB, "fp-refund-1", "acct-a", yesterday, 5000, "apple store refund")
	insertRefundTxn(t, sqlDB, "fp-refund-2", "acct-a", today, 5000, "apple store refund")

	result := classify.DetectRefundMatches(sqlDB,
		yesterday.AddDate(0, 0, -1), today.AddDate(0, 0, 1),
		90, 5.0,
	)

	// At most one refund should be matched to the single expense.
	if len(result.MatchedRefunds) > 1 {
		t.Errorf("multiple refunds: want at most 1 match, got %d", len(result.MatchedRefunds))
	}

	// At least one refund should be matched (not zero).
	if len(result.MatchedRefunds) == 0 {
		t.Errorf("multiple refunds: want at least 1 match, got 0")
	}
}

// ---------------------------------------------------------------------------
// Helper method tests
// ---------------------------------------------------------------------------

func TestRefundMatch_IsFullRefund(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		refundCents  int64
		expenseCents int64 // negative
		wantFull     bool
	}{
		{"exact", 5000, -5000, true},
		{"over-refund", 5100, -5000, true},
		{"partial", 2500, -5000, false},
		{"zero refund", 0, -5000, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := classify.RefundMatch{
				RefundAmountCents:  tc.refundCents,
				ExpenseAmountCents: tc.expenseCents,
			}
			if got := m.IsFullRefund(); got != tc.wantFull {
				t.Errorf("IsFullRefund: want %v, got %v (refund=%d expense=%d)",
					tc.wantFull, got, tc.refundCents, tc.expenseCents)
			}
		})
	}
}

func TestRefundMatch_IsPartialRefund(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		refundCents  int64
		expenseCents int64
		wantPartial  bool
	}{
		{"partial", 2500, -5000, true},
		{"full", 5000, -5000, false},
		{"over", 5100, -5000, false},
		{"zero", 0, -5000, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := classify.RefundMatch{
				RefundAmountCents:  tc.refundCents,
				ExpenseAmountCents: tc.expenseCents,
			}
			if got := m.IsPartialRefund(); got != tc.wantPartial {
				t.Errorf("IsPartialRefund: want %v, got %v (refund=%d expense=%d)",
					tc.wantPartial, got, tc.refundCents, tc.expenseCents)
			}
		})
	}
}

func TestRefundMatchingResult_ExpenseForRefund(t *testing.T) {
	t.Parallel()

	result := &classify.RefundMatchingResult{
		MatchedRefunds: []classify.RefundMatch{
			{
				RefundFingerprint:  "fp-refund",
				ExpenseFingerprint: "fp-expense",
			},
		},
	}

	if got := result.ExpenseForRefund("fp-refund"); got != "fp-expense" {
		t.Errorf("ExpenseForRefund: want fp-expense, got %q", got)
	}
	if got := result.ExpenseForRefund("fp-unknown"); got != "" {
		t.Errorf("ExpenseForRefund (unknown): want empty, got %q", got)
	}
}

func TestRefundMatchingResult_MatchedFingerprints(t *testing.T) {
	t.Parallel()

	result := &classify.RefundMatchingResult{
		MatchedRefunds: []classify.RefundMatch{
			{RefundFingerprint: "fp-r1"},
			{RefundFingerprint: "fp-r2"},
		},
	}

	fps := result.MatchedFingerprints()
	for _, fp := range []string{"fp-r1", "fp-r2"} {
		if !fps[fp] {
			t.Errorf("MatchedFingerprints: want %q present, got absent", fp)
		}
	}
	if fps["fp-other"] {
		t.Error("MatchedFingerprints: want fp-other absent, got present")
	}
}
