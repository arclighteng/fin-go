package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arclighteng/fin-go/internal/config"
	"github.com/arclighteng/fin-go/internal/db"
	"github.com/arclighteng/fin-go/internal/models"
	"github.com/arclighteng/fin-go/internal/server"
)

// newTestServer creates a server backed by an in-memory database.
// The returned handler is ready to receive requests.
func newTestServer(t *testing.T) http.Handler {
	t.Helper()

	database, err := db.Connect(":memory:")
	if err != nil {
		t.Fatalf("db.Connect: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	if err := database.Init(); err != nil {
		t.Fatalf("database.Init: %v", err)
	}

	cfg := &config.Config{
		Timezone: "UTC",
	}

	return server.New(database, cfg, "test")
}

// newTestServerWithDB creates a server and exposes the database for seeding.
func newTestServerWithDB(t *testing.T) (http.Handler, *db.DB) {
	t.Helper()

	database, err := db.Connect(":memory:")
	if err != nil {
		t.Fatalf("db.Connect: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	if err := database.Init(); err != nil {
		t.Fatalf("database.Init: %v", err)
	}

	cfg := &config.Config{
		Timezone: "UTC",
	}

	return server.New(database, cfg, "test"), database
}

// ---------------------------------------------------------------------------
// Health & Version
// ---------------------------------------------------------------------------

func TestHealthEndpoint(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /health: want status 200, got %d", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := body["status"]; got != "ok" {
		t.Errorf("GET /health: want status=ok, got %q", got)
	}
}

func TestAPIVersion(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/version: want status 200, got %d", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := body["version"]; !ok {
		t.Errorf("GET /api/version: response missing 'version' field; body=%v", body)
	}
}

// ---------------------------------------------------------------------------
// Accounts
// ---------------------------------------------------------------------------

func TestAPIAccounts_Empty(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/accounts (empty): want 200, got %d", w.Code)
	}

	// Body must be a JSON array (possibly empty).
	body := strings.TrimSpace(w.Body.String())
	if !strings.HasPrefix(body, "[") {
		t.Errorf("GET /api/accounts (empty): want JSON array, got %q", body)
	}

	// Decode to confirm it parses as a slice (nil or empty).
	var accounts []any
	if err := json.Unmarshal([]byte(body), &accounts); err != nil {
		t.Errorf("GET /api/accounts (empty): cannot decode array: %v", err)
	}
}

