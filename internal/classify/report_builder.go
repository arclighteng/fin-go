// report_builder.go provides pure-Go report generation from pre-classified transactions.
//
// The functions here operate on []ClassifiedTransaction values already in memory,
// unlike ReportPeriod (reporting.go) which queries the database directly. Use
// BuildReport when you have classified transactions available without a database
// connection, such as in tests or offline export pipelines.
//
// Truth Contract compliance:
//   - All spending bucket tallies are absolute values (expenses are stored negative).
//   - Transfers are excluded from net income/spend calculations.
//   - Integrity score gates recommendations at 0.8.
package classify

import (
	"fmt"
	"sort"
	"time"
)

// TimePeriod identifies the granularity of a report period.
type TimePeriod int

const (
	// TimePeriodCustom is an arbitrary date range.
	TimePeriodCustom TimePeriod = iota
	// TimePeriodMonth is a calendar month.
	TimePeriodMonth
	// TimePeriodQuarter is a calendar quarter (3 months).
	TimePeriodQuarter
	// TimePeriodYear is a calendar year.
	TimePeriodYear
)

// CategorySummary holds aggregated totals for a single expense category.
type CategorySummary struct {
	// CategoryID is the category identifier (e.g. "groceries", "utilities").
	CategoryID string
	// TotalCents is the total absolute spend for this category in the period.
	TotalCents int64
	// Count is the number of transactions in this category.
	Count int
	// SharePercent is TotalCents as a percentage of total expense spending (0–100).
	SharePercent float64
}

// MerchantSummary holds aggregated totals for a single merchant.
type MerchantSummary struct {
	// MerchantNorm is the normalized merchant name.
	MerchantNorm string
	// TotalCents is the total absolute spend at this merchant in the period.
	TotalCents int64
	// Count is the number of transactions at this merchant.
	Count int
}

// BucketSummary holds totals for a single spending bucket.
type BucketSummary struct {
	// Bucket is the spending bucket.
	Bucket SpendingBucket
	// TotalCents is the total absolute spend in this bucket.
	TotalCents int64
	// Count is the number of transactions in this bucket.
	Count int
	// SharePercent is TotalCents as a percentage of total expenses (0–100).
	SharePercent float64
}

// ReportSummary is an extended view over a Report, adding derived breakdowns.
// It is produced by BuildReportSummary and is safe to cache.
type ReportSummary struct {
	// Report is the canonical base report.
	Report *Report

	// TopMerchants is the top-N expense merchants sorted by TotalCents descending.
	TopMerchants []MerchantSummary

	// CategoryBreakdown lists all non-zero expense categories, sorted by TotalCents descending.
	CategoryBreakdown []CategorySummary

	// BucketBreakdown lists all non-zero spending buckets, sorted by TotalCents descending.
	BucketBreakdown []BucketSummary

	// IncomeSummary totals income transactions.
	IncomeSummary struct {
		TotalCents int64
		Count      int
	}

	// ExpenseSummary totals expense transactions (absolute values).
	ExpenseSummary struct {
		TotalCents int64
		Count      int
	}

	// TransferSummary totals transfer transactions.
	TransferSummary struct {
		InCents  int64
		OutCents int64
		Count    int
	}

	// NetCents is income minus total expenses (excluding transfers).
	NetCents int64

	// SavingsRatePct is NetCents / IncomeCents * 100, or 0 when income is 0.
	SavingsRatePct float64
}

