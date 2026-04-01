package db_test

import (
	"fmt"
	"testing"
	"time"

	findb "github.com/arclighteng/fin-go/internal/db"
	"github.com/arclighteng/fin-go/internal/models"
)

// newBenchDB opens an in-memory SQLite database and initialises the schema.
// It inserts a single account so foreign-key-style constraints are satisfied.
func newBenchDB(b *testing.B) *findb.DB {
	b.Helper()

	d, err := findb.Connect(":memory:")
	if err != nil {
		b.Fatalf("findb.Connect: %v", err)
	}
	if err := d.Init(); err != nil {
		d.Close()
		b.Fatalf("Init: %v", err)
	}

	// Seed one account so transactions have a valid account_id.
	if err := d.UpsertAccounts([]models.Account{
		{AccountID: "acc-bench", Institution: "Bench Bank", Name: "Checking", Type: "checking", Currency: "USD"},
	}); err != nil {
		d.Close()
		b.Fatalf("UpsertAccounts setup: %v", err)
	}

	b.Cleanup(func() { d.Close() })
	return d
}

// makeTxns generates n unique transactions for the given accountID.
func makeTxns(accountID string, n int) []models.Transaction {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	merchants := []string{"Starbucks", "Amazon", "Whole Foods", "Netflix", "Shell"}

	txns := make([]models.Transaction, n)
	for i := range txns {
		txns[i] = models.Transaction{
			AccountID:   accountID,
			PostedAt:    base.AddDate(0, 0, i%365),
			AmountCents: int64(-(i%100+1)*100),
			Currency:    "USD",
			Description: fmt.Sprintf("Purchase at %s", merchants[i%len(merchants)]),
			Merchant:    merchants[i%len(merchants)],
			SourceTxnID: fmt.Sprintf("src-%07d", i),
			Fingerprint: fmt.Sprintf("fp-%07d", i),
		}
	}
	return txns
}

func BenchmarkUpsertTransactions(b *testing.B) {
	// Each sub-benchmark gets a fresh DB so inserts are always novel.
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		d := func() *findb.DB {
			inner, err := findb.Connect(":memory:")
			if err != nil {
				b.Fatalf("findb.Connect: %v", err)
			}
			if err := inner.Init(); err != nil {
				inner.Close()
				b.Fatalf("Init: %v", err)
			}
			if err := inner.UpsertAccounts([]models.Account{
				{AccountID: "acc-b", Institution: "B", Name: "B", Type: "checking", Currency: "USD"},
			}); err != nil {
				inner.Close()
				b.Fatalf("UpsertAccounts: %v", err)
			}
			return inner
		}()
		txns := makeTxns("acc-b", 100)
		b.StartTimer()

		if _, _, err := d.UpsertTransactions(txns); err != nil {
			b.Fatalf("UpsertTransactions: %v", err)
		}

		b.StopTimer()
		d.Close()
		b.StartTimer()
	}
}

func BenchmarkGetTransactions(b *testing.B) {
	d := newBenchDB(b)

	// Pre-load 10 000 transactions spread over the full year 2024.
	txns := makeTxns("acc-bench", 10_000)
	if _, _, err := d.UpsertTransactions(txns); err != nil {
		b.Fatalf("seed UpsertTransactions: %v", err)
	}

	// Query a 90-day window that covers a large portion of the rows.
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)

	b.ReportAllocs()
	b.ResetTimer()

	var result []models.Transaction
	for i := 0; i < b.N; i++ {
		var err error
		result, err = d.GetTransactions(start, end)
		if err != nil {
			b.Fatalf("GetTransactions: %v", err)
		}
	}
	_ = result
}

func BenchmarkRunsInLast24Hours(b *testing.B) {
	d := newBenchDB(b)

	// Record a handful of runs so the query is non-trivial.
	for i := 0; i < 10; i++ {
		if err := d.RecordRun(30, 100, 90, 10); err != nil {
			b.Fatalf("RecordRun: %v", err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()

	var count int
	for i := 0; i < b.N; i++ {
		var err error
		count, err = d.RunsInLast24Hours()
		if err != nil {
			b.Fatalf("RunsInLast24Hours: %v", err)
		}
	}
	_ = count
}
