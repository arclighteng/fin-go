package classify

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newRecurringTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`
		CREATE TABLE transactions (
			fingerprint  TEXT PRIMARY KEY,
			account_id   TEXT,
			posted_at    TEXT NOT NULL,
			amount_cents INTEGER NOT NULL,
			merchant     TEXT,
			description  TEXT,
			pending      INTEGER DEFAULT 0
		);`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

// insertCharge adds one posted expense (negative amount) for a merchant.
func insertCharge(t *testing.T, db *sql.DB, fp, merchant string, date time.Time, amountCents int64) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO transactions(fingerprint, account_id, posted_at, amount_cents, merchant, pending)
		 VALUES (?, 'acct', ?, ?, ?, 0)`,
		fp, date.Format("2006-01-02"), amountCents, merchant,
	)
	if err != nil {
		t.Fatalf("insert charge: %v", err)
	}
}

func TestDetectRecurring_FindsMonthlySubscription(t *testing.T) {
	db := newRecurringTestDB(t)

	// Netflix: six monthly charges of $15.99 on the 15th.
	for i := 0; i < 6; i++ {
		insertCharge(t, db, fmt.Sprintf("nf-%d", i), "Netflix",
			time.Date(2026, time.Month(1+i), 15, 0, 0, 0, 0, time.UTC), -1599)
	}

	got, err := DetectRecurring(db, RecurringOptions{})
	if err != nil {
		t.Fatalf("DetectRecurring: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 detection, got %d: %+v", len(got), got)
	}
	d := got[0]
	if d.MerchantNorm != "netflix" {
		t.Errorf("merchant: want netflix, got %q", d.MerchantNorm)
	}
	if d.Cadence != "monthly" {
		t.Errorf("cadence: want monthly, got %q", d.Cadence)
	}
	if d.AmountCents != 1599 {
		t.Errorf("amount: want 1599, got %d", d.AmountCents)
	}
	if d.Direction != "expense" {
		t.Errorf("direction: want expense, got %q", d.Direction)
	}
	if d.DayOfMonth != 15 {
		t.Errorf("day_of_month: want 15, got %d", d.DayOfMonth)
	}
	if d.Confidence < 0.5 {
		t.Errorf("confidence: want >= 0.5, got %f", d.Confidence)
	}
}

func TestDetectRecurring_IgnoresVariableIrregularSpend(t *testing.T) {
	db := newRecurringTestDB(t)

	// Grocery store: irregular dates AND highly variable amounts — not a
	// subscription. Must not be detected.
	dates := []time.Time{
		time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 19, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 25, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC),
	}
	amounts := []int64{-4500, -8231, -2200, -12000, -6700}
	for i := range dates {
		insertCharge(t, db, fmt.Sprintf("wf-%d", i), "Whole Foods", dates[i], amounts[i])
	}

	got, err := DetectRecurring(db, RecurringOptions{})
	if err != nil {
		t.Fatalf("DetectRecurring: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want 0 detections for variable spend, got %d: %+v", len(got), got)
	}
}

func TestDetectRecurring_RequiresMinimumOccurrences(t *testing.T) {
	db := newRecurringTestDB(t)

	// Only two charges — below the default minimum of three.
	insertCharge(t, db, "hb-1", "Hulu", time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC), -799)
	insertCharge(t, db, "hb-2", "Hulu", time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC), -799)

	got, err := DetectRecurring(db, RecurringOptions{})
	if err != nil {
		t.Fatalf("DetectRecurring: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want 0 detections below min occurrences, got %d", len(got))
	}
}

func TestDetectRecurring_IgnoresPending(t *testing.T) {
	db := newRecurringTestDB(t)
	for i := 0; i < 4; i++ {
		insertCharge(t, db, fmt.Sprintf("sp-%d", i), "Spotify",
			time.Date(2026, time.Month(1+i), 3, 0, 0, 0, 0, time.UTC), -999)
	}
	// A pending duplicate must be excluded from detection.
	_, err := db.Exec(`INSERT INTO transactions(fingerprint, account_id, posted_at, amount_cents, merchant, pending)
		VALUES ('sp-pending', 'acct', '2026-05-03', -999, 'Spotify', 1)`)
	if err != nil {
		t.Fatalf("insert pending: %v", err)
	}

	got, err := DetectRecurring(db, RecurringOptions{})
	if err != nil {
		t.Fatalf("DetectRecurring: %v", err)
	}
	if len(got) != 1 || got[0].DayOfMonth != 3 {
		t.Fatalf("pending charge leaked into detection: %+v", got)
	}
}

func TestMedianInt64(t *testing.T) {
	cases := []struct {
		in   []int64
		want int64
	}{
		{[]int64{1599}, 1599},
		{[]int64{1000, 2000, 3000}, 2000},
		{[]int64{1000, 2000, 3000, 5000}, 2500}, // mean of two middle
	}
	for _, c := range cases {
		if got := medianInt64(c.in); got != c.want {
			t.Errorf("medianInt64(%v): want %d, got %d", c.in, c.want, got)
		}
	}
}

func TestCadenceForInterval(t *testing.T) {
	cases := map[int]string{
		7:   "weekly",
		14:  "biweekly",
		30:  "monthly",
		31:  "monthly",
		90:  "quarterly",
		365: "annual",
	}
	for days, want := range cases {
		if got := cadenceForInterval(days); got != want {
			t.Errorf("cadenceForInterval(%d): want %s, got %s", days, want, got)
		}
	}
}