// BuildReport assembles a Report from pre-classified transactions.
//
// It tallies each transaction into PeriodTotals following the Truth Contract:
//   - Expenses are stored with their signed (negative) amount; bucket tallies use absolute value.
//   - Transfers are excluded from net calculations.
//   - Integrity flags are derived from the supplied transactions.
//
// The returned Report has ReportHash set and can be passed directly to web/CLI layers.
func BuildReport(
	txns []ClassifiedTransaction,
	period TimePeriod,
	start, end time.Time,
) *Report {
	var totals PeriodTotals
	var integrityFlags []IntegrityFlag

	var unclassifiedCreditCount int
	var unclassifiedCreditCents int64
	hasUnmatchedTransfer := false

	for i := range txns {
		txn := &txns[i]

		switch txn.TxnType {
		case TxnIncome:
			totals.IncomeCents += txn.AmountCents

		case TxnExpense:
			absAmt := txn.AmountCents
			if absAmt < 0 {
				absAmt = -absAmt
			}
			bucket := BucketDiscretionary
			if txn.SpendingBucket != nil {
				bucket = *txn.SpendingBucket
			}
			switch bucket {
			case BucketFixedObligations:
				totals.FixedObligationsCents += absAmt
			case BucketVariableEssentials:
				totals.VariableEssentialsCents += absAmt
			case BucketOneOffs:
				totals.OneOffsCents += absAmt
			default:
				totals.DiscretionaryCents += absAmt
			}

		case TxnTransfer:
			if txn.AmountCents > 0 {
				totals.TransfersInCents += txn.AmountCents
			} else {
				totals.TransfersOutCents += -txn.AmountCents
			}
			if txn.TransferStatus != nil && *txn.TransferStatus == TransferUnmatched {
				hasUnmatchedTransfer = true
			}

		case TxnRefund:
			totals.RefundsCents += txn.AmountCents

		case TxnCreditOther:
			totals.CreditsOtherCents += txn.AmountCents
			unclassifiedCreditCount++
			unclassifiedCreditCents += txn.AmountCents
		}
	}

	// Build integrity flags.
	if unclassifiedCreditCount > 0 {
		integrityFlags = append(integrityFlags, FlagUnclassifiedCredit)
	}
	if hasUnmatchedTransfer {
		integrityFlags = append(integrityFlags, FlagUnmatchedTransfer)
	}

	// Count unmatched transfers for the integrity report.
	unmatchedCount := 0
	for i := range txns {
		if txns[i].TxnType == TxnTransfer &&
			txns[i].TransferStatus != nil &&
			*txns[i].TransferStatus == TransferUnmatched {
			unmatchedCount++
		}
	}

	integrity := IntegrityReport{
		Flags:                   integrityFlags,
		UnmatchedTransferCount:  unmatchedCount,
		UnclassifiedCreditCount: unclassifiedCreditCount,
		UnclassifiedCreditCents: unclassifiedCreditCents,
	}

	label := buildPeriodLabel(period, start, end)

	report := &Report{
		PeriodLabel:       label,
		StartDate:         start,
		EndDate:           end,
		Totals:            totals,
		Transactions:      txns,
		Integrity:         integrity,
		ClassifierVersion: ClassifierVersion,
		ReportVersion:     ReportVersion,
		TransactionCount:  len(txns),
	}

	report.ReportHash = computeReportHash(report)
	return report
}

// BuildReportSummary builds a Report and enriches it with derived breakdowns
// (top merchants, category breakdown, bucket breakdown, income/expense summaries).
//
// topN controls how many top merchants are included; pass 10 for a typical view.
func BuildReportSummary(
	txns []ClassifiedTransaction,
	period TimePeriod,
	start, end time.Time,
	topN int,
) *ReportSummary {
	report := BuildReport(txns, period, start, end)

	summary := &ReportSummary{
		Report: report,
	}

	// --- Merchant aggregation (expenses only) ---
	merchantTotals := make(map[string]*MerchantSummary)
	for i := range txns {
		txn := &txns[i]
		if txn.TxnType != TxnExpense {
			continue
		}
		absAmt := txn.AmountCents
		if absAmt < 0 {
			absAmt = -absAmt
		}
		key := txn.MerchantNorm
		if key == "" {
			key = txn.RawDescription
		}
		ms := merchantTotals[key]
		if ms == nil {
			ms = &MerchantSummary{MerchantNorm: key}
			merchantTotals[key] = ms
		}
		ms.TotalCents += absAmt
		ms.Count++
	}

	merchants := make([]MerchantSummary, 0, len(merchantTotals))
	for _, ms := range merchantTotals {
		merchants = append(merchants, *ms)
	}
	sort.Slice(merchants, func(i, j int) bool {
		return merchants[i].TotalCents > merchants[j].TotalCents
	})
	if topN > 0 && len(merchants) > topN {
		merchants = merchants[:topN]
	}
	summary.TopMerchants = merchants

	// --- Category breakdown (expenses only) ---
	categoryTotals := make(map[string]*CategorySummary)
	for i := range txns {
		txn := &txns[i]
		if txn.TxnType != TxnExpense {
			continue
		}
		absAmt := txn.AmountCents
		if absAmt < 0 {
			absAmt = -absAmt
		}
		catID := txn.CategoryID
		if catID == "" {
			catID = "other"
		}
		cs := categoryTotals[catID]
		if cs == nil {
			cs = &CategorySummary{CategoryID: catID}
			categoryTotals[catID] = cs
		}
		cs.TotalCents += absAmt
		cs.Count++
	}

	totalExpenses := report.Totals.TotalExpensesCents()

	categories := make([]CategorySummary, 0, len(categoryTotals))
	for _, cs := range categoryTotals {
		if totalExpenses > 0 {
			cs.SharePercent = float64(cs.TotalCents) / float64(totalExpenses) * 100
		}
		categories = append(categories, *cs)
	}
	sort.Slice(categories, func(i, j int) bool {
		return categories[i].TotalCents > categories[j].TotalCents
	})
	summary.CategoryBreakdown = categories

	// --- Bucket breakdown ---
	bucketData := []struct {
		bucket SpendingBucket
		cents  int64
	}{
		{BucketFixedObligations, report.Totals.FixedObligationsCents},
		{BucketVariableEssentials, report.Totals.VariableEssentialsCents},
		{BucketDiscretionary, report.Totals.DiscretionaryCents},
		{BucketOneOffs, report.Totals.OneOffsCents},
	}

	// Count transactions per bucket.
	bucketCounts := make(map[SpendingBucket]int)
	for i := range txns {
		txn := &txns[i]
		if txn.TxnType != TxnExpense {
			continue
		}
		b := BucketDiscretionary
		if txn.SpendingBucket != nil {
			b = *txn.SpendingBucket
		}
		bucketCounts[b]++
	}

	buckets := make([]BucketSummary, 0, 4)
	for _, bd := range bucketData {
		if bd.cents == 0 {
			continue
		}
		var sharePct float64
		if totalExpenses > 0 {
			sharePct = float64(bd.cents) / float64(totalExpenses) * 100
		}
		buckets = append(buckets, BucketSummary{
			Bucket:       bd.bucket,
			TotalCents:   bd.cents,
			Count:        bucketCounts[bd.bucket],
			SharePercent: sharePct,
		})
	}
	sort.Slice(buckets, func(i, j int) bool {
		return buckets[i].TotalCents > buckets[j].TotalCents
	})
	summary.BucketBreakdown = buckets

	// --- Income / expense / transfer summaries ---
	for i := range txns {
		txn := &txns[i]
		switch txn.TxnType {
		case TxnIncome:
			summary.IncomeSummary.TotalCents += txn.AmountCents
			summary.IncomeSummary.Count++
		case TxnExpense:
			absAmt := txn.AmountCents
			if absAmt < 0 {
				absAmt = -absAmt
			}
			summary.ExpenseSummary.TotalCents += absAmt
			summary.ExpenseSummary.Count++
		case TxnTransfer:
			summary.TransferSummary.Count++
			if txn.AmountCents > 0 {
				summary.TransferSummary.InCents += txn.AmountCents
			} else {
				summary.TransferSummary.OutCents += -txn.AmountCents
			}
		}
	}

	// Net and savings rate.
	summary.NetCents = report.Totals.NetCents()
	if summary.IncomeSummary.TotalCents > 0 {
		summary.SavingsRatePct = float64(summary.NetCents) / float64(summary.IncomeSummary.TotalCents) * 100
	}

	return summary
}

