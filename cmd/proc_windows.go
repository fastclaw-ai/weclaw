//go:build windows

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

func setSysProcAttr(_ *exec.Cmd) {
}

func processExists(pid int) bool {
	out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid)).Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), fmt.Sprintf(" %d ", pid))
}

func stopAllWeclaw() {
	if pid, err := readPid(); err == nil && processExists(pid) {
		_ = exec.Command("taskkill", "/F", "/PID", fmt.Sprintf("%d", pid)).Run()
	}
	os.Remove(pidFile())
	_ = exec.Command("taskkill", "/F", "/IM", "weclaw.exe").Run()
	time.Sleep(500 * time.Millisecond)
}
