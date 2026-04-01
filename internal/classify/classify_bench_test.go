package classify_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/arclighteng/fin-go/internal/classify"
)

// syntheticTxns builds a slice of ClassifiedTransaction values with a mix of
// income, expenses, and transfers, spread across common merchants.
func syntheticTxns(n int) []classify.ClassifiedTransaction {
	merchants := []struct {
		norm   string
		amount int64
		kind   classify.TransactionType
	}{
		{"starbucks", -599, classify.TxnExpense},
		{"whole foods market", -8734, classify.TxnExpense},
		{"netflix", -1599, classify.TxnExpense},
		{"amazon", -4299, classify.TxnExpense},
		{"shell gas station", -5200, classify.TxnExpense},
		{"direct deposit payroll", 320000, classify.TxnIncome},
		{"zelle transfer", 5000, classify.TxnTransfer},
		{"doordash", -3245, classify.TxnExpense},
		{"chase credit card payment", -75000, classify.TxnTransfer},
		{"cvs pharmacy", -2350, classify.TxnExpense},
	}

	txns := make([]classify.ClassifiedTransaction, n)
	for i := range txns {
		m := merchants[i%len(merchants)]
		bucket := classify.BucketDiscretionary
		var bucketPtr *classify.SpendingBucket
		if m.kind == classify.TxnExpense {
			bucketPtr = &bucket
		}
		txns[i] = classify.ClassifiedTransaction{
			Fingerprint:    fmt.Sprintf("fp-%06d", i),
			AccountID:      "acc-checking",
			PostedAt:       time.Date(2025, 1, 1+i%28, 0, 0, 0, 0, time.UTC),
			AmountCents:    m.amount,
			MerchantNorm:   m.norm,
			RawDescription: m.norm,
			TxnType:        m.kind,
			SpendingBucket: bucketPtr,
			CategoryID:     "other",
			Reason: classify.ClassificationReason{
				PrimaryCode: classify.EvidenceExpenseDefault,
				Confidence:  0.8,
			},
		}
	}
	return txns
}

var sinkResult classify.ClassificationResult
var sinkReport *classify.Report
var sinkSummary *classify.ReportSummary

func BenchmarkClassifyTransaction(b *testing.B) {
	cases := []struct {
		name         string
		amountCents  int64
		merchantNorm string
		isCCAccount  bool
	}{
		{"expense_typical", -4299, "amazon", false},
		{"income_payroll", 320000, "direct deposit payroll", false},
		{"transfer_zelle", 10000, "zelle transfer", false},
		{"expense_coffee", -599, "starbucks", false},
		{"cc_payment_positive", 75000, "chase credit card payment", true},
	}

	registry := classify.NewOverrideRegistry()

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			var r classify.ClassificationResult
			for i := 0; i < b.N; i++ {
				r = classify.ClassifyTransaction(
					tc.amountCents,
					tc.merchantNorm,
					tc.isCCAccount,
					nil,   // no pattern
					registry,
					"fp-bench",
					false, // not transfer paired
					"",    // no refund match
				)
			}
			sinkResult = r
		})
	}
}

func BenchmarkClassifyBatch(b *testing.B) {
	registry := classify.NewOverrideRegistry()

	inputs := make([]struct {
		amount   int64
		merchant string
	}, 1000)
	merchants := []struct {
		merchant string
		amount   int64
	}{
		{"starbucks", -599},
		{"whole foods market", -8734},
		{"direct deposit payroll", 320000},
		{"zelle transfer", 5000},
		{"netflix", -1599},
	}
	for i := range inputs {
		m := merchants[i%len(merchants)]
		inputs[i].amount = m.amount
		inputs[i].merchant = m.merchant
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for j, inp := range inputs {
			sinkResult = classify.ClassifyTransaction(
				inp.amount,
				inp.merchant,
				false,
				nil,
				registry,
				fmt.Sprintf("fp-%d", j),
				false,
				"",
			)
		}
	}
}

func BenchmarkBuildReport(b *testing.B) {
	txns := syntheticTxns(500)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		sinkReport = classify.BuildReport(txns, classify.TimePeriodMonth, start, end)
	}
}

func BenchmarkBuildReportSummary(b *testing.B) {
	txns := syntheticTxns(500)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		sinkSummary = classify.BuildReportSummary(txns, classify.TimePeriodMonth, start, end, 10)
	}
}