// ResolutionTasks derives the set of actions the user should take to resolve
// integrity issues in the report. Tasks are sorted by priority (1 = highest).
// An empty slice is returned when the report is clean.
func ResolutionTasks(report *Report) []ResolutionTask {
	ir := &report.Integrity
	var tasks []ResolutionTask

	for _, flag := range ir.Flags {
		switch flag {
		case FlagUnclassifiedCredit:
			tasks = append(tasks, ResolutionTask{
				TaskType: "CLASSIFY_CREDIT",
				Description: fmt.Sprintf(
					"Classify %d unclassified credit(s) totalling $%.2f",
					ir.UnclassifiedCreditCount,
					float64(ir.UnclassifiedCreditCents)/100.0,
				),
				Priority:      1,
				AffectedCents: ir.UnclassifiedCreditCents,
			})

		case FlagUnmatchedTransfer:
			tasks = append(tasks, ResolutionTask{
				TaskType: "MATCH_TRANSFER",
				Description: fmt.Sprintf(
					"Match or classify %d unmatched transfer(s)",
					ir.UnmatchedTransferCount,
				),
				Priority:      2,
				AffectedCents: 0,
			})

		case FlagReconciliationFailed:
			delta := ir.ReconciliationDeltaCents
			if delta < 0 {
				delta = -delta
			}
			tasks = append(tasks, ResolutionTask{
				TaskType:      "RECONCILE",
				Description:   fmt.Sprintf("Reconcile statement (delta: $%.2f)", float64(delta)/100.0),
				Priority:      1,
				AffectedCents: delta,
			})

		case FlagDuplicateSuspected:
			tasks = append(tasks, ResolutionTask{
				TaskType:      "REVIEW_DUPLICATES",
				Description:   fmt.Sprintf("Review %d suspected duplicate(s)", ir.DuplicateSuspectCount),
				Priority:      3,
				AffectedCents: 0,
			})
		}
	}

	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].Priority < tasks[j].Priority
	})
	return tasks
}

// buildPeriodLabel produces a human-readable period label from the TimePeriod
// and date bounds.
func buildPeriodLabel(period TimePeriod, start, end time.Time) string {
	switch period {
	case TimePeriodMonth:
		return start.Format("Jan 2006")
	case TimePeriodQuarter:
		q := (int(start.Month())-1)/3 + 1
		return fmt.Sprintf("Q%d %d", q, start.Year())
	case TimePeriodYear:
		return fmt.Sprintf("%d", start.Year())
	default:
		return fmt.Sprintf("%s to %s", start.Format("2006-01-02"), end.Format("2006-01-02"))
	}
}
