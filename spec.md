# Spec: Go Code Review Remediation

## Overview

Fix all findings from the five-agent code review of the Go rewrite. The review
identified 5 critical, 13 high, 20 medium, and assorted low-severity issues
across error handling, architecture, security, testing, and code quality.

**Problem solved:** The Go port was done fast. It works, but it ships silent data
corruption paths (swallowed errors that return zero values instead of failures),
a resource leak in the public embedding API, zero HTTP handler tests, and missing
security headers. These must be fixed before the codebase can be treated as
release-quality.

**Users affected:** All Fin users (via the web dashboard) and Kept embedders
(via `pkg/fincore`).

**Done looks like:** All critical and high findings resolved. Tests prove the
fixes. No regressions in existing tests or E2E suite.

**Hard constraints:**
- All money remains integer cents. No floats.
- All SQL remains parameterized. No string-interpolated user input.
- No new dependencies unless strictly necessary.
- No auth code in this repo (belongs in Kept).
- Existing E2E tests must continue to pass.

**Out of scope this iteration:**
- Adding `context.Context` to all DB methods (H10) -- large refactor, do separately
- Moving view SQL into `internal/db` (M1) -- architectural refactor, do separately
- Embedding templates/static with `go:embed` (M15) -- do after fixes land
- `NormalizeMerchant` vs `MerchantNormExpr` alignment (M13) -- needs design decision
- Adding Firefox/WebKit to E2E (M17) -- nice to have, not a fix
- Cache `Get()` write lock to atomic (M11) -- optimization, not a bug

---

## Commands

```bash
# Full test suite
cd C:\Users\AR\Projects\fin\go && C:\Users\AR\go\bin\go.exe test ./...

# Single package
C:\Users\AR\go\bin\go.exe test ./internal/db/...
C:\Users\AR\go\bin\go.exe test ./internal/server/...

# With race detector
C:\Users\AR\go\bin\go.exe test -race ./...

# Benchmarks
C:\Users\AR\go\bin\go.exe test -bench=. -benchmem ./internal/...

# Build
C:\Users\AR\go\bin\go.exe build -o fin.exe ./cmd/fin

# Vet
C:\Users\AR\go\bin\go.exe vet ./...

# E2E (requires built binary)
cd C:\Users\AR\Projects\fin\go\e2e && npx playwright test
```

---

## Files Affected

### Phase 1: Correctness Blockers

| File | Current Responsibility | What Changes |
|------|----------------------|--------------|
| `internal/db/db.go` | DB connection, schema init, core CRUD | Migration error handling: check for "duplicate column" specifically |
| `internal/server/views.go` | Dashboard/connect/sync-log view handlers | Fix `time.Local` to `time.UTC` in `thisMonthRange`, `lastMonthRange`, `nPriorMonths`; fix silent Scan errors in `newBaseData`, `queryPendingCount`, `scanRecentTxns` |
| `internal/server/views_extra.go` | Budget/commitments/insights/review handlers | Fix `endISO` day+1 overflow in `handleReviewView`; fix `time.Local` in `handleBudgetView` |
| `internal/db/queries.go` | Income sources, category overrides, annotations | Fix `SaveIncomeSource` DELETE error not checked |
| `internal/classify/reporting.go` | Canonical report engine | Fix `rows.Scan(...) == nil` pattern to capture and log errors; fix `countPending` nolint suppression |
| `internal/cli/web.go` | CLI web command | Remove sub-module schema calls (moved to `db.Init`) |
| `internal/audit/audit.go` | Audit logging | Remove per-call `EnsureSchema` (called from `db.Init` instead) |
| `internal/closebooks/closebooks.go` | Period closing | Remove per-call `EnsureSchema` (called from `db.Init` instead) |
| `internal/reconciliation/reconciliation.go` | Account reconciliation | Remove per-call `EnsureSchema` (called from `db.Init` instead) |

### Phase 2: API / Architecture

