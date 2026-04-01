package classify_test

import (
	"testing"
	"time"

	"github.com/arclighteng/fin-go/internal/classify"
)

// ptr helpers — avoids noise when assigning optional pointer fields.
func ptrBucket(b classify.SpendingBucket) *classify.SpendingBucket { return &b }
func ptrTransfer(s classify.TransferStatus) *classify.TransferStatus { return &s }

// periodRange returns a fixed start/end pair for test reports.
func periodRange() (time.Time, time.Time) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	return start, end
}

func TestBuildReport_Empty(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	report := classify.BuildReport(nil, classify.TimePeriodMonth, start, end)

	if report == nil {
		t.Fatal("BuildReport returned nil")
	}
	if report.TransactionCount != 0 {
		t.Errorf("TransactionCount = %d, want 0", report.TransactionCount)
	}
	if report.Totals.IncomeCents != 0 {
		t.Errorf("IncomeCents = %d, want 0", report.Totals.IncomeCents)
	}
	if report.Totals.TotalExpensesCents() != 0 {
		t.Errorf("TotalExpensesCents = %d, want 0", report.Totals.TotalExpensesCents())
	}
	if len(report.Integrity.Flags) != 0 {
		t.Errorf("Integrity.Flags = %v, want empty", report.Integrity.Flags)
	}
	if report.ReportHash == "" {
		t.Error("ReportHash must be set even for empty report")
	}
}

func TestBuildReport_PeriodLabel(t *testing.T) {
	t.Parallel()

	start, end := periodRange()

	tests := []struct {
		name   string
		period classify.TimePeriod
		want   string
	}{
		{name: "month", period: classify.TimePeriodMonth, want: "Jan 2024"},
		{name: "quarter Q1", period: classify.TimePeriodQuarter, want: "Q1 2024"},
		{name: "year", period: classify.TimePeriodYear, want: "2024"},
		{name: "custom", period: classify.TimePeriodCustom, want: "2024-01-01 to 2024-02-01"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			report := classify.BuildReport(nil, tc.period, start, end)
			if report.PeriodLabel != tc.want {
				t.Errorf("PeriodLabel = %q, want %q", report.PeriodLabel, tc.want)
			}
		})
	}
}

func TestBuildReport_IncomeTotals(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	txns := []classify.ClassifiedTransaction{
		{Fingerprint: "fp1", TxnType: classify.TxnIncome, AmountCents: 500000},
		{Fingerprint: "fp2", TxnType: classify.TxnIncome, AmountCents: 250000},
	}

	report := classify.BuildReport(txns, classify.TimePeriodMonth, start, end)

	if report.Totals.IncomeCents != 750000 {
		t.Errorf("IncomeCents = %d, want 750000", report.Totals.IncomeCents)
	}
	if report.TransactionCount != 2 {
		t.Errorf("TransactionCount = %d, want 2", report.TransactionCount)
	}
}

func TestBuildReport_ExpenseBuckets(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	txns := []classify.ClassifiedTransaction{
		{Fingerprint: "e1", TxnType: classify.TxnExpense, AmountCents: -10000, SpendingBucket: ptrBucket(classify.BucketFixedObligations)},
		{Fingerprint: "e2", TxnType: classify.TxnExpense, AmountCents: -5000, SpendingBucket: ptrBucket(classify.BucketVariableEssentials)},
		{Fingerprint: "e3", TxnType: classify.TxnExpense, AmountCents: -3000, SpendingBucket: ptrBucket(classify.BucketDiscretionary)},
		{Fingerprint: "e4", TxnType: classify.TxnExpense, AmountCents: -2000, SpendingBucket: ptrBucket(classify.BucketOneOffs)},
		// nil SpendingBucket defaults to Discretionary
		{Fingerprint: "e5", TxnType: classify.TxnExpense, AmountCents: -1000, SpendingBucket: nil},
	}

	report := classify.BuildReport(txns, classify.TimePeriodMonth, start, end)
	tot := report.Totals

	if tot.FixedObligationsCents != 10000 {
		t.Errorf("FixedObligationsCents = %d, want 10000", tot.FixedObligationsCents)
	}
	if tot.VariableEssentialsCents != 5000 {
		t.Errorf("VariableEssentialsCents = %d, want 5000", tot.VariableEssentialsCents)
	}
	if tot.DiscretionaryCents != 4000 { // e3 (-3000 abs) + e5 (-1000 abs)
		t.Errorf("DiscretionaryCents = %d, want 4000", tot.DiscretionaryCents)
	}
	if tot.OneOffsCents != 2000 {
		t.Errorf("OneOffsCents = %d, want 2000", tot.OneOffsCents)
	}
	if tot.TotalExpensesCents() != 21000 {
		t.Errorf("TotalExpensesCents = %d, want 21000", tot.TotalExpensesCents())
	}
}

