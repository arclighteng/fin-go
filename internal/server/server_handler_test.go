package server_test

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"mime/multipart"
	"net/http"
	"regexp"
	"strconv"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arclighteng/fin-go/internal/config"
	"github.com/arclighteng/fin-go/internal/db"
	"github.com/arclighteng/fin-go/internal/models"
	"github.com/arclighteng/fin-go/internal/server"
	"github.com/arclighteng/fin-go/ui"
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

// TestAPISyncStatus_NoSyncs verifies the sync-status response matches the
// frontend contract (has_synced, last_sync, data_range).
// Updated: the original test checked syncs_today/can_sync which didn't match
// the frontend JS in base.html (loadSyncStatus). Fixed to match actual contract.
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

	// has_synced must be false for an empty DB.
	hasSynced, ok := body["has_synced"]
	if !ok {
		t.Errorf("GET /api/sync-status: missing 'has_synced' field")
	} else if hasSynced.(bool) != false {
		t.Errorf("GET /api/sync-status: want has_synced=false, got %v", hasSynced)
	}

	// last_sync should not be present when has_synced is false.
	if _, ok := body["last_sync"]; ok {
		t.Errorf("GET /api/sync-status: last_sync should not be present when has_synced is false")
	}

	// data_range should not be present for an empty DB.
	if _, ok := body["data_range"]; ok {
		t.Errorf("GET /api/sync-status: data_range should not be present for empty DB")
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

// ---------------------------------------------------------------------------
// CSV Import — Preview
// ---------------------------------------------------------------------------

func TestAPICSVPreview_ValidCSV(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)

	csvData := "Date,Amount,Description\n2025-01-15,-50.00,Coffee Shop\n2025-01-16,-120.50,Grocery Store\n"
	body, contentType := buildMultipartCSV(t, csvData)

	req := httptest.NewRequest(http.MethodPost, "/api/import/csv/preview", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Fin-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/import/csv/preview: want 200, got %d; body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode preview response: %v", err)
	}

	// Must contain row_count matching the 2 data rows.
	if rc, ok := resp["row_count"].(float64); !ok || int(rc) != 2 {
		t.Errorf("row_count: want 2, got %v", resp["row_count"])
	}

	// Must contain headers array.
	headers, ok := resp["headers"].([]any)
	if !ok || len(headers) != 3 {
		t.Errorf("headers: want 3 entries, got %v", resp["headers"])
	}

	// Must contain preview array with at most 5 entries.
	preview, ok := resp["preview"].([]any)
	if !ok || len(preview) == 0 {
		t.Errorf("preview: want non-empty array, got %v", resp["preview"])
	}
}

func TestAPICSVPreview_NoFile(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/import/csv/preview", nil)
	req.Header.Set("X-Fin-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("POST /api/import/csv/preview (no file): want 400, got %d; body=%s", w.Code, w.Body.String())
	}
}

func TestAPICSVPreview_EmptyCSV(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)

	body, contentType := buildMultipartCSV(t, "")
	req := httptest.NewRequest(http.MethodPost, "/api/import/csv/preview", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Fin-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("POST /api/import/csv/preview (empty): want 400, got %d; body=%s", w.Code, w.Body.String())
	}
}

func TestAPICSVPreview_DetectsBank(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)

	// Chase-style CSV with "Transaction Date" column.
	csvData := "Transaction Date,Amount,Description\n01/15/2025,-50.00,Coffee Shop\n"
	body, contentType := buildMultipartCSV(t, csvData)

	req := httptest.NewRequest(http.MethodPost, "/api/import/csv/preview", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Fin-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/import/csv/preview (Chase): want 200, got %d; body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode preview response: %v", err)
	}
	bank, _ := resp["detected_bank"].(string)
	if bank == "" {
		t.Errorf("detected_bank: want non-empty (Chase), got empty")
	}
}

// ---------------------------------------------------------------------------
// CSV Import — Confirm
// ---------------------------------------------------------------------------

func TestAPICSVConfirm_ValidCSV(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)

	csvData := "Date,Amount,Description\n2025-01-15,-50.00,Coffee Shop\n2025-01-16,-120.50,Grocery Store\n"
	body, contentType := buildMultipartCSV(t, csvData)

	req := httptest.NewRequest(http.MethodPost, "/api/import/csv/confirm", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Fin-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/import/csv/confirm: want 200, got %d; body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode confirm response: %v", err)
	}

	imported, ok := resp["imported"].(float64)
	if !ok || int(imported) != 2 {
		t.Errorf("imported: want 2, got %v", resp["imported"])
	}
}

