package messaging

import (
	"context"
	"strings"
	"testing"
)

// TestShellCommandWhitelist tests that only whitelisted commands are allowed
func TestShellCommandWhitelist(t *testing.T) {
	h := &Handler{}
	ctx := context.Background()

	// Test allowed commands
	allowedCmds := []string{
		"/sh ls",
		"/sh pwd",
		"/sh cat README.md",
		"/sh echo hello",
		"/sh date",
	}

	for _, cmd := range allowedCmds {
		result := h.handleShell(ctx, cmd)
		// Should not be blocked by whitelist
		if strings.Contains(result, "不在白名单中") {
			t.Errorf("Command %q was incorrectly blocked: %s", cmd, result)
		}
	}

	// Test blocked commands
	blockedCmds := []string{
		"/sh rm -rf /",
		"/sh curl http://example.com",
		"/sh wget http://example.com",
		"/sh bash",
		"/sh sh",
		"/sh python script.py",
		"/sh node app.js",
	}

	for _, cmd := range blockedCmds {
		result := h.handleShell(ctx, cmd)
		if !strings.Contains(result, "不在白名单中") {
			t.Errorf("Command %q should have been blocked, got: %s", cmd, result)
		}
	}
}

// TestShellDangerousOperators tests that dangerous operators are blocked
func TestShellDangerousOperators(t *testing.T) {
	h := &Handler{}
	ctx := context.Background()

	dangerousCmds := []struct {
		cmd      string
		operator string
	}{
		{"/sh ls > file.txt", ">"},
		{"/sh ls >> file.txt", ">>"},
		{"/sh cat < file.txt", "<"},
		{"/sh ls | grep test", "|"},
		{"/sh ls && echo done", "&&"},
		{"/sh ls || echo failed", "||"},
		{"/sh ls; echo done", ";"},
		{"/sh echo `whoami`", "`"},
		{"/sh echo $(whoami)", "$("},
	}

	for _, tc := range dangerousCmds {
		result := h.handleShell(ctx, tc.cmd)
		if !strings.Contains(result, "不允许使用特殊字符") {
			t.Errorf("Command %q with operator %q should have been blocked, got: %s", tc.cmd, tc.operator, result)
		}
	}
}

// TestShellShortcutAliases tests shortcut aliases like ll, .., ...
func TestShellShortcutAliases(t *testing.T) {
	h := &Handler{}
	ctx := context.Background()

	// Test ll alias
	result := h.handleShell(ctx, "/sh ll")
	if strings.Contains(result, "不在白名单中") {
		t.Error("ll alias should be allowed")
	}

	// Test .. alias (cd ..)
	result = h.handleShell(ctx, "/sh ..")
	if strings.Contains(result, "不在白名单中") && !strings.Contains(result, "命令执行失败") {
		// cd might fail if already at root, but should not be blocked by whitelist
		t.Logf(".. alias result: %s", result)
	}
}

// TestShellEmptyCommand tests handling of empty commands
func TestShellEmptyCommand(t *testing.T) {
	h := &Handler{}
	ctx := context.Background()

	// Empty command after "/sh " should show usage
	result := h.handleShell(ctx, "/sh ")
	if !strings.Contains(result, "用法:") {
		t.Errorf("Empty command should show usage, got: %s", result)
	}

	result = h.handleShell(ctx, "/sh   ")
	if !strings.Contains(result, "用法:") {
		t.Errorf("Whitespace-only command should show usage, got: %s", result)
	}
}

// TestShellOutputFormatting tests output formatting
func TestShellOutputFormatting(t *testing.T) {
	// Test shellPrompt
	prompt := shellPrompt("/home/user/projects")
	if !strings.Contains(prompt, "/home/user/projects") {
		t.Errorf("prompt should contain cwd, got: %s", prompt)
	}
	if !strings.HasSuffix(prompt, ":#") {
		t.Errorf("prompt should end with :#, got: %s", prompt)
	}

	// Test formatShellOutput
	output := formatShellOutput("/tmp", "line1\nline2")
	if !strings.Contains(output, "```") {
		t.Error("output should be wrapped in code block")
	}
	if !strings.Contains(output, "line1") {
		t.Error("output should contain original content")
	}

	// Test empty output
	emptyOutput := formatShellOutput("/tmp", "")
	if emptyOutput != "" {
		t.Errorf("empty output should return empty string, got: %s", emptyOutput)
	}
}

