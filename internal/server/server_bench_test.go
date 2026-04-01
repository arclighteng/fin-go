package server_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arclighteng/fin-go/internal/config"
	findb "github.com/arclighteng/fin-go/internal/db"
	"github.com/arclighteng/fin-go/internal/models"
	"github.com/arclighteng/fin-go/internal/server"
	chimw "github.com/go-chi/chi/v5/middleware"
)

// newBenchHandler builds an in-memory DB, seeds it with test data, and returns
// an http.Handler ready for benchmarking. The DB is closed via b.Cleanup.
// Chi's DefaultLogger is replaced with a no-op to keep benchmark output clean.
func newBenchHandler(b *testing.B) http.Handler {
	b.Helper()

	// Suppress chi's request logger for the duration of this benchmark.
	// middleware.Logger delegates to DefaultLogger which is a package-level var.
	orig := chimw.DefaultLogger
	chimw.DefaultLogger = func(next http.Handler) http.Handler { return next }
	b.Cleanup(func() { chimw.DefaultLogger = orig })

	d, err := findb.Connect(":memory:")
	if err != nil {
		b.Fatalf("db.Connect: %v", err)
	}
	if err := d.Init(); err != nil {
		d.Close()
		b.Fatalf("DB.Init: %v", err)
	}
	b.Cleanup(func() { d.Close() })

	// Seed accounts.
	accounts := []models.Account{
		{AccountID: "acc-checking", Institution: "First National", Name: "Checking", Type: "checking", Currency: "USD"},
		{AccountID: "acc-savings", Institution: "First National", Name: "Savings", Type: "savings", Currency: "USD"},
	}
	if err := d.UpsertAccounts(accounts); err != nil {
		b.Fatalf("UpsertAccounts: %v", err)
	}

	// Seed transactions.
	merchants := []string{"Starbucks", "Amazon", "Whole Foods", "Netflix", "Shell"}
	txns := make([]models.Transaction, 50)
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range txns {
		txns[i] = models.Transaction{
			AccountID:   "acc-checking",
			PostedAt:    base.AddDate(0, 0, i),
			AmountCents: int64(-(i%100+1) * 100),
			Currency:    "USD",
			Merchant:    merchants[i%len(merchants)],
			Description: fmt.Sprintf("Purchase %d", i),
			SourceTxnID: fmt.Sprintf("src-%04d", i),
			Fingerprint: fmt.Sprintf("fp-%04d", i),
		}
	}
	if _, _, err := d.UpsertTransactions(txns); err != nil {
		b.Fatalf("UpsertTransactions: %v", err)
	}

	// Seed a couple of sync runs for sync-status.
	for i := 0; i < 3; i++ {
		if err := d.RecordRun(30, 50, 50, 0); err != nil {
			b.Fatalf("RecordRun: %v", err)
		}
	}

	cfg := &config.Config{Timezone: "UTC"}

	// Pass empty template/static dirs; the server falls back to stub handlers
	// for HTML routes but all JSON API routes remain fully functional.
	return server.New(d, cfg, "", "", "bench")
}

func BenchmarkHealthEndpoint(b *testing.B) {
	h := newBenchHandler(b)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("unexpected status %d", rec.Code)
		}
	}
}

func BenchmarkAPIAccounts(b *testing.B) {
	h := newBenchHandler(b)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("unexpected status %d", rec.Code)
		}
	}
}

func BenchmarkAPISyncStatus(b *testing.B) {
	h := newBenchHandler(b)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/sync-status", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("unexpected status %d", rec.Code)
		}
	}
}