| File | Current Responsibility | What Changes |
|------|----------------------|--------------|
| `pkg/fincore/fincore.go` | Public embedding API | Return `*Server` struct with `ServeHTTP` + `Close` instead of bare `http.Handler` |
| `internal/db/db.go` | DB struct definition | Stop embedding `*sql.DB`; hold as unexported `db *sql.DB` field; add `Underlying() *sql.DB` for sub-module schema init |
| `internal/server/server.go` | HTTP handler setup | Add security headers middleware; add `version` field to `Server`; wire version into `handleAPIVersion`; remove or 501 cache stubs |
| `internal/server/api.go` | API handlers | Remove `handleAPICacheStats`/`handleAPICacheClear` stubs |
| `ui/templates/base.html` | HTML base template | Add SRI integrity hash to Chart.js script tag |
| `internal/cli/root.go` | CLI root command | Wire `appVersion` to `server.AppVersion` |

### Phase 3: Test Gaps

| File | Current Responsibility | What Changes |
|------|----------------------|--------------|
| `internal/server/server_handler_test.go` | (new) | HTTP handler tests for /health, /api/version, /api/accounts, /api/sync-status, /api/sync, error paths |
| `internal/classify/transfers_test.go` | (new) | Table-driven tests for transfer pairing algorithm |
| `internal/classify/refunds_test.go` | (new) | Table-driven tests for refund matching algorithm |
| `e2e/playwright.config.ts` | Playwright config | Replace hardcoded Go path with `go` from PATH |

### Phase 4: Polish

| File | What Changes |
|------|--------------|
| `internal/server/sync.go` | Extract `const MaxSyncsPerDay = 20` |
| `internal/server/server.go` | Use `MaxSyncsPerDay` constant |
| `internal/cli/sync.go` | Use `MaxSyncsPerDay` constant |
| `internal/credentials/credentials.go` | Use `sync.Once` for `keyringAvailable` |
| `internal/money/money.go` | Replace `interface{}` with `any` |
| Various `*_test.go` | Add `t.Parallel()`, fix blank subtest names, remove `tc := tc` captures |
| `internal/projections/projections.go` | Remove dead `_ = dayOfMonth`, `_ = cadence` parameters |

### Must NOT Change

- `internal/classify/types.go` Truth Contract semantics
- `internal/categorize/` category rule definitions
- Any template rendering behavior (HTML output must remain identical)
- SQL query results (data must remain identical)
- `go.mod` direct dependencies (no new deps)

---

## Code Style (from existing codebase)

### Error handling
```go
// Return errors with context using fmt.Errorf + %w
if err := database.Init(); err != nil {
    return fmt.Errorf("initialize database: %w", err)
}

// Check every error. Never use _ for error returns on DB operations.
if _, err := d.Exec("DELETE FROM ...", args...); err != nil {
    return err
}

// For view helpers that cannot return errors, log them:
if err := rows.Scan(&val); err != nil {
    log.Printf("scan error: %v", err)
    continue
}
```

### JSON API responses
```go
// Success
writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})

// Error -- use generic message for 500s, specific for 4xx
writeJSON(w, http.StatusBadRequest, map[string]string{"error": "merchant is required"})
writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
```

### Test patterns
```go
// Table-driven with named subtests
func TestFoo(t *testing.T) {
    t.Parallel()
    tests := []struct {
        name string
        // ...
    }{
        {"descriptive name", /* ... */},
    }
    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            t.Parallel()
            // ...
        })
    }
}

// DB test helper
func newTestDB(t *testing.T) *db.DB {
    t.Helper()
    database, err := db.Connect(":memory:")
    if err != nil { t.Fatal(err) }
    t.Cleanup(func() { database.Close() })
    if err := database.Init(); err != nil { t.Fatal(err) }
    return database
}
```

### Naming
```go
// Receivers: short, consistent
func (d *DB) GetAccounts() ([]models.Account, error)
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request)
func (c *Cache) Get(key string) (any, bool)
```

---

## Three-Tier Boundaries