func TestBuildReport_TransferExcludedFromNet(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	txns := []classify.ClassifiedTransaction{
		{Fingerprint: "inc", TxnType: classify.TxnIncome, AmountCents: 100000},
		{Fingerprint: "exp", TxnType: classify.TxnExpense, AmountCents: -40000, SpendingBucket: ptrBucket(classify.BucketDiscretionary)},
		{Fingerprint: "xfr-out", TxnType: classify.TxnTransfer, AmountCents: -20000},
		{Fingerprint: "xfr-in", TxnType: classify.TxnTransfer, AmountCents: 20000},
	}

	report := classify.BuildReport(txns, classify.TimePeriodMonth, start, end)
	totals := report.Totals

	// Net = income - expenses (transfers excluded).
	wantNet := int64(100000 - 40000) // 60000
	if totals.NetCents() != wantNet {
		t.Errorf("NetCents = %d, want %d", totals.NetCents(), wantNet)
	}
	if totals.TransfersInCents != 20000 {
		t.Errorf("TransfersInCents = %d, want 20000", totals.TransfersInCents)
	}
	if totals.TransfersOutCents != 20000 {
		t.Errorf("TransfersOutCents = %d, want 20000", totals.TransfersOutCents)
	}
}

func TestBuildReport_UnmatchedTransferFlag(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	txns := []classify.ClassifiedTransaction{
		{
			Fingerprint:    "xfr-unmatched",
			TxnType:        classify.TxnTransfer,
			AmountCents:    -5000,
			TransferStatus: ptrTransfer(classify.TransferUnmatched),
		},
	}

	report := classify.BuildReport(txns, classify.TimePeriodMonth, start, end)

	flagFound := false
	for _, f := range report.Integrity.Flags {
		if f == classify.FlagUnmatchedTransfer {
			flagFound = true
		}
	}
	if !flagFound {
		t.Error("expected FlagUnmatchedTransfer in Integrity.Flags")
	}
}

func TestBuildReport_UnclassifiedCreditFlag(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	txns := []classify.ClassifiedTransaction{
		{Fingerprint: "cr1", TxnType: classify.TxnCreditOther, AmountCents: 30000},
		{Fingerprint: "cr2", TxnType: classify.TxnCreditOther, AmountCents: 15000},
	}

	report := classify.BuildReport(txns, classify.TimePeriodMonth, start, end)

	flagFound := false
	for _, f := range report.Integrity.Flags {
		if f == classify.FlagUnclassifiedCredit {
			flagFound = true
		}
	}
	if !flagFound {
		t.Error("expected FlagUnclassifiedCredit in Integrity.Flags")
	}
	if report.Integrity.UnclassifiedCreditCount != 2 {
		t.Errorf("UnclassifiedCreditCount = %d, want 2", report.Integrity.UnclassifiedCreditCount)
	}
	if report.Integrity.UnclassifiedCreditCents != 45000 {
		t.Errorf("UnclassifiedCreditCents = %d, want 45000", report.Integrity.UnclassifiedCreditCents)
	}
}

func TestBuildReport_RefundTotals(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	txns := []classify.ClassifiedTransaction{
		{Fingerprint: "ref1", TxnType: classify.TxnRefund, AmountCents: 2500},
	}

	report := classify.BuildReport(txns, classify.TimePeriodMonth, start, end)

	if report.Totals.RefundsCents != 2500 {
		t.Errorf("RefundsCents = %d, want 2500", report.Totals.RefundsCents)
	}
}

func TestBuildReport_HashIsSet(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	txns := []classify.ClassifiedTransaction{
		{Fingerprint: "fp1", TxnType: classify.TxnIncome, AmountCents: 100000},
	}

	report := classify.BuildReport(txns, classify.TimePeriodMonth, start, end)
	if report.ReportHash == "" {
		t.Error("ReportHash must be non-empty after BuildReport")
	}
}

