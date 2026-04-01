package classify_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/arclighteng/fin-go/internal/classify"
	"github.com/arclighteng/fin-go/internal/db"
	_ "modernc.org/sqlite"
)

// newTransferDB opens an in-memory SQLite database with the fin schema and
// returns the raw *sql.DB that DetectTransferPairs requires.
func newTransferDB(t *testing.T) *sql.DB {
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

// insertLeg inserts a single transaction leg into the database for testing.
func insertLeg(t *testing.T, sqlDB *sql.DB, fingerprint, accountID string, postedAt time.Time, amountCents int64, merchant string) {
	t.Helper()

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := sqlDB.Exec(`
		INSERT INTO transactions(account_id, posted_at, amount_cents, currency, description, merchant,
		  source_txn_id, fingerprint, pending, created_at, updated_at)
		VALUES (?, ?, ?, 'USD', ?, ?, NULL, ?, 0, ?, ?)`,
		accountID,
		postedAt.Format("2006-01-02"),
		amountCents,
		merchant, // description (same as merchant for simplicity)
		merchant,
		fingerprint,
		now, now,
	)
	if err != nil {
		t.Fatalf("insertLeg %q: %v", fingerprint, err)
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestDetectTransferPairs_SameDaySameAmount(t *testing.T) {
	t.Parallel()

	sqlDB := newTransferDB(t)

	day := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	insertLeg(t, sqlDB, "fp-out", "acct-savings", day, -100000, "transfer") // -$1000
	insertLeg(t, sqlDB, "fp-in", "acct-checking", day, 100000, "transfer")  // +$1000

	result := classify.DetectTransferPairs(sqlDB,
		day.AddDate(0, 0, -1), day.AddDate(0, 0, 1),
		3, 300,
	)

	if len(result.MatchedPairs) != 1 {
		t.Fatalf("want 1 matched pair, got %d", len(result.MatchedPairs))
	}
	pair := result.MatchedPairs[0]
	if pair.Outflow.Fingerprint != "fp-out" {
		t.Errorf("outflow fingerprint: want fp-out, got %q", pair.Outflow.Fingerprint)
	}
	if pair.Inflow.Fingerprint != "fp-in" {
		t.Errorf("inflow fingerprint: want fp-in, got %q", pair.Inflow.Fingerprint)
	}
	if pair.AmountDiffCents != 0 {
		t.Errorf("amount diff: want 0, got %d", pair.AmountDiffCents)
	}
	if len(result.UnmatchedOutflows) != 0 || len(result.UnmatchedInflows) != 0 {
		t.Errorf("want no unmatched legs, got %d outflows, %d inflows",
			len(result.UnmatchedOutflows), len(result.UnmatchedInflows))
	}
}

func TestDetectTransferPairs_NoMatch(t *testing.T) {
	t.Parallel()

	sqlDB := newTransferDB(t)

	day := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	// All outflows from the same account -- no inflow to pair with.
	insertLeg(t, sqlDB, "fp-out-1", "acct-a", day, -50000, "grocery store")
	insertLeg(t, sqlDB, "fp-out-2", "acct-a", day, -20000, "coffee shop")

	result := classify.DetectTransferPairs(sqlDB,
		day.AddDate(0, 0, -1), day.AddDate(0, 0, 1),
		3, 300,
	)

	if len(result.MatchedPairs) != 0 {
		t.Errorf("want 0 matched pairs, got %d", len(result.MatchedPairs))
	}
	if len(result.UnmatchedOutflows) != 2 {
		t.Errorf("want 2 unmatched outflows, got %d", len(result.UnmatchedOutflows))
	}
}

func TestDetectTransferPairs_DuplicateAmounts(t *testing.T) {
	t.Parallel()

	sqlDB := newTransferDB(t)

	// Two outflows of the same amount on different accounts, one inflow.
	// The algorithm should match the first eligible outflow.
	day := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	insertLeg(t, sqlDB, "fp-out-1", "acct-savings", day, -50000, "transfer") // -$500
	insertLeg(t, sqlDB, "fp-out-2", "acct-brokerage", day, -50000, "transfer") // -$500 duplicate
	insertLeg(t, sqlDB, "fp-in-1", "acct-checking", day, 50000, "transfer")   // +$500 (one match)

	result := classify.DetectTransferPairs(sqlDB,
		day.AddDate(0, 0, -1), day.AddDate(0, 0, 1),
		3, 300,
	)

	// Exactly one pair should form; one outflow remains unmatched.
	if len(result.MatchedPairs) != 1 {
		t.Errorf("want 1 matched pair, got %d", len(result.MatchedPairs))
	}
	if len(result.UnmatchedOutflows) != 1 {
		t.Errorf("want 1 unmatched outflow, got %d", len(result.UnmatchedOutflows))
	}

	// The inflow must be consumed (not left unmatched).
	if len(result.UnmatchedInflows) != 0 {
		t.Errorf("want 0 unmatched inflows, got %d", len(result.UnmatchedInflows))
	}

	// The matched pair fingerprints must not be duplicates.
	pair := result.MatchedPairs[0]
	if pair.Inflow.Fingerprint != "fp-in-1" {
		t.Errorf("inflow fingerprint: want fp-in-1, got %q", pair.Inflow.Fingerprint)
	}
}

func TestDetectTransferPairs_ZeroAmount(t *testing.T) {
	t.Parallel()

	sqlDB := newTransferDB(t)

	// Zero-value transactions must not be paired (amount == 0 falls into
	// neither the outflows nor the inflows bucket).
	day := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	insertLeg(t, sqlDB, "fp-zero-1", "acct-a", day, 0, "fee waiver")
	insertLeg(t, sqlDB, "fp-zero-2", "acct-b", day, 0, "fee waiver")

	result := classify.DetectTransferPairs(sqlDB,
		day.AddDate(0, 0, -1), day.AddDate(0, 0, 1),
		3, 300,
	)

	if len(result.MatchedPairs) != 0 {
		t.Errorf("zero-amount legs: want 0 matched pairs, got %d", len(result.MatchedPairs))
	}
	// Zero-value transactions are bucketed into neither list, so unmatched counts
	// should also be 0.
	if len(result.UnmatchedOutflows) != 0 || len(result.UnmatchedInflows) != 0 {
		t.Errorf("zero-amount legs: want 0 unmatched, got out=%d in=%d",
			len(result.UnmatchedOutflows), len(result.UnmatchedInflows))
	}
}

func TestDetectTransferPairs_CrossDay(t *testing.T) {
	t.Parallel()

	sqlDB := newTransferDB(t)

	// Outflow on day 0, inflow on day 2 -- within the 3-day tolerance window.
	day0 := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	day2 := day0.AddDate(0, 0, 2)

	insertLeg(t, sqlDB, "fp-out", "acct-savings", day0, -200000, "ach transfer")  // -$2000
	insertLeg(t, sqlDB, "fp-in", "acct-checking", day2, 200000, "ach transfer")   // +$2000

	result := classify.DetectTransferPairs(sqlDB,
		day0.AddDate(0, 0, -1), day2.AddDate(0, 0, 1),
		3, 300,
	)

	if len(result.MatchedPairs) != 1 {
		t.Fatalf("cross-day match: want 1 pair, got %d", len(result.MatchedPairs))
	}
	pair := result.MatchedPairs[0]
	if pair.Outflow.Fingerprint != "fp-out" || pair.Inflow.Fingerprint != "fp-in" {
		t.Errorf("cross-day match: unexpected fingerprints out=%q in=%q",
			pair.Outflow.Fingerprint, pair.Inflow.Fingerprint)
	}
}

func TestDetectTransferPairs_SameAccount(t *testing.T) {
	t.Parallel()

	sqlDB := newTransferDB(t)

	// Outflow and inflow on the SAME account must not pair.
	day := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	insertLeg(t, sqlDB, "fp-out", "acct-same", day, -75000, "internal move")
	insertLeg(t, sqlDB, "fp-in", "acct-same", day, 75000, "internal move")

	result := classify.DetectTransferPairs(sqlDB,
		day.AddDate(0, 0, -1), day.AddDate(0, 0, 1),
		3, 300,
	)

	if len(result.MatchedPairs) != 0 {
		t.Errorf("same-account legs: want 0 matched pairs, got %d", len(result.MatchedPairs))
	}
	// Both legs are left unmatched.
	if len(result.UnmatchedOutflows) != 1 {
		t.Errorf("same-account: want 1 unmatched outflow, got %d", len(result.UnmatchedOutflows))
	}
	if len(result.UnmatchedInflows) != 1 {
		t.Errorf("same-account: want 1 unmatched inflow, got %d", len(result.UnmatchedInflows))
	}
}

// ---------------------------------------------------------------------------
// Helper method tests
// ---------------------------------------------------------------------------

func TestTransferPair_NetCents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		outflowCents int64
		inflowCents  int64
		wantNet      int64
	}{
		{"balanced", -100000, 100000, 0},
		{"ach fee deducted", -100000, 99700, -300},
		{"inflow larger", -100000, 100500, 500},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pair := classify.TransferPair{
				Outflow: classify.TransferLeg{AmountCents: tc.outflowCents},
				Inflow:  classify.TransferLeg{AmountCents: tc.inflowCents},
			}
			if got := pair.NetCents(); got != tc.wantNet {
				t.Errorf("NetCents: want %d, got %d", tc.wantNet, got)
			}
		})
	}
}

