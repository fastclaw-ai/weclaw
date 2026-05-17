package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(restartCmd)
}

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the background weclaw process",
	RunE: func(cmd *cobra.Command, args []string) error {
		stopAllWeclaw()
		fmt.Println("Starting weclaw...")
		return runDaemon()
	},
}