func TestAPICSVConfirm_DuplicatesSkipped(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)

	csvData := "Date,Amount,Description\n2025-01-15,-50.00,Coffee Shop\n"
	// Import once.
	body1, ct1 := buildMultipartCSV(t, csvData)
	req1 := httptest.NewRequest(http.MethodPost, "/api/import/csv/confirm", body1)
	req1.Header.Set("Content-Type", ct1)
	req1.Header.Set("X-Fin-Request", "1")
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first import: want 200, got %d; body=%s", w1.Code, w1.Body.String())
	}

	// Import same data again — duplicates should be skipped.
	body2, ct2 := buildMultipartCSV(t, csvData)
	req2 := httptest.NewRequest(http.MethodPost, "/api/import/csv/confirm", body2)
	req2.Header.Set("Content-Type", ct2)
	req2.Header.Set("X-Fin-Request", "1")
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("second import: want 200, got %d; body=%s", w2.Code, w2.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w2.Body).Decode(&resp); err != nil {
		t.Fatalf("decode second confirm response: %v", err)
	}
	skipped, _ := resp["skipped_duplicates"].(float64)
	if int(skipped) != 1 {
		t.Errorf("skipped_duplicates: want 1, got %v", resp["skipped_duplicates"])
	}
}

func TestAPICSVConfirm_NoFile(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/import/csv/confirm", nil)
	req.Header.Set("X-Fin-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("POST /api/import/csv/confirm (no file): want 400, got %d; body=%s", w.Code, w.Body.String())
	}
}

func TestAPICSVConfirm_MissingCSRF(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)

	csvData := "Date,Amount,Description\n2025-01-15,-50.00,Coffee Shop\n"
	body, contentType := buildMultipartCSV(t, csvData)

	req := httptest.NewRequest(http.MethodPost, "/api/import/csv/confirm", body)
	req.Header.Set("Content-Type", contentType)
	// Intentionally omit X-Fin-Request header.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("POST /api/import/csv/confirm (no CSRF): want 403, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// CSV Import — Route existence (regression guard)
// ---------------------------------------------------------------------------

func TestAPICSVPreview_RouteExists(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)

	// A GET to the preview endpoint should return 405 (Method Not Allowed),
	// NOT 404. This proves the route is registered.
	req := httptest.NewRequest(http.MethodGet, "/api/import/csv/preview", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Errorf("GET /api/import/csv/preview: got 404 — route is not registered")
	}
}

func TestAPICSVConfirm_RouteExists(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/import/csv/confirm", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Errorf("GET /api/import/csv/confirm: got 404 — route is not registered")
	}
}

// ---------------------------------------------------------------------------
// buildMultipartCSV helper
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// SRI integrity attribute — regression guard
// ---------------------------------------------------------------------------

func TestDashboard_CSVImportShowsExpenses(t *testing.T) {
	t.Parallel()

	h, database := newTestServerWithDB(t)

	// Seed the csv-import account and April transactions.
	database.UpsertAccounts([]models.Account{
		{AccountID: "csv-import", Institution: "Manual Import", Name: "csv-import", Type: "checking", Currency: "USD"},
	})
	database.UpsertTransactions([]models.Transaction{
		{AccountID: "csv-import", PostedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), AmountCents: -4599, Currency: "USD", Description: "WHOLE FOODS", Merchant: "WHOLE FOODS", Fingerprint: "fp-1"},
		{AccountID: "csv-import", PostedAt: time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC), AmountCents: 250000, Currency: "USD", Description: "EMPLOYER", Merchant: "EMPLOYER", Fingerprint: "fp-2"},
		{AccountID: "csv-import", PostedAt: time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC), AmountCents: -8999, Currency: "USD", Description: "COMCAST", Merchant: "COMCAST", Fingerprint: "fp-3"},
		{AccountID: "csv-import", PostedAt: time.Date(2026, 4, 3, 0, 0, 0, 0, time.UTC), AmountCents: -120000, Currency: "USD", Description: "RENT", Merchant: "RENT", Fingerprint: "fp-4"},
	})

	// Request dashboard for April 2026.
	req := httptest.NewRequest(http.MethodGet, "/dashboard?start_date=2026-04-01&end_date=2026-05-01", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /dashboard: want 200, got %d", w.Code)
	}

	body := w.Body.String()

	// Should contain income amount ($2,500.00).
	if !strings.Contains(body, "$2,500.00") {
		t.Error("dashboard missing income $2,500.00")
	}

	// Should contain expense amounts — at minimum the total or individual items.
	// Total expenses = $45.99 + $89.99 + $1,200.00 = $1,335.98
	if !strings.Contains(body, "$1,335.98") && !strings.Contains(body, "$1,200.00") && !strings.Contains(body, "Expenses") {
		t.Error("dashboard missing expense data")
	}

	// The "Kept" line should show net = $2500 - $1335.98 = $1,164.02
	if !strings.Contains(body, "Kept") {
		t.Error("dashboard missing 'Kept' savings line")
	}

	// Should NOT be blank/empty dashboard.
	if strings.Contains(body, "No data") && !strings.Contains(body, "Cash Flow") {
		t.Error("dashboard showing empty state despite having transactions")
	}

	t.Logf("Dashboard body length: %d bytes", len(body))
}

