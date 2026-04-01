package cli

import (
	"context"
	"fmt"
	"log"

	"github.com/arclighteng/fin-go/internal/config"
	"github.com/arclighteng/fin-go/internal/credentials"
	"github.com/arclighteng/fin-go/internal/db"
	"github.com/arclighteng/fin-go/internal/server"
	"github.com/arclighteng/fin-go/internal/simplefin"
	"github.com/spf13/cobra"
)

var syncLookback int

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync accounts and transactions from SimpleFIN",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Load()
		database, err := db.Connect(cfg.DBPath)
		if err != nil {
			return fmt.Errorf("connect to database: %w", err)
		}
		defer database.Close()

		if err := database.Init(); err != nil {
			return fmt.Errorf("init database: %w", err)
		}

		// Check rate limit
		count, err := database.RunsInLast24Hours()
		if err != nil {
			return fmt.Errorf("check rate limit: %w", err)
		}
		if count >= server.MaxSyncsPerDay {
			return fmt.Errorf("rate limit reached: %d syncs in the last 24 hours (max %d)", count, server.MaxSyncsPerDay)
		}

		// Get access URL
		accessURL, err := credentials.GetSimpleFinURL()
		if err != nil || accessURL == "" {
			return fmt.Errorf("no SimpleFIN access URL configured. Run: fin setup <access-url>")
		}

		client, err := simplefin.NewClient(accessURL)
		if err != nil {
			return fmt.Errorf("create SimpleFIN client: %w", err)
		}

		result, err := client.Fetch(context.Background(), syncLookback)
		if err != nil {
			return fmt.Errorf("fetch from SimpleFIN: %w", err)
		}

		// Upsert accounts
		if err := database.UpsertAccounts(result.Accounts); err != nil {
			return fmt.Errorf("upsert accounts: %w", err)
		}

		// Upsert transactions
		inserted, updated, err := database.UpsertTransactions(result.Transactions)
		if err != nil {
			return fmt.Errorf("upsert transactions: %w", err)
		}

		// Record run
		if err := database.RecordRun(syncLookback, len(result.Transactions), inserted, updated); err != nil {
			log.Printf("warning: failed to record run: %v", err)
		}

		fmt.Printf("Sync complete: %d accounts, %d transactions fetched\n", len(result.Accounts), len(result.Transactions))
		fmt.Printf("  Inserted: %d, Updated: %d\n", inserted, updated)
		fmt.Printf("  Rate limit: %d/%d syncs today\n", count+1, server.MaxSyncsPerDay)

		return nil
	},
}

func init() {
	syncCmd.Flags().IntVar(&syncLookback, "lookback", 30, "Number of days to look back")
}
