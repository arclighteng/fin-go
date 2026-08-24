package planner

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

// createMinimalSchema sets up the tables that SpendingPlan needs.
// Mirrors the subset of internal/db/schema.go required by the classify package.
func createMinimalSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS transactions (
			fingerprint  TEXT PRIMARY KEY,
			account_id   TEXT,
			posted_at    TEXT NOT NULL,
			amount_cents INTEGER NOT NULL,
			merchant     TEXT,
			description  TEXT,
			pending      INTEGER DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS category_overrides (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			merchant_norm TEXT NOT NULL UNIQUE,
			category_id   TEXT NOT NULL,
			created_at    TEXT NOT NULL DEFAULT '',
			updated_at    TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE IF NOT EXISTS merchant_rules (
			id               INTEGER PRIMARY KEY AUTOINCREMENT,
			merchant_pattern TEXT NOT NULL,
			rule_type        TEXT NOT NULL,
			created_at       TEXT NOT NULL DEFAULT '',
			UNIQUE(merchant_pattern, rule_type)
		);
		CREATE TABLE IF NOT EXISTS txn_type_overrides (
			id               INTEGER PRIMARY KEY AUTOINCREMENT,
			fingerprint      TEXT,
			merchant_pattern TEXT,
			target_type      TEXT NOT NULL,
			reason           TEXT,
			created_at       TEXT NOT NULL DEFAULT ''
		);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
}

// insertTransaction is a helper that inserts a transaction row.
func insertTransaction(t *testing.T, db *sql.DB, fp, postedAt, merchant string, amountCents int64) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO transactions (fingerprint, account_id, posted_at, amount_cents, merchant, pending)
		 VALUES (?, 'acct-1', ?, ?, ?, 0)`,
		fp, postedAt, amountCents, merchant,
	)
	if err != nil {
		t.Fatalf("insertTransaction %s: %v", fp, err)
	}
}

// ---------------------------------------------------------------------------
// avgInt64
// ---------------------------------------------------------------------------

func TestAvgInt64(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		vals []int64
		want int64
	}{
		{"empty", nil, 0},
		{"single", []int64{100}, 100},
		{"two equal", []int64{100, 200}, 150},
		{"truncates", []int64{1, 2}, 1}, // integer division
		{"large values", []int64{1000000, 2000000, 3000000}, 2000000},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := avgInt64(tc.vals)
			if got != tc.want {
				t.Errorf("avgInt64(%v): want %d, got %d", tc.vals, tc.want, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// sortedKeys
// ---------------------------------------------------------------------------

func TestSortedKeys(t *testing.T) {
	t.Parallel()
	m := map[string]struct{}{
		"2025-03": {},
		"2025-01": {},
		"2025-02": {},
	}
	keys := sortedKeys(m)
	if len(keys) != 3 {
		t.Fatalf("want 3 keys, got %d", len(keys))
	}
	for i := 1; i < len(keys); i++ {
		if keys[i] < keys[i-1] {
			t.Errorf("keys not sorted: %v", keys)
		}
	}
}

func TestSortedKeysEmpty(t *testing.T) {
	t.Parallel()
	keys := sortedKeys(map[string]struct{}{})
	if len(keys) != 0 {
		t.Errorf("want 0 keys, got %d", len(keys))
	}
}

// ---------------------------------------------------------------------------
// SpendingPlan — empty DB
// ---------------------------------------------------------------------------

func TestSpendingPlanEmptyDB(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createMinimalSchema(t, db)

	plan, err := SpendingPlan(db, PlanOptions{Months: 3})
	if err != nil {
		t.Fatalf("SpendingPlan: %v", err)
	}
	if plan == nil {
		t.Fatal("plan should not be nil")
	}
	if plan.PeriodMonths != 3 {
		t.Errorf("PeriodMonths: want 3, got %d", plan.PeriodMonths)
	}
	if plan.TotalMonthlyIncome != 0 {
		t.Errorf("TotalMonthlyIncome: want 0, got %d", plan.TotalMonthlyIncome)
	}
	if plan.TotalMonthlySpend != 0 {
		t.Errorf("TotalMonthlySpend: want 0, got %d", plan.TotalMonthlySpend)
	}
	if len(plan.Buckets) != 4 {
		t.Errorf("want 4 buckets, got %d", len(plan.Buckets))
	}
}

// TestSpendingPlanDefaultMonths verifies that Months=0 defaults to 6.
func TestSpendingPlanDefaultMonths(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createMinimalSchema(t, db)

	plan, err := SpendingPlan(db, PlanOptions{Months: 0})
	if err != nil {
		t.Fatalf("SpendingPlan: %v", err)
	}
	if plan.PeriodMonths != 6 {
		t.Errorf("default Months: want 6, got %d", plan.PeriodMonths)
	}
}

// TestSpendingPlanBucketLabels verifies all four bucket labels are populated.
func TestSpendingPlanBucketLabels(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createMinimalSchema(t, db)

	plan, err := SpendingPlan(db, PlanOptions{Months: 3})
	if err != nil {
		t.Fatalf("SpendingPlan: %v", err)
	}

	for _, b := range plan.Buckets {
		if b.Label == "" {
			t.Errorf("bucket %v has empty label", b.Bucket)
		}
		if b.Description == "" {
			t.Errorf("bucket %v has empty description", b.Bucket)
		}
	}
}

// TestSpendingPlanWithTransactions verifies that expense transactions are
// picked up and reflected in bucket totals.
func TestSpendingPlanWithTransactions(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createMinimalSchema(t, db)

	// Use a stable past month so "today" doesn't affect the window.
	// SpendingPlan queries from endDate-months to endDate where endDate=start of current month.
	// Insert in the most recent full month.
	now := time.Now().UTC()
	lastMonth := time.Date(now.Year(), now.Month()-1, 15, 0, 0, 0, 0, time.UTC)
	posted := lastMonth.Format("2006-01-02")

	insertTransaction(t, db, "fp-expense", posted, "grocery store", -50000)
	insertTransaction(t, db, "fp-income", posted, "payroll direct deposit", 200000)

	plan, err := SpendingPlan(db, PlanOptions{Months: 3})
	if err != nil {
		t.Fatalf("SpendingPlan: %v", err)
	}
	if plan == nil {
		t.Fatal("plan is nil")
	}
	// Income should be captured.
	if plan.TotalMonthlyIncome == 0 {
		t.Error("TotalMonthlyIncome: want > 0")
	}
}

// TestSpendingPlanHealthScore verifies health score is between 0 and 1.
func TestSpendingPlanHealthScore(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createMinimalSchema(t, db)

	plan, err := SpendingPlan(db, PlanOptions{Months: 3})
	if err != nil {
		t.Fatalf("SpendingPlan: %v", err)
	}
	if plan.HealthScore < 0 || plan.HealthScore > 1 {
		t.Errorf("HealthScore: want [0,1], got %.4f", plan.HealthScore)
	}
}

// TestSpendingPlanNetCalculation verifies that NetMonthlyCents = income - spend.
func TestSpendingPlanNetCalculation(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createMinimalSchema(t, db)

	plan, err := SpendingPlan(db, PlanOptions{Months: 3})
	if err != nil {
		t.Fatalf("SpendingPlan: %v", err)
	}
	expectedNet := plan.TotalMonthlyIncome - plan.TotalMonthlySpend
	if plan.NetMonthlyCents != expectedNet {
		t.Errorf("NetMonthlyCents: want %d, got %d", expectedNet, plan.NetMonthlyCents)
	}
}

// TestBucketDrillDownEmptyDB verifies BucketDrillDown does not error on an empty DB.
func TestBucketDrillDownEmptyDB(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createMinimalSchema(t, db)

	detail, err := BucketDrillDown(db, bucketMeta[0].bucket, PlanOptions{Months: 3})
	if err != nil {
		t.Fatalf("BucketDrillDown: %v", err)
	}
	if detail == nil {
		t.Fatal("detail should not be nil")
	}
	if len(detail.Merchants) != 0 {
		t.Errorf("want 0 merchants, got %d", len(detail.Merchants))
	}
}

// TestProjectMonthlyBudgetForwardMonths verifies that the right number of
// projection months are returned.
func TestProjectMonthlyBudgetForwardMonths(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createMinimalSchema(t, db)

	result, err := ProjectMonthlyBudget(db, 3, 4, nil)
	if err != nil {
		t.Fatalf("ProjectMonthlyBudget: %v", err)
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}

	projs, ok := result["projections"].([]map[string]any)
	if !ok {
		t.Fatal("projections key missing or wrong type")
	}
	if len(projs) != 4 {
		t.Errorf("want 4 projections, got %d", len(projs))
	}

	// Verify that each projection has the expected keys.
	for i, p := range projs {
		for _, key := range []string{
			"month",
			"projected_income_cents",
			"projected_fixed_cents",
			"projected_variable_cents",
			"projected_discretionary_cents",
			"projected_net_cents",
		} {
			if _, ok := p[key]; !ok {
				t.Errorf("projection[%d] missing key %q", i, key)
			}
		}
	}
}

// TestProjectMonthlyBudgetAssumptions verifies that assumptions slice is populated.
func TestProjectMonthlyBudgetAssumptions(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createMinimalSchema(t, db)

	result, err := ProjectMonthlyBudget(db, 3, 1, nil)
	if err != nil {
		t.Fatalf("ProjectMonthlyBudget: %v", err)
	}

	assumptions, ok := result["assumptions"].([]string)
	if !ok {
		t.Fatal("assumptions key missing or wrong type")
	}
	if len(assumptions) == 0 {
		t.Error("assumptions: want non-empty slice")
	}
}

// TestProjectMonthlyBudgetBasedOnMonths verifies that based_on_months reflects historyMonths.
func TestProjectMonthlyBudgetBasedOnMonths(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createMinimalSchema(t, db)

	result, err := ProjectMonthlyBudget(db, 6, 1, nil)
	if err != nil {
		t.Fatalf("ProjectMonthlyBudget: %v", err)
	}

	v, ok := result["based_on_months"].(int)
	if !ok {
		t.Fatal("based_on_months key missing or wrong type")
	}
	if v != 6 {
		t.Errorf("based_on_months: want 6, got %d", v)
	}
}

// TestSpendingPlanAnnualChargeAmortizedOverWindow verifies Bug 5: an
// infrequent/annual charge is amortized over the WINDOW LENGTH in months, not
// reported as a full monthly average.
func TestSpendingPlanAnnualChargeAmortizedOverWindow(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createMinimalSchema(t, db)

	// One $1,200 charge, once, inside a 6-month window.
	now := time.Now().UTC()
	when := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).
		AddDate(0, -3, 14).Format("2006-01-02")
	insertTransaction(t, db, "fp-annual", when, "annual insurance", -120000)

	plan, err := SpendingPlan(db, PlanOptions{Months: 6})
	if err != nil {
		t.Fatalf("SpendingPlan: %v", err)
	}

	// Amortized: 120000 / 6 = 20000/mo. The buggy behavior would report 120000
	// (sum divided by the single active month).
	if plan.TotalMonthlySpend != 20000 {
		t.Errorf("annual charge amortization: want 20000 (120000/6), got %d", plan.TotalMonthlySpend)
	}
	if plan.TotalMonthlySpend == 120000 {
		t.Error("charge was NOT amortized: reported full amount as monthly average")
	}

	// The bucket that holds the charge should also report the amortized average.
	var found bool
	for _, b := range plan.Buckets {
		if b.MonthlyAvgCents == 20000 {
			found = true
		}
		if b.MonthlyAvgCents == 120000 {
			t.Errorf("bucket %v reports un-amortized monthly avg 120000", b.Bucket)
		}
	}
	if !found {
		t.Error("no bucket reports the amortized 20000/mo average")
	}
}

// TestSpendingPlanIncomeAmortizedOverWindow verifies the consistent denominator
// convention: income is also averaged over the window length, not over the
// count of months that happened to contain income.
func TestSpendingPlanIncomeAmortizedOverWindow(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createMinimalSchema(t, db)

	// A single $6,000 payroll deposit inside a 6-month window.
	now := time.Now().UTC()
	when := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).
		AddDate(0, -2, 14).Format("2006-01-02")
	insertTransaction(t, db, "fp-pay", when, "payroll direct deposit", 600000)

	plan, err := SpendingPlan(db, PlanOptions{Months: 6})
	if err != nil {
		t.Fatalf("SpendingPlan: %v", err)
	}
	// 600000 / 6 = 100000/mo (window length), not 600000 (single income month).
	if plan.TotalMonthlyIncome != 100000 {
		t.Errorf("income denominator: want 100000 (600000/6), got %d", plan.TotalMonthlyIncome)
	}
}

// TestSpendingPlanPendingExcluded verifies that pending transactions are not counted.
func TestSpendingPlanPendingExcluded(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	createMinimalSchema(t, db)

	now := time.Now().UTC()
	lastMonth := time.Date(now.Year(), now.Month()-1, 15, 0, 0, 0, 0, time.UTC)
	posted := lastMonth.Format("2006-01-02")

	// Insert a pending income transaction — should be excluded.
	_, err := db.Exec(
		`INSERT INTO transactions (fingerprint, account_id, posted_at, amount_cents, merchant, pending)
		 VALUES ('fp-pending', 'acct-1', ?, 200000, 'payroll', 1)`, posted,
	)
	if err != nil {
		t.Fatalf("insert pending: %v", err)
	}

	plan, err := SpendingPlan(db, PlanOptions{Months: 3})
	if err != nil {
		t.Fatalf("SpendingPlan: %v", err)
	}
	// Pending transaction should not contribute to income.
	if plan.TotalMonthlyIncome != 0 {
		t.Errorf("pending income should be excluded; TotalMonthlyIncome=%d", plan.TotalMonthlyIncome)
	}
}