### Always Do
- Check every `Scan` and `Exec` error return value
- Use `time.UTC` for all date boundary calculations
- Use parameterized SQL for all queries
- Write a test for every behavior change
- Use `t.Parallel()` in new tests
- Use named subtests (never `t.Run("", ...)`)
- Return generic error messages to API clients for 500 errors
- Log detailed errors server-side with `log.Printf`

### Ask First
- Changing the `server.New()` function signature (affects tests and fincore)
- Changing the `fincore.NewServer()` return type (affects Kept)
- Adding any new dependency to `go.mod`
- Removing any API route (even stubs -- check frontend JS first)
- Modifying the `DB` struct's exported methods (affects all callers)

### Never Do
- Modify existing test assertions to make them pass (fix the implementation)
- Add `//nolint` directives to suppress real error paths
- Use `time.Local` for date calculations
- Return raw `err.Error()` to API clients on 500 responses
- Use `interface{}` in new code (use `any`)
- Skip the error return from `database/sql` `Scan`, `Exec`, `Query` calls
- Break the `classify` package Truth Contract
- Add auth code to this repo

---

## Testing Strategy

### Phase 1 Tests (correctness)

**Migration error handling (`internal/db/db_test.go`):**
- `TestInit_DuplicateColumnIgnored` -- run Init twice; second call succeeds
- `TestInit_RealErrorReturned` -- inject a genuinely broken ALTER TABLE; verify error returned (not swallowed)

**UTC date ranges (`internal/server/server_test.go` or new `views_test.go`):**
- `TestThisMonthRange_UsesUTC` -- verify output uses UTC, not local
- `TestLastMonthRange_UsesUTC` -- same
- `TestNPriorMonths_UsesUTC` -- same
- `TestThisMonthRange_EndOfMonth` -- Dec 31 produces Jan 1 next year
- `TestThisMonthRange_Jan1` -- January wraps correctly

**Scan error handling (verified by existing + new tests):**
- `TestNewBaseData_EmptyDB` -- verify returns valid BaseData with zero counts (not an error)
- `TestQueryPendingCount_EmptyDB` -- returns 0

**Schema consolidation (`internal/db/db_test.go`):**
- `TestInit_CreatesAuditTable` -- verify audit_events table exists after Init
- `TestInit_CreatesClosebooksTable` -- verify closed_periods table exists after Init
- `TestInit_CreatesReconciliationTable` -- verify reconciliation_sessions table exists after Init

### Phase 2 Tests (architecture)

**fincore resource management (`pkg/fincore/fincore_test.go`):**
- `TestNewServer_ReturnsCloseable` -- call `NewServer`, verify Close works without panic
- `TestNewServer_ServeHTTP` -- verify the returned value implements `http.Handler`

**Security headers (`internal/server/server_handler_test.go`):**
- `TestSecurityHeaders_XFrameOptions` -- GET /health returns `X-Frame-Options: DENY`
- `TestSecurityHeaders_ContentTypeOptions` -- GET /health returns `X-Content-Type-Options: nosniff`
- `TestSecurityHeaders_CSP` -- GET /health returns a Content-Security-Policy header
- `TestSecurityHeaders_ReferrerPolicy` -- GET /health returns `Referrer-Policy: strict-origin-when-cross-origin`

**Version wiring:**
- `TestAPIVersion_ReturnsConfiguredVersion` -- set version, verify /api/version returns it

**Cache stubs removed:**
- `TestCacheStatsRemoved` -- GET /api/cache/stats returns 404

### Phase 3 Tests (coverage gaps)

**HTTP handler tests (`internal/server/server_handler_test.go`):**
- `TestHealthEndpoint` -- GET /health returns 200 + `{"status":"ok"}`
- `TestAPIAccounts_Empty` -- GET /api/accounts with empty DB returns `[]`
- `TestAPIAccounts_WithData` -- seed accounts, verify JSON shape
- `TestAPISyncStatus_NoSyncs` -- verify `can_sync: true`, `syncs_today: 0`
- `TestAPISync_NoCredentials` -- POST /api/sync without SimpleFIN URL returns 400
- `TestAPISync_RateLimited` -- seed 20 runs, POST /api/sync returns 429
- `TestAPIVersion` -- verify returns version string
- `TestAPICategories` -- verify returns category list
- `TestAPISearch_MissingQuery` -- returns 400
- `TestAPIIncomeSource_EmptyMerchant` -- returns 400
- `TestAPICategoryOverride_UnknownCategory` -- returns 400