func TestBuildReportSummary_TopMerchants(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	txns := []classify.ClassifiedTransaction{
		{Fingerprint: "a1", TxnType: classify.TxnExpense, AmountCents: -5000, MerchantNorm: "amazon", SpendingBucket: ptrBucket(classify.BucketDiscretionary)},
		{Fingerprint: "a2", TxnType: classify.TxnExpense, AmountCents: -3000, MerchantNorm: "amazon", SpendingBucket: ptrBucket(classify.BucketDiscretionary)},
		{Fingerprint: "s1", TxnType: classify.TxnExpense, AmountCents: -1500, MerchantNorm: "starbucks", SpendingBucket: ptrBucket(classify.BucketDiscretionary)},
	}

	summary := classify.BuildReportSummary(txns, classify.TimePeriodMonth, start, end, 10)

	if len(summary.TopMerchants) != 2 {
		t.Fatalf("TopMerchants len = %d, want 2", len(summary.TopMerchants))
	}
	// Amazon should be first (8000 > 1500).
	if summary.TopMerchants[0].MerchantNorm != "amazon" {
		t.Errorf("TopMerchants[0].MerchantNorm = %q, want amazon", summary.TopMerchants[0].MerchantNorm)
	}
	if summary.TopMerchants[0].TotalCents != 8000 {
		t.Errorf("TopMerchants[0].TotalCents = %d, want 8000", summary.TopMerchants[0].TotalCents)
	}
	if summary.TopMerchants[0].Count != 2 {
		t.Errorf("TopMerchants[0].Count = %d, want 2", summary.TopMerchants[0].Count)
	}
}

func TestBuildReportSummary_CategoryBreakdown(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	txns := []classify.ClassifiedTransaction{
		{Fingerprint: "g1", TxnType: classify.TxnExpense, AmountCents: -8000, CategoryID: "groceries", SpendingBucket: ptrBucket(classify.BucketVariableEssentials)},
		{Fingerprint: "g2", TxnType: classify.TxnExpense, AmountCents: -2000, CategoryID: "groceries", SpendingBucket: ptrBucket(classify.BucketVariableEssentials)},
		{Fingerprint: "u1", TxnType: classify.TxnExpense, AmountCents: -5000, CategoryID: "utilities", SpendingBucket: ptrBucket(classify.BucketFixedObligations)},
	}

	summary := classify.BuildReportSummary(txns, classify.TimePeriodMonth, start, end, 10)

	if len(summary.CategoryBreakdown) != 2 {
		t.Fatalf("CategoryBreakdown len = %d, want 2", len(summary.CategoryBreakdown))
	}

	// Groceries has 10000 / 15000 total = 66.67%
	groceries := summary.CategoryBreakdown[0]
	if groceries.CategoryID != "groceries" {
		t.Errorf("CategoryBreakdown[0].CategoryID = %q, want groceries", groceries.CategoryID)
	}
	if groceries.TotalCents != 10000 {
		t.Errorf("groceries TotalCents = %d, want 10000", groceries.TotalCents)
	}
	if groceries.Count != 2 {
		t.Errorf("groceries Count = %d, want 2", groceries.Count)
	}

	// Share percentage must be roughly 66.67.
	want := 100.0 * 10000.0 / 15000.0
	if groceries.SharePercent < want-0.01 || groceries.SharePercent > want+0.01 {
		t.Errorf("groceries SharePercent = %.4f, want ~%.4f", groceries.SharePercent, want)
	}
}

func TestBuildReportSummary_IncomeSummary(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	txns := []classify.ClassifiedTransaction{
		{Fingerprint: "i1", TxnType: classify.TxnIncome, AmountCents: 400000},
		{Fingerprint: "i2", TxnType: classify.TxnIncome, AmountCents: 100000},
		{Fingerprint: "e1", TxnType: classify.TxnExpense, AmountCents: -50000, SpendingBucket: ptrBucket(classify.BucketDiscretionary)},
	}

	summary := classify.BuildReportSummary(txns, classify.TimePeriodMonth, start, end, 10)

	if summary.IncomeSummary.TotalCents != 500000 {
		t.Errorf("IncomeSummary.TotalCents = %d, want 500000", summary.IncomeSummary.TotalCents)
	}
	if summary.IncomeSummary.Count != 2 {
		t.Errorf("IncomeSummary.Count = %d, want 2", summary.IncomeSummary.Count)
	}
	if summary.ExpenseSummary.TotalCents != 50000 {
		t.Errorf("ExpenseSummary.TotalCents = %d, want 50000", summary.ExpenseSummary.TotalCents)
	}
	if summary.ExpenseSummary.Count != 1 {
		t.Errorf("ExpenseSummary.Count = %d, want 1", summary.ExpenseSummary.Count)
	}
}

func TestBuildReportSummary_SavingsRate(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	txns := []classify.ClassifiedTransaction{
		{Fingerprint: "i1", TxnType: classify.TxnIncome, AmountCents: 100000},
		{Fingerprint: "e1", TxnType: classify.TxnExpense, AmountCents: -80000, SpendingBucket: ptrBucket(classify.BucketFixedObligations)},
	}

	summary := classify.BuildReportSummary(txns, classify.TimePeriodMonth, start, end, 10)

	// Net = 100000 - 80000 = 20000; savings rate = 20000/100000 = 20%.
	if summary.NetCents != 20000 {
		t.Errorf("NetCents = %d, want 20000", summary.NetCents)
	}
	wantRate := 20.0
	if summary.SavingsRatePct < wantRate-0.01 || summary.SavingsRatePct > wantRate+0.01 {
		t.Errorf("SavingsRatePct = %f, want ~%f", summary.SavingsRatePct, wantRate)
	}
}

