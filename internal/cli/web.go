package cli

import (
	"fmt"
	"log"
	"net/http"

	"github.com/arclighteng/fin-go/internal/config"
	"github.com/arclighteng/fin-go/internal/db"
	"github.com/arclighteng/fin-go/internal/server"
	"github.com/spf13/cobra"
)

var (
	webHost  string
	webPort  int
	webNoTLS bool
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

		log.Printf("Starting fin %s on http://%s", appVersion, addr)
		return http.ListenAndServe(addr, srv)
	},
}

func init() {
	webCmd.Flags().StringVar(&webHost, "host", "127.0.0.1", "Host to bind to")
	webCmd.Flags().IntVar(&webPort, "port", 8000, "Port to listen on")
	webCmd.Flags().BoolVar(&webNoTLS, "no-tls", false, "Disable TLS")
}