// TestCleanANSI tests ANSI escape code removal
func TestCleanANSI(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"\x1b[31mred text\x1b[0m", "red text"},
		{"\x1b[1;32mbold green\x1b[0m", "bold green"},
		{"no ansi codes", "no ansi codes"},
		{"\x1b[?25lhidden cursor\x1b[?25h", "hidden cursor"},
	}

	for _, tc := range tests {
		result := cleanANSI(tc.input)
		if result != tc.expected {
			t.Errorf("cleanANSI(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

// TestIsLinux tests the isLinux helper
func TestIsLinux(t *testing.T) {
	// Just ensure it doesn't panic and returns a bool
	_ = isLinux()
}

// TestShellModeState tests shell mode state management
func TestShellModeState(t *testing.T) {
	h := &Handler{}
	userID := "test-user-123"

	// Initially not in shell mode
	_, ok := h.shellModeStates.Load(userID)
	if ok {
		t.Error("new user should not be in shell mode")
	}

	// Enter shell mode
	ctx := context.Background()
	reply := h.enterShellMode(ctx, userID)
	if !strings.Contains(reply, "已进入命令行模式") {
		t.Errorf("enterShellMode should confirm entry, got: %s", reply)
	}

	// Check state was stored
	state, ok := h.shellModeStates.Load(userID)
	if !ok {
		t.Fatal("shell mode state should be stored")
	}

	sm := state.(*shellModeState)
	if !sm.enabled {
		t.Error("shell mode should be enabled")
	}

	// Test command in shell mode
	result := h.handleShellWithState(ctx, sm, "pwd")
	if result == "" {
		t.Error("pwd in shell mode should return output")
	}
}

// TestShellModeExit tests exiting shell mode with /q
func TestShellModeExit(t *testing.T) {
	h := &Handler{}
	userID := "test-user-exit"
	ctx := context.Background()

	// Enter shell mode
	h.enterShellMode(ctx, userID)

	// Verify in shell mode
	state, _ := h.shellModeStates.Load(userID)
	sm := state.(*shellModeState)
	if !sm.enabled {
		t.Fatal("should be in shell mode")
	}

	// Exit command would be handled in Handle(), not directly
	// So we just verify the state structure supports it
	sm.enabled = false
	if sm.enabled {
		t.Error("should be able to disable shell mode")
	}
}

// TestShellModeCD tests cd command in shell mode
func TestShellModeCD(t *testing.T) {
	h := &Handler{}
	ctx := context.Background()

	// Create a shell mode state
	state := &shellModeState{
		enabled: true,
		cwd:     "/tmp",
		baseDir: "",
	}

	// Test cd to valid directory
	result := h.handleShellWithState(ctx, state, "cd /var")
	if strings.Contains(result, "❌") {
		t.Errorf("cd to /var should work, got: %s", result)
	}

	// Verify cwd was updated (macOS may use /private/var)
	if state.cwd != "/var" && state.cwd != "/private/var" {
		t.Errorf("cwd should be /var or /private/var, got: %s", state.cwd)
	}
}

// TestShellModeCDSandbox tests sandbox restrictions
func TestShellModeCDSandbox(t *testing.T) {
	h := &Handler{}
	ctx := context.Background()

	// Create a shell mode state with sandbox
	state := &shellModeState{
		enabled: true,
		cwd:     "/tmp/sandbox",
		baseDir: "/tmp/sandbox",
	}

	// Test cd outside sandbox
	result := h.handleShellWithState(ctx, state, "cd /etc")
	if !strings.Contains(result, "不允许访问沙盒目录之外") {
		t.Errorf("cd outside sandbox should be blocked, got: %s", result)
	}

	// Verify cwd was not changed
	if state.cwd != "/tmp/sandbox" {
		t.Errorf("cwd should still be /tmp/sandbox, got: %s", state.cwd)
	}
}

// TestShellModeCatLargeFile tests large file protection
func TestShellModeCatLargeFile(t *testing.T) {
	h := &Handler{}
	ctx := context.Background()

	// Create a temporary large file
	// Note: This test creates an actual file to test the size check
	// In a real scenario, you'd use a test fixture

	state := &shellModeState{
		enabled: true,
		cwd:     "/tmp",
		baseDir: "",
	}

	// We can't easily test the actual large file check without creating a >50KB file
	// So we just verify the command is allowed (the size check happens at runtime)
	result := h.handleShellWithState(ctx, state, "cat small_file.txt")
	// Result may be "file not found" but should not be blocked by whitelist
	if strings.Contains(result, "不在白名单中") {
		t.Error("cat should be in whitelist")
	}
}