func TestBuildReportSummary_ZeroIncomeNoSavingsRate(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	txns := []classify.ClassifiedTransaction{
		{Fingerprint: "e1", TxnType: classify.TxnExpense, AmountCents: -1000, SpendingBucket: ptrBucket(classify.BucketDiscretionary)},
	}

	summary := classify.BuildReportSummary(txns, classify.TimePeriodMonth, start, end, 10)

	if summary.SavingsRatePct != 0 {
		t.Errorf("SavingsRatePct with zero income = %f, want 0", summary.SavingsRatePct)
	}
}

func TestBuildReportSummary_TopNLimit(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	txns := make([]classify.ClassifiedTransaction, 0, 15)
	for i := 0; i < 15; i++ {
		merchants := []string{"m0", "m1", "m2", "m3", "m4", "m5", "m6", "m7", "m8", "m9", "m10", "m11", "m12", "m13", "m14"}
		txns = append(txns, classify.ClassifiedTransaction{
			Fingerprint:  "fp" + merchants[i],
			TxnType:      classify.TxnExpense,
			AmountCents:  int64(-100 * (i + 1)),
			MerchantNorm: merchants[i],
			SpendingBucket: ptrBucket(classify.BucketDiscretionary),
		})
	}

	summary := classify.BuildReportSummary(txns, classify.TimePeriodMonth, start, end, 5)

	if len(summary.TopMerchants) != 5 {
		t.Errorf("TopMerchants len = %d, want 5 (topN limit)", len(summary.TopMerchants))
	}
}

func TestResolutionTasks_Empty(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	report := classify.BuildReport(nil, classify.TimePeriodMonth, start, end)

	tasks := classify.ResolutionTasks(report)
	if len(tasks) != 0 {
		t.Errorf("ResolutionTasks on clean report = %d tasks, want 0", len(tasks))
	}
}

func TestResolutionTasks_UnclassifiedCredit(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	txns := []classify.ClassifiedTransaction{
		{Fingerprint: "cr1", TxnType: classify.TxnCreditOther, AmountCents: 5000},
	}
	report := classify.BuildReport(txns, classify.TimePeriodMonth, start, end)

	tasks := classify.ResolutionTasks(report)
	found := false
	for _, task := range tasks {
		if task.TaskType == "CLASSIFY_CREDIT" {
			found = true
			if task.Priority != 1 {
				t.Errorf("CLASSIFY_CREDIT priority = %d, want 1", task.Priority)
			}
		}
	}
	if !found {
		t.Error("expected CLASSIFY_CREDIT task, not found")
	}
}

func TestResolutionTasks_UnmatchedTransfer(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	txns := []classify.ClassifiedTransaction{
		{Fingerprint: "xfr", TxnType: classify.TxnTransfer, AmountCents: -1000, TransferStatus: ptrTransfer(classify.TransferUnmatched)},
	}
	report := classify.BuildReport(txns, classify.TimePeriodMonth, start, end)

	tasks := classify.ResolutionTasks(report)
	found := false
	for _, task := range tasks {
		if task.TaskType == "MATCH_TRANSFER" {
			found = true
			if task.Priority != 2 {
				t.Errorf("MATCH_TRANSFER priority = %d, want 2", task.Priority)
			}
		}
	}
	if !found {
		t.Error("expected MATCH_TRANSFER task, not found")
	}
}

func TestResolutionTasks_SortedByPriority(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	// Introduce both flags.
	txns := []classify.ClassifiedTransaction{
		{Fingerprint: "cr1", TxnType: classify.TxnCreditOther, AmountCents: 5000},
		{Fingerprint: "xfr", TxnType: classify.TxnTransfer, AmountCents: -1000, TransferStatus: ptrTransfer(classify.TransferUnmatched)},
	}
	report := classify.BuildReport(txns, classify.TimePeriodMonth, start, end)

	tasks := classify.ResolutionTasks(report)
	for i := 1; i < len(tasks); i++ {
		if tasks[i-1].Priority > tasks[i].Priority {
			t.Errorf("tasks not sorted by priority: tasks[%d].Priority=%d > tasks[%d].Priority=%d",
				i-1, tasks[i-1].Priority, i, tasks[i].Priority)
		}
	}
}