**Transfer pairing (`internal/classify/transfers_test.go`):**
- `TestDetectTransferPairs_SameDaySameAmount` -- matching in/out on same day
- `TestDetectTransferPairs_NoMatch` -- all same direction
- `TestDetectTransferPairs_DuplicateAmounts` -- multiple candidates, picks best
- `TestDetectTransferPairs_ZeroAmount` -- zero-value transactions not paired
- `TestDetectTransferPairs_CrossDay` -- matches within tolerance window
- `TestDetectTransferPairs_SameAccount` -- same-account legs not paired

**Refund matching (`internal/classify/refunds_test.go`):**
- `TestDetectRefundMatches_ExactMatch` -- refund matches expense exactly
- `TestDetectRefundMatches_PartialRefund` -- refund < expense within tolerance
- `TestDetectRefundMatches_NoExpense` -- refund with no matching expense
- `TestDetectRefundMatches_TooOld` -- expense outside lookback window
- `TestDetectRefundMatches_MultipleRefunds` -- multiple refunds for one expense

### Phase 4 Tests (polish)

- `TestSyncRateLimit_UsesConstant` -- verify the constant is used (compile-time check via import)
- Verify `t.Parallel()` runs cleanly with `-race` flag

### Coverage expectation
- All new code: 80%+ line coverage
- HTTP handlers: every route has at least one happy-path and one error-path test

---

## Acceptance Criteria

### Phase 1: Correctness Blockers

**AC-1: Migration errors are handled specifically**
- Given: a fresh database
- When: `db.Init()` runs migrations including a genuinely broken ALTER TABLE
- Then: `Init` returns a non-nil error wrapping the original failure
- And: duplicate-column ALTERs (re-running Init) succeed silently

**AC-2: Date ranges use UTC**
- Given: any server timezone
- When: `thisMonthRange()` is called
- Then: start and end dates are computed using `time.UTC`, not `time.Local`
- And: same for `lastMonthRange()`, `nPriorMonths()`, `handleBudgetView`, `handleReviewView`

**AC-3: Scan errors are surfaced**
- Given: a database query that fails mid-scan
- When: `newBaseData`, `queryPendingCount`, or `scanRecentTxns` encounters a scan error
- Then: the error is logged (not silently discarded)
- And: the function returns a safe default (zero/empty), not corrupted data

**AC-4: SaveIncomeSource DELETE error is checked**
- Given: a merchant with an existing income rule
- When: `SaveIncomeSource` is called and the DELETE fails
- Then: the function returns the DELETE error
- And: the INSERT is not attempted

**AC-5: All sub-module schemas initialized via db.Init()**
- Given: a fresh database opened via `fincore.NewServer`
- When: the server starts
- Then: `audit_events`, `closed_periods`, and `reconciliation_sessions` tables exist
- And: `cli/web.go` no longer calls `EnsureSchema` separately

### Phase 2: API / Architecture

**AC-6: fincore.NewServer returns a closeable server**
- Given: a caller imports `pkg/fincore`
- When: they call `NewServer(cfg)`
- Then: the returned value has both `ServeHTTP` and `Close() error` methods
- And: calling `Close` closes the underlying database connection

**AC-7: DB struct does not embed *sql.DB**
- Given: a caller has a `*db.DB`
- When: they attempt to call `d.Query(...)` directly (raw sql.DB method)
- Then: the code does not compile (method not promoted)
- And: all existing callers use wrapper methods on `*db.DB`

**AC-8: Security headers are set on all responses**
- Given: any HTTP request to the server
- When: the response is sent
- Then: headers include: `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: strict-origin-when-cross-origin`, and a `Content-Security-Policy` header
- And: Chart.js script tag in `base.html` has `integrity` and `crossorigin` attributes

