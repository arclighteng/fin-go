package projections

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

// createProjectionSchema sets up all tables that ProjectCashFlow and related
// functions query.
func createProjectionSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS commitments (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			name            TEXT NOT NULL,
			merchant_norm   TEXT,
			expected_cents  INTEGER DEFAULT 0,
			cadence         TEXT NOT NULL,
			day_of_month    INTEGER,
			reference_date  TEXT,
			direction       TEXT NOT NULL DEFAULT 'expense',
			confirmed       INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS subscription_candidates (
			id                          INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id                  TEXT NOT NULL DEFAULT '',
			merchant_norm               TEXT NOT NULL,
			monthly_cost_estimate_cents INTEGER,
			interval_days               INTEGER,
			last_seen_at                TEXT,
			confidence                  REAL DEFAULT 0.5
		);
		CREATE TABLE IF NOT EXISTS transactions (
			fingerprint  TEXT PRIMARY KEY,
			account_id   TEXT,
			posted_at    TEXT NOT NULL,
			amount_cents INTEGER NOT NULL,
			merchant     TEXT,
			description  TEXT,
			pending      INTEGER DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS merchant_rules (
			id               INTEGER PRIMARY KEY,
			merchant_pattern TEXT,
			rule_type        TEXT
		);
	`)
	if err != nil {
		t.Fatalf("create projection schema: %v", err)
	}
}

// ---------------------------------------------------------------------------
// startOfDay
// ---------------------------------------------------------------------------

func TestStartOfDay(t *testing.T) {
	t.Parallel()
	input := time.Date(2025, 3, 15, 14, 30, 59, 999, time.UTC)
	got := startOfDay(input)
	want := time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("startOfDay: want %v, got %v", want, got)
	}
}

func TestStartOfDayMidnight(t *testing.T) {
	t.Parallel()
	input := time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC)
	got := startOfDay(input)
	if !got.Equal(input) {
		t.Errorf("startOfDay(midnight): want %v, got %v", input, got)
	}
}

// ---------------------------------------------------------------------------
// startOfMonth
// ---------------------------------------------------------------------------

func TestStartOfMonth(t *testing.T) {
	t.Parallel()
	input := time.Date(2025, 3, 15, 14, 30, 0, 0, time.UTC)
	got := startOfMonth(input)
	want := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("startOfMonth: want %v, got %v", want, got)
	}
}

func TestStartOfMonthFirstDay(t *testing.T) {
	t.Parallel()
	input := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	got := startOfMonth(input)
	if !got.Equal(input) {
		t.Errorf("startOfMonth(first day): want %v, got %v", input, got)
	}
}

// ---------------------------------------------------------------------------
// daysInMonth
// ---------------------------------------------------------------------------

func TestDaysInMonth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		year  int
		month time.Month
		want  int
	}{
		{2025, time.January, 31},
		{2025, time.February, 28},
		{2024, time.February, 29}, // leap year
		{2025, time.March, 31},
		{2025, time.April, 30},
		{2025, time.June, 30},
		{2025, time.December, 31},
	}

	for _, tc := range tests {
		t.Run(tc.month.String(), func(t *testing.T) {
			t.Parallel()
			got := daysInMonth(tc.year, tc.month)
			if got != tc.want {
				t.Errorf("daysInMonth(%d, %s): want %d, got %d", tc.year, tc.month, tc.want, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// lower
// ---------------------------------------------------------------------------

func TestLower(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"abc", "abc"},
		{"ABC", "abc"},
		{"StArBuCkS", "starbucks"},
		{"hello WORLD", "hello world"},
		{"123", "123"},
		{"already lower", "already lower"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got := lower(tc.input)
			if got != tc.want {
				t.Errorf("lower(%q): want %q, got %q", tc.input, tc.want, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// sortChargesByDate
// ---------------------------------------------------------------------------

func TestSortChargesByDate(t *testing.T) {
	t.Parallel()
	d1 := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2025, 3, 10, 0, 0, 0, 0, time.UTC)
	d3 := time.Date(2025, 3, 20, 0, 0, 0, 0, time.UTC)

	charges := []UpcomingCharge{
		{ExpectedDate: d3, Merchant: "c"},
		{ExpectedDate: d1, Merchant: "a"},
		{ExpectedDate: d2, Merchant: "b"},
	}

	sortChargesByDate(charges)

	if !charges[0].ExpectedDate.Equal(d1) {
		t.Errorf("charges[0]: want %v, got %v", d1, charges[0].ExpectedDate)
	}
	if !charges[1].ExpectedDate.Equal(d2) {
		t.Errorf("charges[1]: want %v, got %v", d2, charges[1].ExpectedDate)
	}
	if !charges[2].ExpectedDate.Equal(d3) {
		t.Errorf("charges[2]: want %v, got %v", d3, charges[2].ExpectedDate)
	}
}

func TestSortChargesByDateEmpty(t *testing.T) {
	t.Parallel()
	sortChargesByDate(nil)    // must not panic
	sortChargesByDate([]UpcomingCharge{}) // must not panic
}

// ---------------------------------------------------------------------------
// nextMonthlyDate
// ---------------------------------------------------------------------------

func TestNextMonthlyDate(t *testing.T) {
	t.Parallel()
	// lastDate is the 15th; today is the 10th → next is the 15th of this month.
	last := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)
	after := time.Date(2025, 3, 10, 0, 0, 0, 0, time.UTC)
	got := nextMonthlyDate(last, after)
	want := time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("nextMonthlyDate: want %v, got %v", want, got)
	}
}

func TestNextMonthlyDateRollsOver(t *testing.T) {
	t.Parallel()
	// lastDate is the 15th; today is the 20th → next is the 15th of the following month.
	last := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)
	after := time.Date(2025, 3, 20, 0, 0, 0, 0, time.UTC)
	got := nextMonthlyDate(last, after)
	want := time.Date(2025, 4, 15, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("nextMonthlyDate rollover: want %v, got %v", want, got)
	}
}

func TestNextMonthlyDateClampsShortMonth(t *testing.T) {
	t.Parallel()
	// lastDate is the 31st; February has 28 days → clamps to 28.
	last := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)
	after := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)
	got := nextMonthlyDate(last, after)
	if got.Day() > 28 {
		t.Errorf("nextMonthlyDate in February: day should be ≤ 28, got %d", got.Day())
	}
}

// ---------------------------------------------------------------------------
// computeNextDueDate
// ---------------------------------------------------------------------------

func TestComputeNextDueDateMonthly(t *testing.T) {
	t.Parallel()
	today := time.Date(2025, 3, 10, 0, 0, 0, 0, time.UTC)
	day := 15
	next := computeNextDueDate("monthly", &day, nil, today)
	if next == nil {
		t.Fatal("computeNextDueDate monthly: want non-nil")
	}
	if next.Day() != 15 {
		t.Errorf("day: want 15, got %d", next.Day())
	}
	if !next.After(today) || next.Equal(today) {
		// The result should be on or after today but at least the same date.
	}
}

func TestComputeNextDueDateOneTime(t *testing.T) {
	t.Parallel()
	today := time.Date(2025, 3, 10, 0, 0, 0, 0, time.UTC)

	// Future reference date → should return that date.
	future := time.Date(2025, 3, 20, 0, 0, 0, 0, time.UTC)
	next := computeNextDueDate("one_time", nil, &future, today)
	if next == nil {
		t.Fatal("one_time with future date: want non-nil")
	}
	if !next.Equal(future) {
		t.Errorf("one_time: want %v, got %v", future, *next)
	}

	// Past reference date → should return nil.
	past := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	next = computeNextDueDate("one_time", nil, &past, today)
	if next != nil {
		t.Errorf("one_time with past date: want nil, got %v", *next)
	}

	// No reference date → nil.
	next = computeNextDueDate("one_time", nil, nil, today)
	if next != nil {
		t.Errorf("one_time with no reference: want nil, got %v", *next)
	}
}

func TestComputeNextDueDateBiweekly(t *testing.T) {
	t.Parallel()
	ref := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	today := time.Date(2025, 3, 10, 0, 0, 0, 0, time.UTC)

	next := computeNextDueDate("biweekly", nil, &ref, today)
	if next == nil {
		t.Fatal("biweekly: want non-nil")
	}
	if !next.After(today) {
		t.Errorf("biweekly: next (%v) should be after today (%v)", *next, today)
	}
	// Gap from ref should be a multiple of 14.
	daysSinceRef := int(next.Sub(ref).Hours() / 24)
	if daysSinceRef%14 != 0 {
		t.Errorf("biweekly: gap from ref (%d days) is not a multiple of 14", daysSinceRef)
	}
}

func TestComputeNextDueDateUnknownCadence(t *testing.T) {
	t.Parallel()
	today := time.Date(2025, 3, 10, 0, 0, 0, 0, time.UTC)
	next := computeNextDueDate("unknown_cadence", nil, nil, today)
	if next != nil {
		t.Errorf("unknown cadence: want nil, got %v", *next)
	}
}

// ---------------------------------------------------------------------------
// DetectAlerts
// ---------------------------------------------------------------------------

func TestDetectAlertsLowIntegrity(t *testing.T) {
	t.Parallel()
	proj := &Projection{
		StartDate: time.Now().UTC(),
		EndDate:   time.Now().UTC().AddDate(0, 0, 30),
	}
	alerts := DetectAlerts(proj, 0.5, 0.5) // integrity < 0.8
	if len(alerts) != 1 {
		t.Fatalf("want 1 alert for low integrity, got %d", len(alerts))
	}
	if alerts[0].AlertType != "resolution_needed" {
		t.Errorf("AlertType: want resolution_needed, got %q", alerts[0].AlertType)
	}
}

func TestDetectAlertsLowConfidence(t *testing.T) {
	t.Parallel()
	proj := &Projection{
		StartDate:  time.Now().UTC(),
		EndDate:    time.Now().UTC().AddDate(0, 0, 30),
		Confidence: 0.3,
	}
	alerts := DetectAlerts(proj, 0.9, 0.5) // confidence < minConfidence
	if len(alerts) != 1 {
		t.Fatalf("want 1 alert for low confidence, got %d", len(alerts))
	}
	if alerts[0].AlertType != "low_confidence" {
		t.Errorf("AlertType: want low_confidence, got %q", alerts[0].AlertType)
	}
}

func TestDetectAlertsShortfall(t *testing.T) {
	t.Parallel()
	proj := &Projection{
		StartDate:           time.Now().UTC(),
		EndDate:             time.Now().UTC().AddDate(0, 0, 30),
		Confidence:          0.9,
		ExpectedIncomeCents: 100000,
		ExpectedFixedCents:  200000, // more than income → shortfall
		ExpectedNetCents:    -100000,
	}
	alerts := DetectAlerts(proj, 0.9, 0.5)

	var found bool
	for _, a := range alerts {
		if a.AlertType == "shortfall" {
			found = true
			break
		}
	}
	if !found {
		t.Error("want shortfall alert, none found")
	}
}

func TestDetectAlertsHighShortfallSeverity(t *testing.T) {
	t.Parallel()
	// Shortfall > $500 → severity "high".
	proj := &Projection{
		StartDate:        time.Now().UTC(),
		EndDate:          time.Now().UTC().AddDate(0, 0, 30),
		Confidence:       0.9,
		ExpectedNetCents: -60000, // $600 shortfall
	}
	alerts := DetectAlerts(proj, 0.9, 0.5)

	for _, a := range alerts {
		if a.AlertType == "shortfall" && a.Severity != "high" {
			t.Errorf("shortfall > $500: want severity high, got %q", a.Severity)
		}
	}
}

func TestDetectAlertsLargeCharge(t *testing.T) {
	t.Parallel()
	income := int64(100000) // $1000
	chargeAmt := int64(25000) // $250 = 25% of income (> 20%)
	proj := &Projection{
		StartDate:           time.Now().UTC(),
		EndDate:             time.Now().UTC().AddDate(0, 0, 30),
		Confidence:          0.9,
		ExpectedIncomeCents: income,
		ExpectedNetCents:    0,
		UpcomingCharges: []UpcomingCharge{
			{
				Merchant:      "big rent payment",
				ExpectedDate:  time.Now().UTC().AddDate(0, 0, 10),
				ExpectedCents: chargeAmt,
				Confidence:    0.9,
			},
		},
	}

	alerts := DetectAlerts(proj, 0.9, 0.5)

	var found bool
	for _, a := range alerts {
		if a.AlertType == "large_charge" {
			found = true
			break
		}
	}
	if !found {
		t.Error("want large_charge alert, none found")
	}
}

func TestDetectAlertsMultipleChargesSameDay(t *testing.T) {
	t.Parallel()
	sameDay := time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC)
	chargeAmt := int64(1000)
	proj := &Projection{
		StartDate:  time.Now().UTC(),
		EndDate:    time.Now().UTC().AddDate(0, 0, 30),
		Confidence: 0.9,
		UpcomingCharges: []UpcomingCharge{
			{Merchant: "a", ExpectedDate: sameDay, ExpectedCents: chargeAmt},
			{Merchant: "b", ExpectedDate: sameDay, ExpectedCents: chargeAmt},
			{Merchant: "c", ExpectedDate: sameDay, ExpectedCents: chargeAmt},
		},
	}

	alerts := DetectAlerts(proj, 0.9, 0.5)

	var found bool
	for _, a := range alerts {
		if a.AlertType == "multiple_charges" {
			found = true
			break
		}
	}
	if !found {
		t.Error("want multiple_charges alert, none found")
	}
}

func TestDetectAlertsNoAlertsForHealthyProjection(t *testing.T) {
	t.Parallel()
	proj := &Projection{
		StartDate:           time.Now().UTC(),
		EndDate:             time.Now().UTC().AddDate(0, 0, 30),
		Confidence:          0.9,
		ExpectedIncomeCents: 500000,
		ExpectedFixedCents:  100000,
		ExpectedNetCents:    400000, // healthy positive net
	}

	alerts := DetectAlerts(proj, 0.9, 0.5)

	// May have zero alerts or non-shortfall alerts only.
	for _, a := range alerts {
		if a.AlertType == "shortfall" {
			t.Errorf("should not have shortfall alert for healthy projection")
		}
	}
}

// ---------------------------------------------------------------------------
// ProjectCashFlow — empty DB
// ---------------------------------------------------------------------------

func TestProjectCashFlowEmptyDB(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createProjectionSchema(t, db)

	proj, err := ProjectCashFlow(db, ProjectOptions{DaysForward: 30})
	if err != nil {
		t.Fatalf("ProjectCashFlow: %v", err)
	}
	if proj == nil {
		t.Fatal("projection should not be nil")
	}
	if !proj.IsHeuristic {
		t.Error("IsHeuristic should always be true")
	}
	if proj.Confidence < 0 || proj.Confidence > 1 {
		t.Errorf("Confidence: want [0,1], got %.4f", proj.Confidence)
	}
}

func TestProjectCashFlowDefaultDays(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createProjectionSchema(t, db)

	proj, err := ProjectCashFlow(db, ProjectOptions{DaysForward: 0})
	if err != nil {
		t.Fatalf("ProjectCashFlow: %v", err)
	}

	// Default is 30 days.
	expectedDays := 30
	actualDays := int(proj.EndDate.Sub(proj.StartDate).Hours() / 24)
	if actualDays != expectedDays {
		t.Errorf("DaysForward default: want %d, got %d", expectedDays, actualDays)
	}
}

func TestProjectCashFlowDateRange(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createProjectionSchema(t, db)

	proj, err := ProjectCashFlow(db, ProjectOptions{DaysForward: 14})
	if err != nil {
		t.Fatalf("ProjectCashFlow: %v", err)
	}

	days := int(proj.EndDate.Sub(proj.StartDate).Hours() / 24)
	if days != 14 {
		t.Errorf("date range: want 14 days, got %d", days)
	}
	if proj.StartDate.After(proj.EndDate) {
		t.Error("StartDate should not be after EndDate")
	}
}

func TestProjectCashFlowNetCalculation(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createProjectionSchema(t, db)

	proj, err := ProjectCashFlow(db, ProjectOptions{DaysForward: 30})
	if err != nil {
		t.Fatalf("ProjectCashFlow: %v", err)
	}

	expectedNet := proj.ExpectedIncomeCents -
		proj.ExpectedFixedCents -
		proj.ExpectedVariableCents -
		proj.ExpectedDiscretionary
	if proj.ExpectedNetCents != expectedNet {
		t.Errorf("ExpectedNetCents: want %d, got %d", expectedNet, proj.ExpectedNetCents)
	}
}

// ---------------------------------------------------------------------------
// AccountFilter — verifies that filtered projections exclude other accounts
// ---------------------------------------------------------------------------

func TestProjectCashFlow_AccountFilter_ExcludesOtherAccounts(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	createProjectionSchema(t, db)

	// Seed transactions: two accounts, only one is selected by the filter.
	// All transactions are in the past 90 days so estimateFlexibleSpending picks them up.
	pastDate := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")
	_, err := db.Exec(`
		INSERT INTO transactions(fingerprint, account_id, posted_at, amount_cents, merchant, pending)
		VALUES
		  ('fp-a1', 'acct-A', ?, -10000, 'grocery', 0),
		  ('fp-a2', 'acct-A', ?, -5000,  'coffee',  0),
		  ('fp-b1', 'acct-B', ?, -99999, 'excluded', 0)
		`,
		pastDate, pastDate, pastDate,
	)
	if err != nil {
		t.Fatalf("seed transactions: %v", err)
	}

	// Project with only acct-A selected.
	projFiltered, err := ProjectCashFlow(db, ProjectOptions{
		DaysForward:   30,
		AccountFilter: []string{"acct-A"},
	})
	if err != nil {
		t.Fatalf("ProjectCashFlow (filtered): %v", err)
	}

	// Project with all accounts (no filter).
	projAll, err := ProjectCashFlow(db, ProjectOptions{DaysForward: 30})
	if err != nil {
		t.Fatalf("ProjectCashFlow (all): %v", err)
	}

	// The filtered projection should produce lower flexible spending than the
	// unfiltered one because acct-B's large transaction is excluded.
	filteredSpend := projFiltered.ExpectedVariableCents + projFiltered.ExpectedDiscretionary
	allSpend := projAll.ExpectedVariableCents + projAll.ExpectedDiscretionary

	if filteredSpend >= allSpend {
		t.Errorf(
			"account filter not applied: filtered spend (%d) should be < all-accounts spend (%d)",
			filteredSpend, allSpend,
		)
	}
}

// ---------------------------------------------------------------------------
// Bug 1: fixed commitments must not be double-counted in flexible spending
// ---------------------------------------------------------------------------

func TestEstimateFlexibleSpendingExcludesFixedCommitments(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createProjectionSchema(t, db)

	som := startOfMonth(time.Now().UTC())
	// Dates in each of the previous three full months (the estimate window).
	dates := []string{
		som.AddDate(0, -1, 14).Format("2006-01-02"),
		som.AddDate(0, -2, 14).Format("2006-01-02"),
		som.AddDate(0, -3, 14).Format("2006-01-02"),
	}

	// "rent" is a fixed commitment (also present as transactions); "coffee" is
	// genuinely flexible spending.
	for i, d := range dates {
		if _, err := db.Exec(
			`INSERT INTO transactions(fingerprint, account_id, posted_at, amount_cents, merchant, pending)
			 VALUES (?, 'acct-1', ?, -100000, 'rent', 0), (?, 'acct-1', ?, -3000, 'coffee', 0)`,
			"rent-"+itoa(i), d, "coffee-"+itoa(i), d,
		); err != nil {
			t.Fatalf("seed transactions: %v", err)
		}
	}

	// Without dedup: rent gets counted in flexible spending too.
	varNo, discNo, err := estimateFlexibleSpending(db, 30, nil, nil)
	if err != nil {
		t.Fatalf("estimateFlexibleSpending (no exclude): %v", err)
	}
	noTotal := varNo + discNo

	// With rent excluded (as a fixed commitment would be), only coffee remains.
	exclude := map[string]struct{}{"rent": {}}
	varYes, discYes, err := estimateFlexibleSpending(db, 30, nil, exclude)
	if err != nil {
		t.Fatalf("estimateFlexibleSpending (exclude rent): %v", err)
	}
	yesTotal := varYes + discYes

	// 3 months, 30-day forward window: monthly avg == total/3, scaled *30/30.
	// No-exclude: (3*100000 + 3*3000)/3 = 103000. Exclude rent: 3*3000/3 = 3000.
	if noTotal != 103000 {
		t.Errorf("baseline flexible spend: want 103000 (proves double-count), got %d", noTotal)
	}
	if yesTotal != 3000 {
		t.Errorf("deduped flexible spend: want 3000 (coffee only), got %d", yesTotal)
	}
	if yesTotal >= noTotal {
		t.Errorf("excluding fixed commitment should lower flexible spend: %d vs %d", yesTotal, noTotal)
	}
}

// ---------------------------------------------------------------------------
// Bug 2: income fallback dedupes multi-rule matches and guards empty patterns
// ---------------------------------------------------------------------------

func TestEstimateIncomeFallbackDedupesAndGuardsEmptyPattern(t *testing.T) {
	t.Parallel()

	// estimate builds a fresh DB with the given income rules and a single
	// positive transaction, then runs the fallback income estimator.
	estimate := func(t *testing.T, patterns []string, merchant string, amt int64) int64 {
		t.Helper()
		db := newTestDB(t)
		createProjectionSchema(t, db)
		for i, p := range patterns {
			if _, err := db.Exec(
				`INSERT INTO merchant_rules(id, merchant_pattern, rule_type) VALUES (?, ?, 'income')`,
				i+1, p,
			); err != nil {
				t.Fatalf("insert rule: %v", err)
			}
		}
		d := startOfMonth(time.Now().UTC()).AddDate(0, -1, 14).Format("2006-01-02")
		if _, err := db.Exec(
			`INSERT INTO transactions(fingerprint, account_id, posted_at, amount_cents, merchant, pending)
			 VALUES ('inc-1', 'acct-1', ?, ?, ?, 0)`,
			d, amt, merchant,
		); err != nil {
			t.Fatalf("insert txn: %v", err)
		}
		got, err := estimateIncome(db, 30, nil)
		if err != nil {
			t.Fatalf("estimateIncome: %v", err)
		}
		return got
	}

	// One matching rule.
	one := estimate(t, []string{"payroll"}, "payroll deposit", 300000)
	if one <= 0 {
		t.Fatalf("single-rule income estimate should be > 0, got %d", one)
	}

	// Two rules that BOTH match the same deposit must not double-count it.
	two := estimate(t, []string{"payroll", "deposit"}, "payroll deposit", 300000)
	if two != one {
		t.Errorf("multi-rule match double-counts: want %d (deduped), got %d", one, two)
	}

	// An empty/blank pattern must not match every positive transaction.
	empty := estimate(t, []string{""}, "random unrelated deposit", 300000)
	if empty != 0 {
		t.Errorf("empty income pattern should match nothing, got %d", empty)
	}
	blank := estimate(t, []string{"   "}, "random unrelated deposit", 300000)
	if blank != 0 {
		t.Errorf("blank income pattern should match nothing, got %d", blank)
	}
}

// ---------------------------------------------------------------------------
// Bug 3: a monthly commitment due later this month is returned, not skipped
// ---------------------------------------------------------------------------

func TestComputeNextDueDateMonthlyCurrentMonth(t *testing.T) {
	t.Parallel()

	// Today is the 10th; the bill is due the 25th → next due date is the 25th
	// of THIS month, not next month.
	today := time.Date(2025, 3, 10, 0, 0, 0, 0, time.UTC)
	day := 25
	next := computeNextDueDate("monthly", &day, nil, today)
	if next == nil {
		t.Fatal("computeNextDueDate monthly: want non-nil")
	}
	want := time.Date(2025, 3, 25, 0, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("current-month bill: want %v, got %v", want, *next)
	}

	// Today is the 25th, bill due the 10th (already passed) → next month.
	today2 := time.Date(2025, 3, 25, 0, 0, 0, 0, time.UTC)
	day2 := 10
	next2 := computeNextDueDate("monthly", &day2, nil, today2)
	if next2 == nil {
		t.Fatal("computeNextDueDate monthly (past day): want non-nil")
	}
	want2 := time.Date(2025, 4, 10, 0, 0, 0, 0, time.UTC)
	if !next2.Equal(want2) {
		t.Errorf("passed-this-month bill: want %v, got %v", want2, *next2)
	}

	// Bill due exactly today → today (inclusive).
	today3 := time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC)
	day3 := 15
	next3 := computeNextDueDate("monthly", &day3, nil, today3)
	if next3 == nil || !next3.Equal(today3) {
		t.Errorf("bill due today: want %v, got %v", today3, next3)
	}
}

// ---------------------------------------------------------------------------
// Bug 4: medianCommitmentAmount returns a numeric median, not date-middle
// ---------------------------------------------------------------------------

func TestMedianCommitmentAmountNumericMedian(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createProjectionSchema(t, db)

	// Insert three charges for one merchant. Ordered by posted_at DESC the
	// amounts are 100, 500, 300 — so the date-middle element is 500, but the
	// true numeric median of {100, 300, 500} is 300.
	if _, err := db.Exec(`
		INSERT INTO transactions(fingerprint, account_id, posted_at, amount_cents, merchant, pending) VALUES
		  ('m1', 'acct-1', '2025-03-03', -100, 'netflix', 0),
		  ('m2', 'acct-1', '2025-03-02', -500, 'netflix', 0),
		  ('m3', 'acct-1', '2025-03-01', -300, 'netflix', 0)`,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, ok, err := medianCommitmentAmount(db, "netflix", nil, "monthly")
	if err != nil {
		t.Fatalf("medianCommitmentAmount: %v", err)
	}
	if !ok {
		t.Fatal("want ok=true")
	}
	if got != 300 {
		t.Errorf("numeric median: want 300, got %d (date-middle would be 500)", got)
	}

	// Even-sized sample: median is the mean of the two middle values.
	if _, err := db.Exec(`
		INSERT INTO transactions(fingerprint, account_id, posted_at, amount_cents, merchant, pending) VALUES
		  ('e1', 'acct-1', '2025-04-04', -100, 'spotify', 0),
		  ('e2', 'acct-1', '2025-04-03', -200, 'spotify', 0),
		  ('e3', 'acct-1', '2025-04-02', -300, 'spotify', 0),
		  ('e4', 'acct-1', '2025-04-01', -400, 'spotify', 0)`,
	); err != nil {
		t.Fatalf("seed even: %v", err)
	}
	gotEven, ok, err := medianCommitmentAmount(db, "spotify", nil, "monthly")
	if err != nil || !ok {
		t.Fatalf("medianCommitmentAmount even: ok=%v err=%v", ok, err)
	}
	if gotEven != 250 { // (200+300)/2
		t.Errorf("even median: want 250, got %d", gotEven)
	}
}

// itoa is a tiny helper to avoid importing strconv in tests.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// buildPlaceholders helper
// ---------------------------------------------------------------------------

func TestBuildPlaceholders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		n    int
		want string
	}{
		{0, ""},
		{1, "?"},
		{2, "?, ?"},
		{3, "?, ?, ?"},
	}

	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			t.Parallel()

			got := buildPlaceholders(tc.n)
			if got != tc.want {
				t.Errorf("buildPlaceholders(%d): want %q, got %q", tc.n, tc.want, got)
			}
		})
	}
}