func TestBaseTemplate_ChartJSVendored(t *testing.T) {
	t.Parallel()

	// Verify Chart.js is loaded from a local vendored path, not a CDN.
	h := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/connect", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /connect: want 200, got %d", w.Code)
	}

	body := w.Body.String()

	if !strings.Contains(body, `/static/js/chart.umd.min.js`) {
		t.Error("base template: Chart.js should be loaded from vendored /static/js/ path")
	}

	// Ensure no CDN reference remains.
	if strings.Contains(body, `cdn.jsdelivr.net`) {
		t.Error("base template: still references CDN — should use vendored Chart.js")
	}
}

// ---------------------------------------------------------------------------
// buildMultipartCSV helper
// ---------------------------------------------------------------------------

func buildMultipartCSV(t *testing.T, csvContent string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "test.csv")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte(csvContent)); err != nil {
		t.Fatalf("write csv content: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return &buf, writer.FormDataContentType()
}

// ---------------------------------------------------------------------------
// Commitments API
// ---------------------------------------------------------------------------

// postJSON is a test helper that sends a JSON POST with the CSRF header.
func postJSON(t *testing.T, h http.Handler, url string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fin-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// patchJSON is a test helper that sends a JSON PATCH with the CSRF header.
func patchJSON(t *testing.T, h http.Handler, url string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPatch, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fin-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// deleteReq is a test helper that sends a DELETE with the CSRF header.
func deleteReq(t *testing.T, h http.Handler, url string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, url, nil)
	req.Header.Set("X-Fin-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestAPICreateCommitment(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)

	w := postJSON(t, h, "/api/commitments", map[string]any{
		"name":      "Netflix",
		"direction": "expense",
		"cadence":   "monthly",
		"source":    "manual",
		"confirmed": 1,
		"expected_cents": 1599,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/commitments: want 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("want status=ok, got %v", body["status"])
	}
	if body["id"] == nil || body["id"].(float64) < 1 {
		t.Errorf("want id >= 1, got %v", body["id"])
	}
}

func TestAPICreateCommitment_MissingName(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)

	w := postJSON(t, h, "/api/commitments", map[string]any{
		"direction": "expense",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("POST /api/commitments (no name): want 400, got %d", w.Code)
	}
}

func TestAPIUpdateCommitment(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)

	// Create first.
	w := postJSON(t, h, "/api/commitments", map[string]any{
		"name": "Spotify", "direction": "expense", "cadence": "monthly",
	})
	var created map[string]any
	json.NewDecoder(w.Body).Decode(&created)
	id := int(created["id"].(float64))

	// Confirm it.
	w2 := patchJSON(t, h, "/api/commitments/"+strconv.Itoa(id), map[string]any{
		"confirmed": 1,
	})
	if w2.Code != http.StatusOK {
		t.Fatalf("PATCH /api/commitments/%d: want 200, got %d; body: %s", id, w2.Code, w2.Body.String())
	}
}

func TestAPIDeleteCommitment(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)

	// Create.
	w := postJSON(t, h, "/api/commitments", map[string]any{
		"name": "Gym", "direction": "expense",
	})
	var created map[string]any
	json.NewDecoder(w.Body).Decode(&created)
	id := int(created["id"].(float64))

	// Delete.
	w2 := deleteReq(t, h, "/api/commitments/"+strconv.Itoa(id))
	if w2.Code != http.StatusOK {
		t.Fatalf("DELETE /api/commitments/%d: want 200, got %d", id, w2.Code)
	}
}

func TestAPIDismissDuplicate(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)

	// Create two commitments with same merchant.
	postJSON(t, h, "/api/commitments", map[string]any{"name": "Amazon", "direction": "expense"})
	postJSON(t, h, "/api/commitments", map[string]any{"name": "Amazon", "direction": "expense"})

	// Dismiss the duplicate group.
	w := postJSON(t, h, "/api/dismiss-duplicate", map[string]any{
		"merchant": "Amazon", "dismiss": true,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/dismiss-duplicate: want 200, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestAPICommitments_CSRF(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)

	// POST without X-Fin-Request header should be rejected.
	body, _ := json.Marshal(map[string]any{"name": "Test"})
	req := httptest.NewRequest(http.MethodPost, "/api/commitments", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("POST /api/commitments without CSRF: want 403, got %d", w.Code)
	}
}

func TestAPICommitmentRoutes_NotFound(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)

	// Verify routes exist (POST should not 404).
	routes := []struct {
		method string
		url    string
	}{
		{http.MethodPost, "/api/commitments"},
		{http.MethodPatch, "/api/commitments/1"},
		{http.MethodDelete, "/api/commitments/1"},
		{http.MethodPost, "/api/dismiss-duplicate"},
	}
	for _, rt := range routes {
		body, _ := json.Marshal(map[string]any{})
		req := httptest.NewRequest(rt.method, rt.url, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Fin-Request", "1")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("%s %s: got 404 — route not registered", rt.method, rt.url)
		}
	}
}

// ---------------------------------------------------------------------------
// Route coverage: every fetch('/api/...') in templates must have a route
// ---------------------------------------------------------------------------

// TestAllTemplateFetchRoutesExist scans embedded HTML templates and JS assets
// for fetch() calls to /api/ endpoints and verifies each one is registered
// (not 404).
func TestAllTemplateFetchRoutesExist(t *testing.T) {
	t.Parallel()

	h := newTestServer(t)

	templateFS, err := fs.Sub(ui.Templates, "templates")
	if err != nil {
		t.Fatalf("fs.Sub templates: %v", err)
	}
	staticFS, err := fs.Sub(ui.Static, "static")
	if err != nil {
		t.Fatalf("fs.Sub static: %v", err)
	}

	// Match patterns like: fetch('/api/something' or fetch('/api/something/' + id
	// Also match finApi.postJSON('/api/something'
	fetchRe := regexp.MustCompile(`(?:fetch|finApi\.postJSON)\s*\(\s*['"](/api/[^'"?]+)`)
	methodRe := regexp.MustCompile(`method:\s*['"](\w+)['"]`)

	type route struct {
		method string
		url    string
		file   string
	}
	var routes []route

	scan := func(label string, fsys fs.FS, suffix string) error {
		return fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, suffix) {
				return err
			}
			// Skip vendored libraries we don't author (Chart.js).
			if strings.Contains(path, "chart.umd") {
				return nil
			}
			data, err := fs.ReadFile(fsys, path)
			if err != nil {
				return err
			}
			content := string(data)
			lines := strings.Split(content, "\n")

			for i, line := range lines {
				matches := fetchRe.FindAllStringSubmatch(line, -1)
				for _, m := range matches {
					url := m[1]
					url = strings.TrimRight(url, "/")
					if strings.Contains(url, "/' +") || strings.HasSuffix(url, "/' +") {
						continue
					}

					method := "GET"
					if strings.Contains(line, "finApi.postJSON") {
						method = "POST"
					}
					contextStart := i
					if contextStart > 0 {
						contextStart = i - 1
					}
					contextEnd := i + 5
					if contextEnd > len(lines) {
						contextEnd = len(lines)
					}
					for _, cl := range lines[contextStart:contextEnd] {
						if mm := methodRe.FindStringSubmatch(cl); mm != nil {
							method = strings.ToUpper(mm[1])
						}
					}

					if strings.Contains(url, "/' +") {
						url = strings.Split(url, "/' +")[0] + "/1"
					}

					routes = append(routes, route{method: method, url: url, file: label + ":" + path})
				}
			}
			return nil
		})
	}

	if err := scan("template", templateFS, ".html"); err != nil {
		t.Fatalf("walk templates: %v", err)
	}
	if err := scan("static", staticFS, ".js"); err != nil {
		t.Fatalf("walk static: %v", err)
	}

	if len(routes) == 0 {
		t.Fatal("no fetch('/api/...') calls found in templates or JS — test is broken")
	}

	// Deduplicate.
	seen := map[string]bool{}
	for _, r := range routes {
		key := r.method + " " + r.url
		if seen[key] {
			continue
		}
		seen[key] = true

		t.Run(r.method+" "+r.url, func(t *testing.T) {
			t.Parallel()
			var body *bytes.Reader
			if r.method == "POST" || r.method == "PATCH" || r.method == "DELETE" {
				body = bytes.NewReader([]byte("{}"))
			} else {
				body = bytes.NewReader(nil)
			}
			req := httptest.NewRequest(r.method, r.url, body)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Fin-Request", "1")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code == http.StatusNotFound {
				t.Errorf("%s %s (from %s): got 404 — route not registered", r.method, r.url, r.file)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// View smoke tests
// ---------------------------------------------------------------------------

func TestViewBudget_Smoke(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/budget", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("GET /budget: want 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("GET /budget: want text/html, got %q", ct)
	}
	if w.Body.Len() == 0 {
		t.Error("GET /budget: empty body")
	}
}

func TestViewInsights_Smoke(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/insights", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("GET /insights: want 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("GET /insights: want text/html, got %q", ct)
	}
	if w.Body.Len() == 0 {
		t.Error("GET /insights: empty body")
	}
}

func TestViewReview_Smoke(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/review", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("GET /review: want 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("GET /review: want text/html, got %q", ct)
	}
	if w.Body.Len() == 0 {
		t.Error("GET /review: empty body")
	}
}

