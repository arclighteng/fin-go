package cli

import (
	"fmt"

	"github.com/arclighteng/fin-go/internal/credentials"
	"github.com/spf13/cobra"
)

var setupCmd = &cobra.Command{
	Use:   "setup [access-url]",
	Short: "Set up SimpleFIN connection",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		url := args[0]
		if err := credentials.SetSimpleFinURL(url); err != nil {
			return fmt.Errorf("store credential in system keyring: %w\n\n"+
				"No unlocked keyring? (headless server, locked login collection.) "+
				"Skip setup and export the URL in the environment instead:\n"+
				"  export SIMPLEFIN_ACCESS_URL='<access-url>'", err)
		}
		fmt.Println("SimpleFIN access URL stored in system keyring.")
		return nil
	},
}
