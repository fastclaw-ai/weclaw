//go:build !windows

package cmd

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

func notifyContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
}

func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func processExists(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

func signalTerminate(p *os.Process) error {
	return p.Signal(syscall.SIGTERM)
}

func killByName(exePath string) {
	_ = exec.Command("pkill", "-f", exePath+" start").Run()
}
