package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(restartCmd)
}

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the background weclaw process",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Stop if running
		pid, err := readPid()
		if err == nil && processExists(pid) {
			fmt.Printf("Stopping weclaw (pid=%d)...\n", pid)
			if p, err := os.FindProcess(pid); err == nil {
				_ = signalTerminate(p)
			}
			for i := 0; i < 20; i++ {
				if !processExists(pid) {
					break
				}
				time.Sleep(500 * time.Millisecond)
			}
			os.Remove(pidFile())
		}

		// Start
		fmt.Println("Starting weclaw...")
		return runDaemon()
	},
}
