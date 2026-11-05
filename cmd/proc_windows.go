//go:build windows

package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
)

func notifyContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(ctx, os.Interrupt)
}

func setSysProcAttr(_ *exec.Cmd) {
	// No Setsid on Windows — process is already detached via Start()
}

func processExists(pid int) bool {
	out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/NH").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), fmt.Sprintf("%d", pid))
}

func signalTerminate(p *os.Process) error {
	return p.Kill()
}

func killByName(exePath string) {
	_ = exec.Command("taskkill", "/F", "/IM", filepath.Base(exePath)).Run()
}