func TestTransferPair_IsBalanced(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		outflowCents int64
		inflowCents  int64
		wantBalanced bool
	}{
		{"exact match", -100000, 100000, true},
		{"within $5", -100000, 99600, true},  // $4 difference
		{"exactly $5", -100000, 99500, true},
		{"over $5", -100000, 99400, false},   // $6 difference
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pair := classify.TransferPair{
				Outflow: classify.TransferLeg{AmountCents: tc.outflowCents},
				Inflow:  classify.TransferLeg{AmountCents: tc.inflowCents},
			}
			if got := pair.IsBalanced(); got != tc.wantBalanced {
				t.Errorf("IsBalanced: want %v, got %v", tc.wantBalanced, got)
			}
		})
	}
}

func TestTransferPairingResult_PairedFingerprints(t *testing.T) {
	t.Parallel()

	result := &classify.TransferPairingResult{
		MatchedPairs: []classify.TransferPair{
			{
				PairID:  "00000001",
				Outflow: classify.TransferLeg{Fingerprint: "fp-out-1"},
				Inflow:  classify.TransferLeg{Fingerprint: "fp-in-1"},
			},
		},
	}

	fps := result.PairedFingerprints()
	for _, fp := range []string{"fp-out-1", "fp-in-1"} {
		if !fps[fp] {
			t.Errorf("PairedFingerprints: want %q present, got absent", fp)
		}
	}
	if fps["fp-other"] {
		t.Error("PairedFingerprints: want fp-other absent, got present")
	}
}

func TestTransferPairingResult_PairID(t *testing.T) {
	t.Parallel()

	result := &classify.TransferPairingResult{
		MatchedPairs: []classify.TransferPair{
			{
				PairID:  "00000001",
				Outflow: classify.TransferLeg{Fingerprint: "fp-out"},
				Inflow:  classify.TransferLeg{Fingerprint: "fp-in"},
			},
		},
	}

	if got := result.PairID("fp-out"); got != "00000001" {
		t.Errorf("PairID(fp-out): want 00000001, got %q", got)
	}
	if got := result.PairID("fp-in"); got != "00000001" {
		t.Errorf("PairID(fp-in): want 00000001, got %q", got)
	}
	if got := result.PairID("fp-unknown"); got != "" {
		t.Errorf("PairID(fp-unknown): want empty, got %q", got)
	}
}
