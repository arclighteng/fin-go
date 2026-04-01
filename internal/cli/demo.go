package cli

import (
	"fmt"

	"github.com/arclighteng/fin-go/internal/config"
	"github.com/arclighteng/fin-go/internal/db"
	"github.com/arclighteng/fin-go/internal/demo"
	"github.com/spf13/cobra"
)

var demoClear bool

var demoCmd = &cobra.Command{
	Use:   "demo",
	Short: "Manage demo data for exploring fin without a real bank connection",
	Long: `Generate or remove synthetic transaction data.

  fin demo           Generate 12 months of realistic sample transactions.
  fin demo --clear   Remove all demo accounts and transactions.

Demo accounts are prefixed with "demo-" and can be safely deleted without
affecting any real synced data.`,
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

		if demoClear {
			if err := demo.Clear(database.Underlying()); err != nil {
				return fmt.Errorf("clear demo data: %w", err)
			}
			fmt.Println("Demo data removed.")
			return nil
		}

		if err := demo.Generate(database.Underlying()); err != nil {
			return fmt.Errorf("generate demo data: %w", err)
		}
		fmt.Println("Demo data generated. Run `fin web` to explore the app.")
		return nil
	},
}

func init() {
	demoCmd.Flags().BoolVar(&demoClear, "clear", false, "Remove all demo accounts and transactions")
	rootCmd.AddCommand(demoCmd)
}
