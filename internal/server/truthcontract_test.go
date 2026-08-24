package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arclighteng/fin-go/internal/classify"
	"github.com/arclighteng/fin-go/internal/config"
	"github.com/arclighteng/fin-go/internal/db"
	"github.com/arclighteng/fin-go/internal/models"
)

// newRawDB returns an initialised in-memory database for internal-package tests.
func newRawDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Connect(":memory:")
	if err != nil {
		t.Fatalf("db.Connect: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := database.Init(); err != nil {
		t.Fatalf("database.Init: %v", err)
	}
	return database
}

func aprilDate(day int) time.Time {
	return time.Date(2026, 4, day, 0, 0, 0, 0, time.UTC)
}

// seedTruthDataset seeds a real-money dataset whose only true income is a
// $5,000 payroll deposit. It also contains three positive-amount credits that a
// naive positive=income rule would misreport as income:
//   - a $75 refund (refund keyword)
//   - a $250 transfer-in (transfer keyword)
//   - a $300 credit-card payment received (positive on a credit-card account)
//
// Canonical income must therefore be exactly $5,000 (500000 cents); the naive
// positive-sum would be $5,625 (562500 cents).
func seedTruthDataset(t *testing.T, database *db.DB) {
	t.Helper()
	if err := database.UpsertAccounts([]models.Account{
		{AccountID: "acct-check", Institution: "Bank", Name: "Checking", Type: "checking", Currency: "USD"},
		{AccountID: "acct-cc", Institution: "Bank", Name: "Card", Type: "credit card", Currency: "USD"},
	}); err != nil {
		t.Fatalf("seed accounts: %v", err)
	}
	if _, _, err := database.UpsertTransactions([]models.Transaction{
		{AccountID: "acct-check", PostedAt: aprilDate(1), AmountCents: 500000, Currency: "USD", Description: "PAYROLL DIRECT DEPOSIT", Merchant: "PAYROLL DIRECT DEPOSIT", Fingerprint: "fp-income"},
		{AccountID: "acct-check", PostedAt: aprilDate(5), AmountCents: 7500, Currency: "USD", Description: "AMAZON REFUND", Merchant: "AMAZON REFUND", Fingerprint: "fp-refund"},
		{AccountID: "acct-check", PostedAt: aprilDate(7), AmountCents: 25000, Currency: "USD", Description: "ONLINE BANKING TRANSFER", Merchant: "ONLINE BANKING TRANSFER", Fingerprint: "fp-transfer"},
		{AccountID: "acct-check", PostedAt: aprilDate(9), AmountCents: -45000, Currency: "USD", Description: "WHOLE FOODS", Merchant: "WHOLE FOODS", Fingerprint: "fp-expense"},
		{AccountID: "acct-cc", PostedAt: aprilDate(11), AmountCents: 30000, Currency: "USD", Description: "PAYMENT THANK YOU", Merchant: "PAYMENT THANK YOU", Fingerprint: "fp-ccpay"},
	}); err != nil {
		t.Fatalf("seed transactions: %v", err)
	}
}

// TestQueryPeriodTotals_MatchesCanonicalReport is the ADA-108 unit assertion:
// the dashboard's period totals must equal the canonical classify report, and a
// refund + transfer-in + card payment must NOT be counted as income.
func TestQueryPeriodTotals_MatchesCanonicalReport(t *testing.T) {
	database := newRawDB(t)
	seedTruthDataset(t, database)

	const startISO, endISO = "2026-04-01", "2026-05-01"
	ps := queryPeriodTotals(database, startISO, endISO, nil)
	if ps == nil {
		t.Fatal("queryPeriodTotals returned nil")
	}

	start, _ := time.Parse("2006-01-02", startISO)
	end, _ := time.Parse("2006-01-02", endISO)
	report := classify.ReportPeriod(database.Underlying(), start, end, false, nil)

	if ps.IncomeCents != report.Totals.IncomeCents {
		t.Fatalf("dashboard income %d != canonical report income %d", ps.IncomeCents, report.Totals.IncomeCents)
	}
	if ps.IncomeCents != 500000 {
		t.Fatalf("income = %d, want 500000 (payroll only); refund/transfer/card-payment leaked in", ps.IncomeCents)
	}
	if ps.IncomeCents == 562500 {
		t.Fatal("income equals the naive positive-sum ($5,625); non-income credits were counted as income")
	}

	// Net must follow the Truth Contract: income + refunds - expenses.
	wantNet := report.Totals.NetCents()
	if ps.NetCents != wantNet {
		t.Fatalf("net = %d, want %d (canonical)", ps.NetCents, wantNet)
	}
}

// TestDashboard_IncomeMatchesCanonicalReport is the ADA-108 handler/integration
// assertion: the rendered dashboard shows the canonical income, not the inflated
// positive-sum.
func TestDashboard_IncomeMatchesCanonicalReport(t *testing.T) {
	database := newRawDB(t)
	seedTruthDataset(t, database)

	h := New(database, &config.Config{Timezone: "UTC"}, "test")
	req := httptest.NewRequest(http.MethodGet, "/dashboard?start_date=2026-04-01&end_date=2026-05-01", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /dashboard: want 200, got %d", w.Code)
	}
	body := w.Body.String()

	if !strings.Contains(body, "$5,000.00") {
		t.Error("dashboard missing canonical income $5,000.00")
	}
	// The naive positive=income total would render $5,625.00 — it must not.
	if strings.Contains(body, "$5,625.00") {
		t.Error("dashboard shows inflated income $5,625.00 — non-income credits leaked into income")
	}
}

// TestQueryPeriodTotals_ExcludesDemoWhenRealPresent is the ADA-112 assertion:
// when real and demo accounts coexist, reported totals reflect real data only.
func TestQueryPeriodTotals_ExcludesDemoWhenRealPresent(t *testing.T) {
	database := newRawDB(t)
	seedTruthDataset(t, database)

	// Add a demo account with a large demo payroll deposit in the same window.
	if err := database.UpsertAccounts([]models.Account{
		{AccountID: "demo-checking", Institution: "Demo", Name: "Demo Checking", Type: "checking", Currency: "USD"},
	}); err != nil {
		t.Fatalf("seed demo account: %v", err)
	}
	if _, _, err := database.UpsertTransactions([]models.Transaction{
		{AccountID: "demo-checking", PostedAt: aprilDate(3), AmountCents: 999999, Currency: "USD", Description: "PAYROLL DEMO", Merchant: "PAYROLL DEMO", Fingerprint: "demo-fp-income"},
	}); err != nil {
		t.Fatalf("seed demo transactions: %v", err)
	}

	ps := queryPeriodTotals(database, "2026-04-01", "2026-05-01", nil)
	if ps == nil {
		t.Fatal("queryPeriodTotals returned nil")
	}
	if ps.IncomeCents != 500000 {
		t.Fatalf("income = %d, want 500000 (real only); demo account leaked into totals", ps.IncomeCents)
	}
}
