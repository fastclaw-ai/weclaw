package cmd

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/fastclaw-ai/weclaw/config"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(webCmd)
}

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Open the WeClaw web management UI in a browser",
	RunE: func(cmd *cobra.Command, args []string) error {
		url := webUIURL()
		fmt.Printf("Opening %s\n", url)
		return openBrowser(url)
	},
}

func webUIURL() string {
	cfg, err := config.Load()
	if err == nil && cfg.APIAddr != "" {
		return "http://" + cfg.APIAddr
	}
	return "http://127.0.0.1:18011"
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		return exec.Command("xdg-open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return fmt.Errorf("unsupported platform")
	}
}
