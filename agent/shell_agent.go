package agent

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

// ShellAgent executes shell commands directly and returns their output.
// It allows users to run arbitrary commands from WeChat and see the results.
type ShellAgent struct {
	name    string
	shell   string // shell binary, e.g. "/bin/bash", "/bin/zsh"
	cwd     string
	env     map[string]string
	aliases []string
}

// ShellAgentConfig holds configuration for a shell agent.
type ShellAgentConfig struct {
	Name    string
	Shell   string            // shell binary (defaults to /bin/sh)
	Cwd     string            // working directory
	Env     map[string]string // extra environment variables
	Aliases []string
}

// NewShellAgent creates a new shell agent.
func NewShellAgent(cfg ShellAgentConfig) *ShellAgent {
	shell := cfg.Shell
	if shell == "" {
		shell = "/bin/sh"
	}
	cwd := cfg.Cwd
	if cwd == "" {
		cwd = defaultWorkspace()
	}
	return &ShellAgent{
		name:    cfg.Name,
		shell:   shell,
		cwd:     cwd,
		env:     cfg.Env,
		aliases: cfg.Aliases,
	}
}

// Info returns metadata about this agent.
func (a *ShellAgent) Info() AgentInfo {
	return AgentInfo{
		Name:    a.name,
		Type:    "shell",
		Command: a.shell,
	}
}

// ResetSession is a no-op for shell agents (no session state).
func (a *ShellAgent) ResetSession(_ context.Context, _ string) (string, error) {
	return "", nil
}

// SetCwd changes the working directory for subsequent commands.
func (a *ShellAgent) SetCwd(cwd string) {
	a.cwd = cwd
}

// Chat executes the message as a shell command and returns stdout+stderr.
func (a *ShellAgent) Chat(ctx context.Context, _ string, message string) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", fmt.Errorf("empty command")
	}

	log.Printf("[shell] executing command (shell=%s, cwd=%s): %s", a.shell, a.cwd, message)

	cmd := exec.CommandContext(ctx, a.shell, "-c", message)
	cmd.Dir = a.cwd
	if len(a.env) > 0 {
		cmdEnv, err := mergeEnv(os.Environ(), a.env)
		if err != nil {
			return "", fmt.Errorf("build shell env: %w", err)
		}
		cmd.Env = cmdEnv
	}

	// Capture both stdout and stderr together so the user sees all output.
	out, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(out))

	if err != nil {
		// If there's output, return it along with the error (e.g. command not found).
		if result != "" {
			return fmt.Sprintf("%s\n\n[exit: %s]", result, err), nil
		}
		return "", fmt.Errorf("command failed: %w", err)
	}

	if result == "" {
		return "(no output)", nil
	}

	return result, nil
}
