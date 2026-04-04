package server

import (
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/arclighteng/fin-go/internal/config"
	"github.com/arclighteng/fin-go/internal/db"
	"github.com/arclighteng/fin-go/internal/models"
	"github.com/arclighteng/fin-go/ui"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Server holds application state.
type Server struct {
	db      *db.DB
	cfg     *config.Config
	version string
}

// New creates the HTTP handler with all routes.
// version is the build version string (injected via -ldflags); defaults to "dev".
// Templates and static assets are embedded in the binary via go:embed.
func New(database *db.DB, cfg *config.Config, version string) http.Handler {
	if version == "" {
		version = "dev"
	}
	s := &Server{db: database, cfg: cfg, version: version}

	r := chi.NewRouter()

	// Middleware
	// Recoverer must wrap Logger so panics are caught before the logger records
	// the (never-written) status code.
	r.Use(middleware.Recoverer)
	r.Use(middleware.Logger)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(securityHeaders)
	r.Use(csrfProtection)

	// Embedded UI assets.
	templateFS, _ := fs.Sub(ui.Templates, "templates")
	staticFS, _ := fs.Sub(ui.Static, "static")

	if err := RegisterViewRoutes(r, database, templateFS, staticFS, version); err != nil {
		log.Printf("WARN: template views disabled: %v", err)
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/dashboard", http.StatusFound)
		})
		r.Get("/dashboard", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte("<html><body><h1>fin dashboard</h1><p>Templates failed to load.</p></body></html>"))
		})
	}

	r.Get("/health", s.handleHealth)

	// API
	r.Route("/api", func(r chi.Router) {
		r.Use(maxJSONBody)
		r.Get("/version", s.handleAPIVersion)
		r.Get("/accounts", s.handleAPIAccounts)
		r.Post("/sync", s.handleAPISync)
		r.Get("/sync-status", s.handleAPISyncStatus)
		r.Get("/sync-history", s.handleAPISyncHistory)
		r.Get("/categories", s.handleAPICategories)
		r.Get("/search", s.handleAPISearch)

		// Income sources
		r.Post("/income-source", s.handleAPIIncomeSource)
		r.Get("/income-sources", s.handleAPIIncomeSources)

		// Category overrides
		r.Post("/category-override", s.handleAPICategoryOverride)

		// Alert actions
		r.Post("/alert-action", s.handleAPIAlertAction)

		// Transaction annotations
		r.Get("/transaction/{fingerprint}/annotations", s.handleAPIGetAnnotations)
		r.Post("/transaction/{fingerprint}/note", s.handleAPISaveNote)
		r.Delete("/transaction/{fingerprint}/note", s.handleAPIDeleteNote)
		r.Post("/transaction/{fingerprint}/tag", s.handleAPIAddTag)
		r.Delete("/transaction/{fingerprint}/tag/{tag}", s.handleAPIDeleteTag)
		r.Get("/tags", s.handleAPIGetTags)

		// Budget
		r.Get("/budget/targets", s.handleAPIBudgetTargets)
		r.Post("/budget/target", s.handleAPISaveBudgetTarget)
		r.Delete("/budget/target/{categoryID}", s.handleAPIDeleteBudgetTarget)

		// SimpleFIN token
		r.Post("/simplefin-token", s.handleAPISimpleFinToken)

		// CSV import
		r.Post("/import/csv/preview", s.handleAPICSVPreview)
		r.Post("/import/csv/confirm", s.handleAPICSVConfirm)

		// Commitments
		r.Post("/commitments", s.handleAPICreateCommitment)
		r.Patch("/commitments/{id}", s.handleAPIUpdateCommitment)
		r.Delete("/commitments/{id}", s.handleAPIDeleteCommitment)
		r.Post("/dismiss-duplicate", s.handleAPIDismissDuplicate)
	})

	return r
}

// securityHeaders sets security-related HTTP response headers on every response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Cache-Control", "no-store, no-cache")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		// TODO(inline-js): script-src includes 'unsafe-inline' because templates use inline
		// <script> blocks. The proper fix is to extract all inline JS into separate .js files.
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; "+
				"font-src 'self'")
		next.ServeHTTP(w, r)
	})
}

// csrfProtection rejects state-mutating requests that do not carry the
// X-Fin-Request header. GET, HEAD, and OPTIONS are exempt because they are
// safe / pre-flight methods.
//
// This is a lightweight same-origin guard: cross-origin scripts cannot set
// custom headers without a CORS pre-flight, and the server does not emit
// Access-Control-Allow-Origin headers, so the browser will block pre-flights
// from foreign origins.
func csrfProtection(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			// Safe methods — no CSRF check required.
		default:
			if r.Header.Get("X-Fin-Request") != "1" {
				writeJSON(w, http.StatusForbidden, map[string]string{
					"error": "missing X-Fin-Request header",
				})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAPIVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": s.version})
}

func (s *Server) handleAPIAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.db.GetAccounts()
	if err != nil {
		log.Printf("handleAPIAccounts: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	// Return an empty JSON array rather than null when there are no accounts.
	if accounts == nil {
		accounts = []models.Account{}
	}
	writeJSON(w, http.StatusOK, accounts)
}

func (s *Server) handleAPISyncStatus(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"has_synced": false,
	}

	runs, err := s.db.RecentRuns(1)
	if err != nil {
		log.Printf("handleAPISyncStatus: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	if len(runs) > 0 {
		resp["has_synced"] = true
		resp["last_sync"] = map[string]any{
			"timestamp": runs[0].RanAt.Format(time.RFC3339),
		}
	}

	// Data range from transaction dates.
	oldest, errOld := s.db.OldestTransaction()
	newest, errNew := s.db.NewestTransaction()
	if errOld == nil && errNew == nil && !oldest.IsZero() && !newest.IsZero() {
		resp["data_range"] = map[string]string{
			"earliest": oldest.Format("2006-01-02"),
			"latest":   newest.Format("2006-01-02"),
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAPISyncHistory(w http.ResponseWriter, r *http.Request) {
	runs, err := s.db.RecentRuns(20)
	if err != nil {
		log.Printf("handleAPISyncHistory: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

// maxJSONBodySize limits the request body for non-multipart API calls.
const maxJSONBodySize = 1 << 20 // 1 MB

// maxJSONBody wraps r.Body with http.MaxBytesReader for non-multipart
// requests. Multipart requests (CSV uploads) are excluded because they
// manage their own size limits via http.MaxBytesReader in readCSVUpload.
func maxJSONBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		if r.Body != nil && !strings.HasPrefix(ct, "multipart/") {
			r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("json encode error: %v", err)
	}
}