**AC-9: /api/version returns the build version**
- Given: the server is started with version "1.2.3" injected via ldflags
- When: GET /api/version is called
- Then: response body contains `{"version":"1.2.3"}`

**AC-10: Cache stub routes are removed**
- Given: the server is running
- When: GET /api/cache/stats or POST /api/cache/clear is called
- Then: the server returns 404 (route not found)

### Phase 3: Test Gaps

**AC-11: HTTP handler tests exist**
- Given: the test suite
- When: `go test ./internal/server/...` runs
- Then: at least 10 handler tests pass, covering /health, /api/accounts, /api/sync-status, /api/sync (POST), and error paths

**AC-12: Transfer and refund matching tests exist**
- Given: the test suite
- When: `go test ./internal/classify/...` runs
- Then: transfer pairing tests cover same-day match, no-match, duplicates, zero-amount, cross-day
- And: refund matching tests cover exact, partial, no-expense, too-old, multiple-refunds

**AC-13: E2E config uses PATH-based go**
- Given: `e2e/playwright.config.ts`
- When: opened on any machine with `go` on PATH
- Then: the `webServer.command` does not contain an absolute path to `go.exe`
- And: uses `go build -o e2e/fin-test.exe ./cmd/fin` (relying on PATH)

### Phase 4: Polish

**AC-14: Sync rate limit uses a shared constant**
- Given: the constant `MaxSyncsPerDay` (or similar)
- When: sync rate limit is checked in `server/sync.go`, `server/server.go`, and `cli/sync.go`
- Then: all three reference the same constant, not the magic number `20`

**AC-15: All new tests use t.Parallel() and named subtests**
- Given: any new test function or subtest
- When: inspected
- Then: `t.Parallel()` is called at both the function and subtest level
- And: all `t.Run` calls use a descriptive name string, never `""`

---

## Out of Scope

These are explicitly deferred. Do not implement them in this iteration:

1. **`context.Context` on DB methods** -- Every DB method should eventually accept `ctx` as first param. This is a large, mechanical refactor touching every caller. Separate PR.
2. **Move view SQL into `internal/db`** -- Proper layering fix. Touches 400+ lines across views.go and views_extra.go. Separate PR after these fixes land.
3. **`go:embed` for templates/static** -- Makes the binary self-contained. Good, but unrelated to correctness. Separate PR.
4. **`NormalizeMerchant` vs `MerchantNormExpr` alignment** -- Needs design decision on whether SQL should strip suffixes. Separate design doc.
5. **`DB` interface for mock testing** -- Useful for error injection in handler tests. Can be added later; these tests will use real in-memory SQLite.
6. **Firefox/WebKit E2E projects** -- Chromium-only is acceptable for a local-first app. Document the decision.
7. **Cache eviction optimization** (heap vs sort) -- Performance optimization, not correctness.
8. **Request body size limits** -- Low risk for a local-first app. Document as known limitation.

---

## Risk Notes

**AC-7 (stop embedding *sql.DB)** is the highest-risk change. It touches every
file that calls raw `d.Query(...)` or `d.Exec(...)` on the `DB` struct. The
`classify/reporting.go` functions take `*sql.DB` directly (not `*db.DB`), so
they need `d.Underlying()` or an interface. Test by verifying `go build ./...`
compiles and `go test ./...` passes.

**AC-2 (UTC date ranges)** changes which transactions appear on the dashboard
for users whose server timezone is not UTC. This is a correctness fix, but users
may notice different data on the dashboard. The change is correct -- DB stores UTC.

**AC-6 (fincore return type change)** is a breaking API change for Kept. The
Kept codebase must be updated to call `.Close()` on the returned server. Since
Kept is our own code, this is acceptable.

**AC-10 (remove cache stubs)** -- check `ui/static/js/` for any frontend JS
that calls `/api/cache/stats` or `/api/cache/clear` before removing. If found,
remove the JS calls too.
