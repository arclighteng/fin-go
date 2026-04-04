# ADR-002: JSON Body Size Limit Middleware

**Date**: 2026-04-03
**Status**: Proposed
**Deciders**: AR (maintainer)
**Tags**: architecture, security, middleware

---

## Context

The fin-go server accepts JSON request bodies on 11 POST/PATCH/DELETE endpoints
(income sources, category overrides, alert actions, annotations, budget targets,
commitments, SimpleFIN token, and duplicate dismissal). None of these endpoints
limit the size of `r.Body` before passing it to `json.NewDecoder().Decode()`.

A malicious or misconfigured client can send an arbitrarily large body, causing
the server to allocate unbounded memory until the process is OOM-killed or the
30-second timeout fires. This is a basic denial-of-service vector.

The CSV import endpoints already apply `http.MaxBytesReader` (10 MB cap), which
demonstrates the correct pattern. JSON payloads in this application are small
configuration objects -- the largest reasonable payload is a commitment creation
at roughly 500 bytes. A 1 MB cap provides three orders of magnitude of headroom.

### Decision Drivers

- All JSON bodies in this app are small (<1 KB typical, <10 KB theoretical max).
- CSV import already uses `MaxBytesReader`, creating an inconsistency.
- Middleware at the router group level avoids modifying all 11 handler call sites.
- `http.MaxBytesReader` is stdlib and integrates cleanly with `json.Decoder`.

---

## Decision

We will add a `maxJSONBody` middleware function applied to the `/api` chi.Route
group. The middleware will wrap `r.Body` with `http.MaxBytesReader(w, r.Body, limit)`
for requests whose `Content-Type` contains `application/json`. The limit will be
a package-level constant set to 1 MB (`1 << 20`).

When the limit is exceeded, `json.NewDecoder().Decode()` will return an error
containing `http: request body too large`. Handlers that already check decode
errors will naturally return 400. We will additionally add a check in the
middleware (or a shared decode helper) to detect `*http.MaxBytesError` and return
HTTP 413 (Payload Too Large) for clarity.

The middleware will NOT apply to multipart/form-data requests (CSV upload), which
have their own size control via `MaxBytesReader` in `readCSVUpload`.

---

## Consequences

### Positive
- Closes a denial-of-service vector with a single middleware addition.
- Zero changes required to existing handler code for basic protection.
- Consistent with the CSV upload pattern already in the codebase.
- 1 MB limit is generous enough that no legitimate request will be rejected.

### Negative
- If a future endpoint needs larger JSON payloads (unlikely for this app),
  the middleware constant must be increased or the endpoint must be exempted.
- `MaxBytesReader` closes the body after the limit, which can produce confusing
  error messages if handlers don't check for the specific error type.

### Neutral / Ongoing
- Handlers continue to use `json.NewDecoder(r.Body).Decode()` unchanged.
- The 413 vs 400 distinction is a refinement that can be added later via a
  shared `decodeJSON` helper without changing the middleware.

---

## Alternatives Considered

### Option A: Per-handler MaxBytesReader
**Description**: Add `r.Body = http.MaxBytesReader(w, r.Body, limit)` at the
top of each handler that decodes JSON.
**Pros**: Explicit per-handler limits; no middleware magic.
**Cons**: 11 call sites to modify; easy to forget on new endpoints; violates DRY.
**Reason rejected**: Middleware is idiomatic in chi and covers all current and
future JSON endpoints automatically.

### Option B: Shared `decodeJSON[T]` helper
**Description**: Replace all `json.NewDecoder().Decode()` calls with a generic
helper that wraps MaxBytesReader, decodes, and returns typed errors.
**Pros**: Centralizes decode logic, error formatting, and size limiting.
**Cons**: Larger refactor; changes every handler signature; harder to review.
**Reason rejected**: Good long-term direction but too large for Phase 2 scope.
Can be adopted incrementally after the middleware is in place.

### Doing nothing
**Description**: Rely on the 30-second timeout middleware to bound resource usage.
**Reason rejected**: A 30-second window is sufficient for an attacker to send
hundreds of MB. Memory exhaustion is faster than timeout expiry.

---

## Implementation Notes

Suggested placement in `server.go`:

```go
const maxJSONBodySize = 1 << 20 // 1 MB

func limitJSONBody(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        ct := r.Header.Get("Content-Type")
        if r.Body != nil && r.ContentLength != 0 &&
            (strings.Contains(ct, "application/json") || ct == "") {
            r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)
        }
        next.ServeHTTP(w, r)
    })
}
```

Apply inside the `/api` route group:

```go
r.Route("/api", func(r chi.Router) {
    r.Use(limitJSONBody)
    // ... existing routes
})
```

Note: checking `ct == ""` covers clients that omit Content-Type on POST, which
`json.Decoder` will still attempt to parse. This is defensive.

---

## Review Trigger

- If any JSON endpoint legitimately needs payloads >1 MB.
- If the codebase adopts a shared `decodeJSON` helper (Option B), the middleware
  may become redundant -- evaluate whether to keep both layers.

---

## References

- Go stdlib: https://pkg.go.dev/net/http#MaxBytesReader
- OWASP: https://cheatsheetseries.owasp.org/cheatsheets/Denial_of_Service_Cheat_Sheet.html
- Related: ADR-001 (template/data assembly)
