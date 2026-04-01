package server

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/arclighteng/fin-go/internal/config"
	"github.com/arclighteng/fin-go/internal/db"
	"github.com/arclighteng/fin-go/internal/models"
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
// templateDir and staticDir specify the paths to the UI assets.
// Pass empty strings to use the defaults derived from the binary location.
// version is the build version string (injected via -ldflags); defaults to "dev".
func New(database *db.DB, cfg *config.Config, templateDir, staticDir, version string) http.Handler {
	if version == "" {
		version = "dev"
	}
	s := &Server{db: database, cfg: cfg, version: version}

	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(securityHeaders)

	// Template-rendered pages (dashboard, connect, sync-log).
	if templateDir == "" {
		templateDir = DefaultTemplateDir()
	}
	if staticDir == "" {
		staticDir = DefaultStaticDir()
	}
	if err := RegisterViewRoutes(r, database, templateDir, staticDir, version); err != nil {
		log.Printf("WARN: template views disabled: %v", err)
		// Fall back to stub handlers if templates are missing.
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/dashboard", http.StatusFound)
		})
		r.Get("/dashboard", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte("<html><body><h1>fin dashboard</h1><p>Templates not found. Run from the go/ directory or set FIN_TEMPLATES_DIR.</p></body></html>"))
		})
	}

	r.Get("/health", s.handleHealth)

	// API
	r.Route("/api", func(r chi.Router) {
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
	})

	return r
}

// securityHeaders sets security-related HTTP response headers on every response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' https://cdn.jsdelivr.net; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; "+
				"font-src 'self'")
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
	count, err := s.db.RunsInLast24Hours()
	if err != nil {
		log.Printf("handleAPISyncStatus: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"syncs_today": count,
		"limit":       MaxSyncsPerDay,
		"can_sync":    count < MaxSyncsPerDay,
	})
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

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("json encode error: %v", err)
	}
}
