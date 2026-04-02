package cli

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/arclighteng/fin-go/internal/config"
	"github.com/arclighteng/fin-go/internal/db"
	"github.com/arclighteng/fin-go/internal/server"
	"github.com/spf13/cobra"
)

var (
	webHost string
	webPort int
)

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Start the web server",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Load()
		config.EnsureDataDir(cfg.DBPath)

		database, err := db.Connect(cfg.DBPath)
		if err != nil {
			return fmt.Errorf("connect to database: %w", err)
		}
		defer database.Close()

		if err := database.Init(); err != nil {
			return fmt.Errorf("initialize database: %w", err)
		}

		srv := server.New(database, cfg, "", "", appVersion)
		addr := fmt.Sprintf("%s:%d", webHost, webPort)

		httpServer := &http.Server{
			Addr:    addr,
			Handler: srv,
		}

		// Graceful shutdown on SIGINT/SIGTERM.
		go func() {
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			<-sigCh
			log.Println("Shutting down gracefully...")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = httpServer.Shutdown(ctx)
		}()

		log.Printf("Starting fin %s on http://%s", appVersion, addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	},
}

func init() {
	webCmd.Flags().StringVar(&webHost, "host", "127.0.0.1", "Host to bind to")
	webCmd.Flags().IntVar(&webPort, "port", 8000, "Port to listen on")
}