func TestAPIAccounts_WithData(t *testing.T) {
	t.Parallel()

	h, database := newTestServerWithDB(t)

	// Seed two accounts.
	accts := []models.Account{
		{AccountID: "acct-1", Institution: "First Bank", Name: "Checking", Type: "checking", Currency: "USD"},
		{AccountID: "acct-2", Institution: "Second Bank", Name: "Savings", Type: "savings", Currency: "USD"},
	}
	if err := database.UpsertAccounts(accts); err != nil {
		t.Fatalf("seed accounts: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/accounts (seeded): want 200, got %d", w.Code)
	}

	var got []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode /api/accounts body: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("GET /api/accounts (seeded): want 2 accounts, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// Sync Status
// ---------------------------------------------------------------------------

func TestAPISyncStatus_NoSyncs(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/sync-status", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/sync-status: want 200, got %d", w.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode /api/sync-status: %v", err)
	}

	// syncs_today must be 0 for an empty DB.
	syncsToday, ok := body["syncs_today"]
	if !ok {
		t.Errorf("GET /api/sync-status: missing 'syncs_today' field")
	} else {
		// JSON numbers decode to float64.
		if syncsToday.(float64) != 0 {
			t.Errorf("GET /api/sync-status: want syncs_today=0, got %v", syncsToday)
		}
	}

	// can_sync must be true.
	canSync, ok := body["can_sync"]
	if !ok {
		t.Errorf("GET /api/sync-status: missing 'can_sync' field")
	} else if canSync.(bool) != true {
		t.Errorf("GET /api/sync-status: want can_sync=true, got %v", canSync)
	}
}

// ---------------------------------------------------------------------------
// Sync POST
// ---------------------------------------------------------------------------

func TestAPISync_NoCredentials(t *testing.T) {
	// t.Setenv requires serial execution -- t.Parallel() cannot be used here.
	// Ensure no env var leaks into the test -- the handler checks keyring then
	// falls back to SIMPLEFIN_ACCESS_URL. With no keyring entry and no env var,
	// it should return 400.
	t.Setenv("SIMPLEFIN_ACCESS_URL", "")

	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	req.Header.Set("X-Fin-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	// The handler returns 400 when no credentials are configured.
	if w.Code != http.StatusBadRequest {
		t.Errorf("POST /api/sync (no creds): want 400, got %d; body=%s", w.Code, w.Body.String())
	}
}

func TestAPISync_RateLimited(t *testing.T) {
	t.Parallel()

	h, database := newTestServerWithDB(t)

	// Seed 20 runs within the last 24 hours so the rate limit is hit.
	now := time.Now().UTC()
	for i := 0; i < 20; i++ {
		ranAt := now.Add(-time.Duration(i) * time.Minute).Format(time.RFC3339)
		_, err := database.Underlying().Exec(
			"INSERT INTO runs(ran_at, lookback_days, txns_fetched, txns_inserted, txns_updated) VALUES (?, 30, 0, 0, 0)",
			ranAt,
		)
		if err != nil {
			t.Fatalf("seed run %d: %v", i, err)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	req.Header.Set("X-Fin-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("POST /api/sync (rate limited): want 429, got %d; body=%s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Categories
// ---------------------------------------------------------------------------

func TestAPICategories(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/categories", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/categories: want 200, got %d", w.Code)
	}

	var cats []map[string]string
	if err := json.NewDecoder(w.Body).Decode(&cats); err != nil {
		t.Fatalf("decode /api/categories: %v", err)
	}
	if len(cats) == 0 {
		t.Errorf("GET /api/categories: want non-empty list")
	}
	// Each entry must have the required fields.
	for i, c := range cats {
		for _, field := range []string{"id", "name"} {
			if c[field] == "" {
				t.Errorf("GET /api/categories: entry %d missing field %q", i, field)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Error paths
// ---------------------------------------------------------------------------

func TestAPISearch_MissingQuery(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/search", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("GET /api/search (no q): want 400, got %d", w.Code)
	}
}

func TestAPIIncomeSource_EmptyMerchant(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)

	tests := []struct {
		name string
		body string
	}{
		{"empty merchant field", `{"merchant":"","is_income":true}`},
		{"whitespace merchant", `{"merchant":"   ","is_income":true}`},
		{"missing merchant key", `{"is_income":true}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodPost, "/api/income-source",
				bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Fin-Request", "1")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("POST /api/income-source (%s): want 400, got %d; body=%s",
					tc.name, w.Code, w.Body.String())
			}
		})
	}
}

func TestAPICategoryOverride_UnknownCategory(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)

	body := `{"merchant":"starbucks","category_id":"nonexistent_category_xyz"}`
	req := httptest.NewRequest(http.MethodPost, "/api/category-override",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fin-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("POST /api/category-override (bad category): want 400, got %d; body=%s",
			w.Code, w.Body.String())
	}
}

func TestAPICategoryOverride_EmptyMerchant(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)

	body := `{"merchant":"","category_id":"dining"}`
	req := httptest.NewRequest(http.MethodPost, "/api/category-override",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fin-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("POST /api/category-override (empty merchant): want 400, got %d", w.Code)
	}
}

func TestAPIIncomeSource_InvalidJSON(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/income-source",
		bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fin-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("POST /api/income-source (bad JSON): want 400, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Security headers -- Requires Phase 1+2 changes
// ---------------------------------------------------------------------------

func TestSecurityHeaders_XFrameOptions(t *testing.T) {
	// Requires Phase 1+2 changes: security headers middleware not yet added.
	t.Parallel()

	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	got := w.Header().Get("X-Frame-Options")
	if got != "DENY" {
		t.Errorf("X-Frame-Options: want DENY, got %q", got)
	}
}

func TestSecurityHeaders_ContentTypeOptions(t *testing.T) {
	// Requires Phase 1+2 changes: security headers middleware not yet added.
	t.Parallel()

	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	got := w.Header().Get("X-Content-Type-Options")
	if got != "nosniff" {
		t.Errorf("X-Content-Type-Options: want nosniff, got %q", got)
	}
}

func TestSecurityHeaders_CSP(t *testing.T) {
	// Requires Phase 1+2 changes: security headers middleware not yet added.
	t.Parallel()

	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	got := w.Header().Get("Content-Security-Policy")
	if got == "" {
		t.Errorf("Content-Security-Policy: want non-empty header, got empty")
	}
}

func TestSecurityHeaders_ReferrerPolicy(t *testing.T) {
	// Requires Phase 1+2 changes: security headers middleware not yet added.
	t.Parallel()

	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	got := w.Header().Get("Referrer-Policy")
	if got != "strict-origin-when-cross-origin" {
		t.Errorf("Referrer-Policy: want strict-origin-when-cross-origin, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Cache stubs removed -- Requires Phase 1+2 changes
// ---------------------------------------------------------------------------

func TestCacheStatsRemoved(t *testing.T) {
	// Requires Phase 1+2 changes: /api/cache/stats route should be removed (404).
	t.Parallel()

	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/cache/stats", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("GET /api/cache/stats: want 404 (route removed), got %d", w.Code)
	}
}

func TestCacheClearRemoved(t *testing.T) {
	// Requires Phase 1+2 changes: /api/cache/clear route should be removed (404).
	t.Parallel()

	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/cache/clear", nil)
	req.Header.Set("X-Fin-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("POST /api/cache/clear: want 404 (route removed), got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Income sources
// ---------------------------------------------------------------------------

func TestAPIIncomeSources_Empty(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/income-sources", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/income-sources (empty): want 200, got %d", w.Code)
	}

	// Body must be a JSON array (possibly empty or null).
	var sources []string
	if err := json.NewDecoder(w.Body).Decode(&sources); err != nil {
		t.Errorf("GET /api/income-sources (empty): cannot decode array: %v", err)
	}
	if len(sources) != 0 {
		t.Errorf("GET /api/income-sources (empty): want empty list, got %v", sources)
	}
}

// ---------------------------------------------------------------------------
// Alert action
// ---------------------------------------------------------------------------

func TestAPIAlertAction_MissingKey(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)

	tests := []struct {
		name string
		body string
	}{
		{"missing alert_key", `{"action":"dismiss"}`},
		{"missing action", `{"alert_key":"some-key"}`},
		{"both empty", `{"alert_key":"","action":""}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodPost, "/api/alert-action",
				bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Fin-Request", "1")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("POST /api/alert-action (%s): want 400, got %d; body=%s",
					tc.name, w.Code, w.Body.String())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Transaction annotations
// ---------------------------------------------------------------------------

func TestAPIGetAnnotations_UnknownFingerprint(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/transaction/unknown123/annotations", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	// A fingerprint that does not exist still returns 200 with empty note/tags.
	if w.Code != http.StatusOK {
		t.Errorf("GET /api/transaction/unknown123/annotations: want 200, got %d", w.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode annotations response: %v", err)
	}
	if _, ok := body["fingerprint"]; !ok {
		t.Errorf("annotations response missing 'fingerprint' field; body=%v", body)
	}
}

func TestAPISaveNote(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)
	fp := "test-fingerprint-save-note"

	body := `{"note":"this is my note"}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/transaction/"+fp+"/note",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fin-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("POST /api/transaction/%s/note: want 200, got %d; body=%s",
			fp, w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode note save response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("note save: want status=ok, got %q", resp["status"])
	}
}

func TestAPIDeleteNote(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)
	fp := "test-fingerprint-delete-note"

	// Delete on a non-existent note must still succeed (idempotent).
	req := httptest.NewRequest(http.MethodDelete,
		"/api/transaction/"+fp+"/note", nil)
	req.Header.Set("X-Fin-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("DELETE /api/transaction/%s/note: want 200, got %d; body=%s",
			fp, w.Code, w.Body.String())
	}
}

func TestAPIAddTag(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)
	fp := "test-fingerprint-add-tag"

	body := `{"tag":"travel"}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/transaction/"+fp+"/tag",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fin-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("POST /api/transaction/%s/tag: want 200, got %d; body=%s",
			fp, w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode tag add response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("tag add: want status=ok, got %q", resp["status"])
	}
}

func TestAPIDeleteTag(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)
	fp := "test-fingerprint-delete-tag"
	tag := "travel"

	// Delete on a non-existent tag must still succeed (idempotent).
	req := httptest.NewRequest(http.MethodDelete,
		"/api/transaction/"+fp+"/tag/"+tag, nil)
	req.Header.Set("X-Fin-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("DELETE /api/transaction/%s/tag/%s: want 200, got %d; body=%s",
			fp, tag, w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Tags
// ---------------------------------------------------------------------------

func TestAPIGetTags_Empty(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/tags", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/tags (empty): want 200, got %d", w.Code)
	}

	// Body must be a JSON array (possibly empty or null).
	var tags []string
	if err := json.NewDecoder(w.Body).Decode(&tags); err != nil {
		t.Errorf("GET /api/tags (empty): cannot decode array: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("GET /api/tags (empty): want empty list, got %v", tags)
	}
}

// ---------------------------------------------------------------------------
// Budget targets
// ---------------------------------------------------------------------------

func TestAPISaveBudgetTarget(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)

	body := `{"category_id":"dining","monthly_target_cents":50000}`
	req := httptest.NewRequest(http.MethodPost, "/api/budget/target",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fin-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("POST /api/budget/target: want 200, got %d; body=%s",
			w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode budget target save response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("budget target save: want status=ok, got %q", resp["status"])
	}
}

func TestAPIDeleteBudgetTarget(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)

	// Delete on non-existent category must still succeed (idempotent).
	req := httptest.NewRequest(http.MethodDelete, "/api/budget/target/dining", nil)
	req.Header.Set("X-Fin-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("DELETE /api/budget/target/dining: want 200, got %d; body=%s",
			w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// SimpleFIN token
// ---------------------------------------------------------------------------

func TestAPISimpleFinToken_EmptyURL(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)

	body := `{"access_url":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/simplefin-token",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fin-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("POST /api/simplefin-token (empty URL): want 400, got %d; body=%s",
			w.Code, w.Body.String())
	}
}

func TestAPISimpleFinToken_NonHTTPS(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)

	body := `{"access_url":"http://app.simplefin.org/simplefin/token"}`
	req := httptest.NewRequest(http.MethodPost, "/api/simplefin-token",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fin-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("POST /api/simplefin-token (http://): want 400, got %d; body=%s",
			w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode simplefin-token response: %v", err)
	}
	if resp["error"] == "" {
		t.Errorf("POST /api/simplefin-token (http://): want error message, got empty")
	}
}

// ---------------------------------------------------------------------------
// Sync history
// ---------------------------------------------------------------------------

func TestAPISyncHistory_Empty(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/sync-history", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/sync-history (empty): want 200, got %d", w.Code)
	}

	// Body must be a JSON array (possibly empty or null).
	body := strings.TrimSpace(w.Body.String())
	// The handler may return null or [] for an empty table -- both are valid JSON.
	if body != "null" && !strings.HasPrefix(body, "[") {
		t.Errorf("GET /api/sync-history (empty): want JSON array or null, got %q", body)
	}
}

// ---------------------------------------------------------------------------
// Category override — auto (delete override)
// ---------------------------------------------------------------------------

func TestAPICategoryOverride_Auto(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)

	// category_id = "auto" deletes any existing override; must return 200
	// even if no override existed.
	body := `{"merchant":"starbucks","category_id":"auto"}`
	req := httptest.NewRequest(http.MethodPost, "/api/category-override",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fin-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("POST /api/category-override (auto): want 200, got %d; body=%s",
			w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode category-override auto response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("category-override auto: want status=ok, got %q", resp["status"])
	}
}