func TestViewSyncLog_Smoke(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/sync-log", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("GET /sync-log: want 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("GET /sync-log: want text/html, got %q", ct)
	}
	if w.Body.Len() == 0 {
		t.Error("GET /sync-log: empty body")
	}
}

// ---------------------------------------------------------------------------
// Smart redirect test
// ---------------------------------------------------------------------------

func TestDashboard_SmartRedirect(t *testing.T) {
	t.Parallel()
	h, database := newTestServerWithDB(t)

	// Seed a transaction 6 months ago — dashboard default (this_month) should
	// redirect to that month since current month has no data.
	pastDate := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	database.UpsertAccounts([]models.Account{
		{AccountID: "acct-redir", Institution: "Bank", Name: "Checking", Type: "checking", Currency: "USD"},
	})
	database.UpsertTransactions([]models.Transaction{
		{AccountID: "acct-redir", PostedAt: pastDate, AmountCents: -5000, Currency: "USD",
			Description: "Coffee", Merchant: "Coffee", Fingerprint: "fp-redir-1"},
	})

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("GET /dashboard (smart redirect): want 302, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "start_date=2025-06-01") {
		t.Errorf("redirect location missing start_date=2025-06-01: %s", loc)
	}
}

func TestDashboard_NoRedirectWhenCurrentMonthHasData(t *testing.T) {
	t.Parallel()
	h, database := newTestServerWithDB(t)

	// Seed a transaction in the current month.
	now := time.Now().UTC()
	database.UpsertAccounts([]models.Account{
		{AccountID: "acct-now", Institution: "Bank", Name: "Checking", Type: "checking", Currency: "USD"},
	})
	database.UpsertTransactions([]models.Transaction{
		{AccountID: "acct-now", PostedAt: now, AmountCents: -5000, Currency: "USD",
			Description: "Coffee", Merchant: "Coffee", Fingerprint: "fp-now-1"},
	})

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /dashboard (current month data): want 200, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Search happy-path test
// ---------------------------------------------------------------------------

func TestAPISearch_HappyPath(t *testing.T) {
	t.Parallel()
	h, database := newTestServerWithDB(t)

	database.UpsertAccounts([]models.Account{
		{AccountID: "acct-search", Institution: "First Bank", Name: "My Checking", Type: "checking", Currency: "USD"},
	})
	database.UpsertTransactions([]models.Transaction{
		{AccountID: "acct-search", PostedAt: time.Date(2025, 3, 10, 0, 0, 0, 0, time.UTC),
			AmountCents: -4599, Currency: "USD", Description: "WHOLE FOODS MARKET",
			Merchant: "WHOLE FOODS", Fingerprint: "fp-search-1"},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/search?q=WHOLE", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/search?q=WHOLE: want 200, got %d; body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode search response: %v", err)
	}

	matches, ok := resp["matches"].([]any)
	if !ok || len(matches) == 0 {
		t.Fatal("search: want non-empty matches array")
	}

	first := matches[0].(map[string]any)
	for _, key := range []string{"date", "amount_cents", "merchant", "description", "account_name", "fingerprint"} {
		if _, exists := first[key]; !exists {
			t.Errorf("search result missing key %q", key)
		}
	}
}

func TestAPISearch_AccountNameResolved(t *testing.T) {
	t.Parallel()
	h, database := newTestServerWithDB(t)

	database.UpsertAccounts([]models.Account{
		{AccountID: "acct-name-test", Institution: "Big Bank", Name: "Premium Checking", Type: "checking", Currency: "USD"},
	})
	database.UpsertTransactions([]models.Transaction{
		{AccountID: "acct-name-test", PostedAt: time.Date(2025, 3, 10, 0, 0, 0, 0, time.UTC),
			AmountCents: -1000, Currency: "USD", Description: "Test Store",
			Merchant: "Test Store", Fingerprint: "fp-name-test-1"},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/search?q=Test+Store", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("search: want 200, got %d", w.Code)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	matches := resp["matches"].([]any)
	first := matches[0].(map[string]any)

	accountName, _ := first["account_name"].(string)
	if accountName != "Premium Checking" {
		t.Errorf("account_name: want %q, got %q", "Premium Checking", accountName)
	}
}

// ---------------------------------------------------------------------------
// JSON body size limit test
// ---------------------------------------------------------------------------

func TestJSONBodySizeLimit(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)

	// Send a body larger than 1 MB to a JSON endpoint.
	bigBody := strings.Repeat("x", 2<<20) // 2 MB
	req := httptest.NewRequest(http.MethodPost, "/api/income-source",
		strings.NewReader(bigBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fin-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	// http.MaxBytesReader causes json.Decode to fail, yielding 400.
	if w.Code == http.StatusOK {
		t.Errorf("POST /api/income-source (oversized body): want non-200, got 200")
	}
}

// ---------------------------------------------------------------------------
// Search filter tests
// ---------------------------------------------------------------------------

func TestAPISearch_DaysFilter(t *testing.T) {
	t.Parallel()
	h, database := newTestServerWithDB(t)

	database.UpsertAccounts([]models.Account{
		{AccountID: "acct-days", Institution: "Bank", Name: "Checking", Type: "checking", Currency: "USD"},
	})
	// Recent transaction (today).
	database.UpsertTransactions([]models.Transaction{
		{AccountID: "acct-days", PostedAt: time.Now().UTC(), AmountCents: -1000, Currency: "USD",
			Description: "Recent Coffee", Merchant: "Coffee Shop", Fingerprint: "fp-days-1"},
	})
	// Old transaction (200 days ago).
	database.UpsertTransactions([]models.Transaction{
		{AccountID: "acct-days", PostedAt: time.Now().UTC().AddDate(0, 0, -200), AmountCents: -2000, Currency: "USD",
			Description: "Old Coffee", Merchant: "Coffee Shop", Fingerprint: "fp-days-2"},
	})

	// days=30 should return only the recent one.
	req := httptest.NewRequest(http.MethodGet, "/api/search?q=Coffee&days=30", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("search with days=30: want 200, got %d; body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	matches := resp["matches"].([]any)
	if len(matches) != 1 {
		t.Errorf("days=30: want 1 match, got %d", len(matches))
	}

	// days=365 should return both.
	req2 := httptest.NewRequest(http.MethodGet, "/api/search?q=Coffee&days=365", nil)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)

	var resp2 map[string]any
	json.NewDecoder(w2.Body).Decode(&resp2)
	matches2 := resp2["matches"].([]any)
	if len(matches2) != 2 {
		t.Errorf("days=365: want 2 matches, got %d", len(matches2))
	}
}

func TestAPISearch_AccountsFilter(t *testing.T) {
	t.Parallel()
	h, database := newTestServerWithDB(t)

	database.UpsertAccounts([]models.Account{
		{AccountID: "acct-a", Institution: "Bank A", Name: "Checking A", Type: "checking", Currency: "USD"},
		{AccountID: "acct-b", Institution: "Bank B", Name: "Checking B", Type: "checking", Currency: "USD"},
	})
	database.UpsertTransactions([]models.Transaction{
		{AccountID: "acct-a", PostedAt: time.Now().UTC(), AmountCents: -1000, Currency: "USD",
			Description: "Grocery", Merchant: "Grocery Store", Fingerprint: "fp-acct-a"},
		{AccountID: "acct-b", PostedAt: time.Now().UTC(), AmountCents: -2000, Currency: "USD",
			Description: "Grocery", Merchant: "Grocery Store", Fingerprint: "fp-acct-b"},
	})

	// Filter to acct-a only.
	req := httptest.NewRequest(http.MethodGet, "/api/search?q=Grocery&accounts=acct-a", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("search accounts=acct-a: want 200, got %d", w.Code)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	matches := resp["matches"].([]any)
	if len(matches) != 1 {
		t.Errorf("accounts=acct-a: want 1 match, got %d", len(matches))
	}

	// Filter to both accounts.
	req2 := httptest.NewRequest(http.MethodGet, "/api/search?q=Grocery&accounts=acct-a&accounts=acct-b", nil)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)

	var resp2 map[string]any
	json.NewDecoder(w2.Body).Decode(&resp2)
	matches2 := resp2["matches"].([]any)
	if len(matches2) != 2 {
		t.Errorf("accounts=acct-a&accounts=acct-b: want 2 matches, got %d", len(matches2))
	}

	// Unknown account returns empty, not error.
	req3 := httptest.NewRequest(http.MethodGet, "/api/search?q=Grocery&accounts=unknown", nil)
	w3 := httptest.NewRecorder()
	h.ServeHTTP(w3, req3)

	if w3.Code != http.StatusOK {
		t.Errorf("accounts=unknown: want 200, got %d", w3.Code)
	}
}

func TestAPISearch_DaysCapped(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)

	// days=9999 should be silently capped to 3650, not error.
	req := httptest.NewRequest(http.MethodGet, "/api/search?q=test&days=9999", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("search days=9999: want 200, got %d", w.Code)
	}
}

func TestAPISearch_DaysInvalid(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)

	// Non-integer days should be ignored (no filter applied), not error.
	req := httptest.NewRequest(http.MethodGet, "/api/search?q=test&days=abc", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("search days=abc: want 200, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Drilldown API tests
// ---------------------------------------------------------------------------

func TestAPIDrilldown_MissingScope(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/drilldown?start_date=2026-01-01&end_date=2026-02-01", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("drilldown missing scope: want 400, got %d", w.Code)
	}
}

func TestAPIDrilldown_MissingDates(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/drilldown?scope=income", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("drilldown missing dates: want 400, got %d", w.Code)
	}
}

func TestAPIDrilldown_Income(t *testing.T) {
	t.Parallel()
	h, database := newTestServerWithDB(t)

	database.UpsertAccounts([]models.Account{
		{AccountID: "acct-dd", Institution: "Bank", Name: "Checking", Type: "checking", Currency: "USD"},
	})
	database.UpsertTransactions([]models.Transaction{
		{AccountID: "acct-dd", PostedAt: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			AmountCents: 250000, Currency: "USD", Description: "EMPLOYER", Merchant: "EMPLOYER", Fingerprint: "fp-dd-inc"},
		{AccountID: "acct-dd", PostedAt: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			AmountCents: -5000, Currency: "USD", Description: "Coffee", Merchant: "Coffee", Fingerprint: "fp-dd-exp"},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/drilldown?scope=income&start_date=2026-01-01&end_date=2026-02-01", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("drilldown income: want 200, got %d; body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["title"] != "Income" {
		t.Errorf("title: want Income, got %v", resp["title"])
	}
	txns := resp["transactions"].([]any)
	if len(txns) != 1 {
		t.Errorf("income transactions: want 1, got %d", len(txns))
	}
}

func TestAPIDrilldown_Spend(t *testing.T) {
	t.Parallel()
	h, database := newTestServerWithDB(t)

	database.UpsertAccounts([]models.Account{
		{AccountID: "acct-dd2", Institution: "Bank", Name: "Checking", Type: "checking", Currency: "USD"},
	})
	database.UpsertTransactions([]models.Transaction{
		{AccountID: "acct-dd2", PostedAt: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			AmountCents: 250000, Currency: "USD", Description: "EMPLOYER", Merchant: "EMPLOYER", Fingerprint: "fp-dd2-inc"},
		{AccountID: "acct-dd2", PostedAt: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			AmountCents: -5000, Currency: "USD", Description: "Coffee", Merchant: "Coffee", Fingerprint: "fp-dd2-exp"},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/drilldown?scope=spend&start_date=2026-01-01&end_date=2026-02-01", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("drilldown spend: want 200, got %d", w.Code)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	txns := resp["transactions"].([]any)
	if len(txns) != 1 {
		t.Errorf("spend transactions: want 1, got %d", len(txns))
	}
}

func TestAPIDrilldown_Merchant(t *testing.T) {
	t.Parallel()
	h, database := newTestServerWithDB(t)

	database.UpsertAccounts([]models.Account{
		{AccountID: "acct-dd3", Institution: "Bank", Name: "Checking", Type: "checking", Currency: "USD"},
	})
	database.UpsertTransactions([]models.Transaction{
		{AccountID: "acct-dd3", PostedAt: time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC),
			AmountCents: -3000, Currency: "USD", Description: "STARBUCKS #123", Merchant: "Starbucks", Fingerprint: "fp-dd3-1"},
		{AccountID: "acct-dd3", PostedAt: time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC),
			AmountCents: -3500, Currency: "USD", Description: "STARBUCKS #456", Merchant: "Starbucks", Fingerprint: "fp-dd3-2"},
		{AccountID: "acct-dd3", PostedAt: time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC),
			AmountCents: -8000, Currency: "USD", Description: "WHOLE FOODS", Merchant: "Whole Foods", Fingerprint: "fp-dd3-3"},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/drilldown?scope=merchant:starbucks&start_date=2026-01-01&end_date=2026-02-01", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("drilldown merchant: want 200, got %d; body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	txns := resp["transactions"].([]any)
	if len(txns) != 2 {
		t.Errorf("merchant:starbucks transactions: want 2, got %d", len(txns))
	}
}

func TestAPIDrilldown_UnknownScope(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/drilldown?scope=bogus&start_date=2026-01-01&end_date=2026-02-01", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("drilldown unknown scope: want 400, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// NotYetPosted detection
// ---------------------------------------------------------------------------

func TestCommitments_NotYetPosted_ShowsMissing(t *testing.T) {
	t.Parallel()
	h, database := newTestServerWithDB(t)

	// Create a confirmed monthly commitment for "netflix" expected on the 1st.
	dom := 1
	cents := int64(1599)
	database.SaveCommitment(models.Commitment{
		Name:          "Netflix",
		MerchantNorm:  "netflix",
		ExpectedCents: &cents,
		Cadence:       "monthly",
		DayOfMonth:    &dom,
		Confirmed:     true,
		Source:        "manual",
		Direction:     "expense",
	})

	// Seed an account but NO matching transaction for netflix this month.
	database.UpsertAccounts([]models.Account{
		{AccountID: "acct1", Institution: "Bank", Name: "Checking", Type: "checking", Currency: "USD"},
	})

	req := httptest.NewRequest(http.MethodGet, "/commitments", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /commitments: want 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Not Yet Posted") {
		t.Error("expected 'Not Yet Posted' section when commitment has no matching transaction")
	}
	if !strings.Contains(body, "Netflix") {
		t.Error("expected Netflix to appear in Not Yet Posted section")
	}
}

func TestCommitments_NotYetPosted_HiddenWhenMatched(t *testing.T) {
	t.Parallel()
	h, database := newTestServerWithDB(t)

	now := time.Now().UTC()
	dom := now.Day()
	cents := int64(1599)
	database.SaveCommitment(models.Commitment{
		Name:          "Netflix",
		MerchantNorm:  "netflix",
		ExpectedCents: &cents,
		Cadence:       "monthly",
		DayOfMonth:    &dom,
		Confirmed:     true,
		Source:        "manual",
		Direction:     "expense",
	})

	// Seed a matching transaction for netflix posted today.
	database.UpsertAccounts([]models.Account{
		{AccountID: "acct1", Institution: "Bank", Name: "Checking", Type: "checking", Currency: "USD"},
	})
	database.UpsertTransactions([]models.Transaction{
		{AccountID: "acct1", PostedAt: now, AmountCents: -1599, Currency: "USD",
			Description: "NETFLIX.COM", Merchant: "Netflix", Fingerprint: "fp-netflix"},
	})

	req := httptest.NewRequest(http.MethodGet, "/commitments", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /commitments: want 200, got %d", w.Code)
	}

	body := w.Body.String()
	if strings.Contains(body, "Not Yet Posted") {
		t.Error("should NOT show 'Not Yet Posted' when matching transaction exists")
	}
}
