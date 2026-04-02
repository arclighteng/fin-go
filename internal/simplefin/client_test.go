package simplefin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// validAccountsJSON is a minimal but well-formed SimpleFIN /accounts response.
const validAccountsJSON = `{
	"errors": [],
	"accounts": [
		{
			"id": "acct-001",
			"org": {"name": "Test Bank"},
			"name": "Checking",
			"currency": "USD",
			"balance": "1234.56",
			"transactions": [
				{
					"id": "txn-001",
					"posted": 1700000000,
					"amount": "-42.50",
					"description": "Coffee Shop",
					"payee": "STARBUCKS",
					"pending": false
				},
				{
					"id": "txn-002",
					"posted": 1700100000,
					"amount": "2500.00",
					"description": "Direct Deposit",
					"payee": "EMPLOYER",
					"pending": false
				}
			]
		},
		{
			"id": "acct-002",
			"org": {"name": "Credit Union"},
			"name": "Savings",
			"currency": "USD",
			"balance": "5000.00",
			"transactions": []
		}
	]
}`

// ---------------------------------------------------------------------------
// TestFetch_Success
// ---------------------------------------------------------------------------

func TestFetch_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(validAccountsJSON))
	}))
	defer srv.Close()

	c := &Client{
		accessURL: srv.URL,
		http:      srv.Client(),
	}

	result, err := c.Fetch(context.Background(), 30)
	if err != nil {
		t.Fatalf("Fetch: unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Fetch: result must not be nil")
	}

	// Verify accounts.
	if len(result.Accounts) != 2 {
		t.Errorf("Accounts: want 2, got %d", len(result.Accounts))
	}
	if result.Accounts[0].AccountID != "acct-001" {
		t.Errorf("Account[0].AccountID: want acct-001, got %q", result.Accounts[0].AccountID)
	}
	if result.Accounts[0].Institution != "Test Bank" {
		t.Errorf("Account[0].Institution: want Test Bank, got %q", result.Accounts[0].Institution)
	}
	if result.Accounts[0].Name != "Checking" {
		t.Errorf("Account[0].Name: want Checking, got %q", result.Accounts[0].Name)
	}
	if result.Accounts[0].Currency != "USD" {
		t.Errorf("Account[0].Currency: want USD, got %q", result.Accounts[0].Currency)
	}

	// Verify transactions (2 from acct-001, 0 from acct-002 = 2 total).
	if len(result.Transactions) != 2 {
		t.Errorf("Transactions: want 2, got %d", len(result.Transactions))
	}

	// First transaction should be the coffee shop (negative amount).
	txn0 := result.Transactions[0]
	if txn0.AccountID != "acct-001" {
		t.Errorf("Txn[0].AccountID: want acct-001, got %q", txn0.AccountID)
	}
	if txn0.AmountCents != -4250 {
		t.Errorf("Txn[0].AmountCents: want -4250, got %d", txn0.AmountCents)
	}
	if txn0.Merchant != "STARBUCKS" {
		t.Errorf("Txn[0].Merchant: want STARBUCKS, got %q", txn0.Merchant)
	}
	if txn0.Pending {
		t.Error("Txn[0].Pending: want false, got true")
	}
	if txn0.PostedAt.IsZero() {
		t.Error("Txn[0].PostedAt must not be zero")
	}
}

// ---------------------------------------------------------------------------
// TestFetch_AuthFailure
// ---------------------------------------------------------------------------

func TestFetch_AuthFailure(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Unauthorized"))
	}))
	defer srv.Close()

	c := &Client{
		accessURL: srv.URL,
		http:      srv.Client(),
	}

	_, err := c.Fetch(context.Background(), 30)
	if err == nil {
		t.Error("Fetch: want error on 401, got nil")
	}
}

// ---------------------------------------------------------------------------
// TestFetch_ServerError
// ---------------------------------------------------------------------------

func TestFetch_ServerError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer srv.Close()

	c := &Client{
		accessURL: srv.URL,
		http:      srv.Client(),
	}

	_, err := c.Fetch(context.Background(), 30)
	if err == nil {
		t.Error("Fetch: want error on 500, got nil")
	}
}

// ---------------------------------------------------------------------------
// TestFetch_MalformedJSON
// ---------------------------------------------------------------------------

func TestFetch_MalformedJSON(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"accounts": [invalid json`))
	}))
	defer srv.Close()

	c := &Client{
		accessURL: srv.URL,
		http:      srv.Client(),
	}

	_, err := c.Fetch(context.Background(), 30)
	if err == nil {
		t.Error("Fetch: want error on malformed JSON, got nil")
	}
}

// ---------------------------------------------------------------------------
// TestFetch_Timeout
// ---------------------------------------------------------------------------

func TestFetch_Timeout(t *testing.T) {
	t.Parallel()

	// The handler sleeps longer than the context deadline so Fetch should fail.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			// Client cancelled — do nothing.
		case <-time.After(10 * time.Second):
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(validAccountsJSON))
		}
	}))
	defer srv.Close()

	c := &Client{
		accessURL: srv.URL,
		http:      srv.Client(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.Fetch(ctx, 30)
	if err == nil {
		t.Error("Fetch: want error on context timeout, got nil")
	}
}

// ---------------------------------------------------------------------------
// TestFetch_APIErrors
// ---------------------------------------------------------------------------

func TestFetch_APIErrors(t *testing.T) {
	t.Parallel()

	// SimpleFIN can return HTTP 200 with an errors array.
	const errJSON = `{"errors": ["bank connection failed"], "accounts": []}`

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(errJSON))
	}))
	defer srv.Close()

	c := &Client{
		accessURL: srv.URL,
		http:      srv.Client(),
	}

	_, err := c.Fetch(context.Background(), 30)
	if err == nil {
		t.Error("Fetch: want error when API response contains errors, got nil")
	}
}

// ---------------------------------------------------------------------------
// TestNewClient_InvalidURL
// ---------------------------------------------------------------------------

func TestNewClient_InvalidURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
	}{
		{"http scheme", "http://app.simplefin.org/simplefin/token"},
		{"invalid URL", "://bad-url"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewClient(tc.url)
			if err == nil {
				t.Errorf("NewClient(%q): want error, got nil", tc.url)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestFetch_ZeroLookback (no start-date query param)
// ---------------------------------------------------------------------------

func TestFetch_ZeroLookback(t *testing.T) {
	t.Parallel()

	var capturedURL string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"errors":[],"accounts":[]}`))
	}))
	defer srv.Close()

	c := &Client{
		accessURL: srv.URL,
		http:      srv.Client(),
	}

	_, err := c.Fetch(context.Background(), 0)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// When lookbackDays == 0 the start-date param must not be appended.
	if contains := func(s, sub string) bool {
		return len(s) >= len(sub) && func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}()
	}; contains(capturedURL, "start-date") {
		t.Errorf("lookbackDays=0: URL must not contain start-date, got %q", capturedURL)
	}
}
