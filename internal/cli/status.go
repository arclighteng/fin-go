package cli

import (
	"fmt"
	"time"

	"github.com/arclighteng/fin-go/internal/config"
	"github.com/arclighteng/fin-go/internal/credentials"
	"github.com/arclighteng/fin-go/internal/db"
	"github.com/arclighteng/fin-go/internal/money"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show account balances and sync status",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Load()
		database, err := db.Connect(cfg.DBPath)
		if err != nil {
			return fmt.Errorf("connect to database: %w", err)
		}
		defer database.Close()

		// Accounts
		accounts, err := database.GetAccounts()
		if err != nil {
			return fmt.Errorf("get accounts: %w", err)
		}

		if len(accounts) == 0 {
			fmt.Println("No accounts found. Run: fin sync")
			return nil
		}

		fmt.Printf("Accounts (%d):\n", len(accounts))
		for _, a := range accounts {
			id := a.AccountID
			if len(id) > 8 {
				id = id[:8]
			}
			fmt.Printf("  %s / %s (%s)\n", a.Institution, a.Name, id)
		}

		// Recent syncs
		runs, err := database.RecentRuns(5)
		if err != nil {
			return fmt.Errorf("get recent runs: %w", err)
		}

		fmt.Println()
		if len(runs) == 0 {
			fmt.Println("No syncs recorded yet.")
		} else {
			fmt.Println("Recent syncs:")
			for _, r := range runs {
				ago := time.Since(r.RanAt).Truncate(time.Minute)
				fmt.Printf("  %s ago: %d fetched, %d new, %d updated\n",
					ago, r.TxnsFetched, r.TxnsInserted, r.TxnsUpdated)
			}
		}

		// Rate limit
		count, err := database.RunsInLast24Hours()
		if err == nil {
			fmt.Printf("\nSync rate: %d/20 in last 24h\n", count)
		}

		// Credential status
		src := credentials.GetCredentialSource()
		fmt.Printf("Credential: %s\n", src)

		// Transaction count for current month
		now := time.Now()
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		nextMonth := monthStart.AddDate(0, 1, 0)
		txns, err := database.GetTransactions(monthStart, nextMonth)
		if err == nil {
			var totalCents int64
			for _, t := range txns {
				if t.AmountCents < 0 {
					totalCents += t.AmountCents
				}
			}
			fmt.Printf("\nThis month: %d transactions, %s spent\n",
				len(txns), money.FormatUSD(-totalCents))
		}

		return nil
	},
}
