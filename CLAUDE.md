# fin-go

Personal finance dashboard — single-binary Go app with embedded UI.

## Build & Test

```bash
go build ./...
go test -race -count=1 ./...
```

CI runs `go test -race` on every push/PR to master (`.github/workflows/ci.yml`).

## Architecture

- `internal/server/` — HTTP handlers (chi router), middleware, template rendering
- `internal/db/` — SQLite data access (parameterized queries only)
- `internal/models/` — shared data types
- `internal/csvimport/` — CSV parsing for bank statement import
- `internal/categorize/` — transaction categorization
- `ui/templates/` — Go HTML templates (embedded via `go:embed`)
- `ui/static/` — CSS, JS assets (embedded; Chart.js vendored locally)

## Key Conventions

- **Every `fetch('/api/...')` in a template must have a matching route in `server.go` with a handler and test.**
  `TestAllTemplateFetchRoutesExist` enforces this — it scans all embedded templates for API calls and verifies none return 404.
- **CSRF**: All mutating requests require `X-Fin-Request: 1` header. Middleware rejects without it.
- **Security headers**: CSP, X-Frame-Options, etc. set in `securityHeaders` middleware.
- **Static assets are vendored** (no CDN dependencies) to avoid SRI hash drift.

## Adding a New API Route

1. Add DB method in `internal/db/queries.go` (parameterized SQL only)
2. Add handler in `internal/server/` (follow existing patterns in `api.go`)
3. Register route in `internal/server/server.go` under the `/api` block
4. Add handler test in `internal/server/server_handler_test.go`
5. The route coverage test will catch any template fetch() that lacks a route
