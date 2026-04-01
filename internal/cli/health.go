package cli

import (
	"fmt"

	"github.com/arclighteng/fin-go/internal/config"
	"github.com/arclighteng/fin-go/internal/credentials"
	"github.com/arclighteng/fin-go/internal/db"
	"github.com/spf13/cobra"
)

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check system health",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Load()

		fmt.Println("fin health check")
		fmt.Println("================")

		// Database
		database, err := db.Connect(cfg.DBPath)
		if err != nil {
			fmt.Printf("Database: FAIL (%v)\n", err)
		} else {
			fmt.Printf("Database: OK (%s)\n", cfg.DBPath)
			database.Close()
		}

		// Credentials
		src := credentials.GetCredentialSource()
		fmt.Printf("Credentials: %s\n", src)

		// Timezone
		fmt.Printf("Timezone: %s\n", cfg.Timezone)

		return nil
	},
}
