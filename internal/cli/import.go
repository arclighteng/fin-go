package cli

import (
	"fmt"
	"os"

	"github.com/arclighteng/fin-go/internal/config"
	"github.com/arclighteng/fin-go/internal/csvimport"
	"github.com/arclighteng/fin-go/internal/db"
	"github.com/arclighteng/fin-go/internal/models"
	"github.com/spf13/cobra"
)

var (
	importAccountID  string
	importDateFormat string
	importDelimiter  string
	importDryRun     bool
)

var importCmd = &cobra.Command{
	Use:   "import <file.csv>",
	Short: "Import transactions from a bank CSV export",
	Long: `Parse a CSV file exported from your bank and load the transactions into fin.

Bank formats are auto-detected from column headers. Supported banks:
  Chase, Bank of America, American Express, Wells Fargo, Capital One.

Unknown formats are handled via a best-effort column search (date, amount,
description/merchant).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filePath := args[0]

		f, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("open file: %w", err)
		}
		defer f.Close()

		delim := ','
		if importDelimiter != "" {
			runes := []rune(importDelimiter)
			if len(runes) != 1 {
				return fmt.Errorf("--delimiter must be a single character, got %q", importDelimiter)
			}
			delim = runes[0]
		}

		opts := csvimport.ImportOptions{
			AccountID:  importAccountID,
			DateFormat: importDateFormat,
			Delimiter:  delim,
			DryRun:     importDryRun,
		}

		result, err := csvimport.Import(f, opts)
		if err != nil {
			return fmt.Errorf("parse CSV: %w", err)
		}

		// Report parse errors before proceeding.
		for _, e := range result.Errors {
			fmt.Fprintf(os.Stderr, "warning: %s\n", e)
		}

		if importDryRun {
			fmt.Printf("Dry run: parsed %d transaction(s), %d error(s)\n",
				len(result.Transactions), len(result.Errors))
			for _, t := range result.Transactions {
				fmt.Printf("  %s  %s  %s\n",
					t.PostedAt.Format("2006-01-02"),
					formatCents(t.AmountCents),
					displayName(t),
				)
			}
			return nil
		}

		if len(result.Transactions) == 0 {
			fmt.Println("No transactions parsed.")
			return nil
		}

		cfg := config.Load()
		database, err := db.Connect(cfg.DBPath)
		if err != nil {
			return fmt.Errorf("connect to database: %w", err)
		}
		defer database.Close()

		if err := database.Init(); err != nil {
			return fmt.Errorf("init database: %w", err)
		}

		// Ensure the target account exists so the foreign-key-style reference works.
		accountID := importAccountID
		if accountID == "" {
			accountID = "csv-import"
		}
		if err := database.UpsertAccounts([]models.Account{
			{
				AccountID:   accountID,
				Institution: "Manual Import",
				Name:        accountID,
				Type:        "checking",
				Currency:    "USD",
			},
		}); err != nil {
			return fmt.Errorf("ensure account: %w", err)
		}

		inserted, _, err := database.UpsertTransactions(result.Transactions)
		if err != nil {
			return fmt.Errorf("write transactions: %w", err)
		}
		skipped := len(result.Transactions) - inserted

		fmt.Printf("Import complete: %d inserted, %d skipped (duplicates), %d parse error(s)\n",
			inserted, skipped, len(result.Errors))

		return nil
	},
}

func init() {
	importCmd.Flags().StringVar(&importAccountID, "account", "csv-import", "Account ID to assign imported transactions")
	importCmd.Flags().StringVar(&importDateFormat, "date-format", "", "Go time layout for dates (e.g. 01/02/2006); auto-detected when omitted")
	importCmd.Flags().StringVar(&importDelimiter, "delimiter", ",", "Field delimiter (single character)")
	importCmd.Flags().BoolVar(&importDryRun, "dry-run", false, "Parse and preview without writing to the database")

	rootCmd.AddCommand(importCmd)
}

// formatCents renders cents as a signed dollar string for dry-run display.
func formatCents(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s$%d.%02d", sign, cents/100, cents%100)
}

// displayName returns the most informative name for a transaction row.
func displayName(t models.Transaction) string {
	if t.Merchant != "" {
		return t.Merchant
	}
	return t.Description
}
