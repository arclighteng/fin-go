// Package fincore provides the public API for embedding fin in other applications (e.g., Kept).
//
// Usage:
//
//	cfg := fincore.LoadConfig()
//	srv, err := fincore.NewServer(cfg)
//	if err != nil { ... }
//	defer srv.Close()
//	// Wrap srv with auth middleware, then serve.
package fincore

import (
	"fmt"
	"net/http"

	"github.com/arclighteng/fin-go/internal/config"
	"github.com/arclighteng/fin-go/internal/db"
	"github.com/arclighteng/fin-go/internal/server"
)

// Config is the public configuration type.
type Config = config.Config

// LoadConfig loads configuration from environment/.env/keyring.
func LoadConfig() *Config {
	return config.Load()
}

// Server wraps the fin HTTP handler and holds the underlying database
// connection. Callers must call Close when done to release resources.
type Server struct {
	handler http.Handler
	db      *db.DB
}

// ServeHTTP implements http.Handler, delegating to the underlying fin handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

// Close releases the database connection held by this server.
func (s *Server) Close() error {
	return s.db.Close()
}

// NewServer creates the fin HTTP handler. The returned *Server implements
// http.Handler and can be wrapped with additional middleware (e.g., authentication).
// Callers must call Close() on the returned server to release the database connection.
//
// Set cfg.Version to the application version string; it defaults to "dev" when empty.
func NewServer(cfg *Config) (*Server, error) {
	config.EnsureDataDir(cfg.DBPath)

	database, err := db.Connect(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	if err := database.Init(); err != nil {
		database.Close()
		return nil, fmt.Errorf("initialize database: %w", err)
	}

	version := cfg.Version
	if version == "" {
		version = "dev"
	}
	handler := server.New(database, cfg, version)
	return &Server{handler: handler, db: database}, nil
}
