package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDetectCodexACPViaAppServer(t *testing.T) {
	dir := t.TempDir()
	codexPath := writeExecutable(t, dir, "codex", `#!/bin/sh
if [ "$1" = "app-server" ] && [ "$2" = "--help" ]; then
  exit 0
fi
exit 0
`)
	t.Setenv("PATH", dir)
	t.Setenv("OPENCLAW_GATEWAY_URL", "")
	t.Setenv("OPENCLAW_GATEWAY_TOKEN", "")
	t.Setenv("OPENCLAW_GATEWAY_PASSWORD", "")

	cfg := DefaultConfig()
	DetectAndConfigure(cfg)

	ag, ok := cfg.Agents["codex"]
	if !ok {
		t.Fatalf("codex was not detected")
	}
	if ag.Type != "acp" {
		t.Fatalf("expected codex type acp, got %q", ag.Type)
	}
	if ag.Command != codexPath {
		t.Fatalf("expected codex command %q, got %q", codexPath, ag.Command)
	}
	wantArgs := []string{"app-server", "--listen", "stdio://"}
	if !reflect.DeepEqual(ag.Args, wantArgs) {
		t.Fatalf("expected codex args %v, got %v", wantArgs, ag.Args)
	}
}

func TestDetectCodexFallbackToCLIWhenACPUnsupported(t *testing.T) {
	dir := t.TempDir()
	codexPath := writeExecutable(t, dir, "codex", `#!/bin/sh
if [ "$1" = "app-server" ] && [ "$2" = "--help" ]; then
  exit 1
fi
exit 0
`)
	t.Setenv("PATH", dir)
	t.Setenv("OPENCLAW_GATEWAY_URL", "")
	t.Setenv("OPENCLAW_GATEWAY_TOKEN", "")
	t.Setenv("OPENCLAW_GATEWAY_PASSWORD", "")

	cfg := DefaultConfig()
	DetectAndConfigure(cfg)

	ag, ok := cfg.Agents["codex"]
	if !ok {
		t.Fatalf("codex was not detected")
	}
	if ag.Type != "cli" {
		t.Fatalf("expected codex type cli, got %q", ag.Type)
	}
	if ag.Command != codexPath {
		t.Fatalf("expected codex command %q, got %q", codexPath, ag.Command)
	}
	if len(ag.Args) != 0 {
		t.Fatalf("expected empty codex args, got %v", ag.Args)
	}
}

func TestDetectUpgradesLegacyCodexCLIToACP(t *testing.T) {
	dir := t.TempDir()
	codexPath := writeExecutable(t, dir, "codex", `#!/bin/sh
if [ "$1" = "app-server" ] && [ "$2" = "--help" ]; then
  exit 0
fi
exit 0
`)
	t.Setenv("PATH", dir)
	t.Setenv("OPENCLAW_GATEWAY_URL", "")
	t.Setenv("OPENCLAW_GATEWAY_TOKEN", "")
	t.Setenv("OPENCLAW_GATEWAY_PASSWORD", "")

	cfg := DefaultConfig()
	cfg.Agents["codex"] = AgentConfig{
		Type:    "cli",
		Command: codexPath,
		Args:    []string{"--skip-git-repo-check"},
	}

	DetectAndConfigure(cfg)

	ag := cfg.Agents["codex"]
	if ag.Type != "acp" {
		t.Fatalf("expected codex to be upgraded to acp, got %q", ag.Type)
	}
	wantArgs := []string{"app-server", "--listen", "stdio://"}
	if !reflect.DeepEqual(ag.Args, wantArgs) {
		t.Fatalf("expected upgraded codex args %v, got %v", wantArgs, ag.Args)
	}
}

func TestDetectKeepsCustomizedCodexCLIConfig(t *testing.T) {
	dir := t.TempDir()
	codexPath := writeExecutable(t, dir, "codex", `#!/bin/sh
if [ "$1" = "app-server" ] && [ "$2" = "--help" ]; then
  exit 0
fi
exit 0
`)
	t.Setenv("PATH", dir)
	t.Setenv("OPENCLAW_GATEWAY_URL", "")
	t.Setenv("OPENCLAW_GATEWAY_TOKEN", "")
	t.Setenv("OPENCLAW_GATEWAY_PASSWORD", "")

	cfg := DefaultConfig()
	cfg.Agents["codex"] = AgentConfig{
		Type:    "cli",
		Command: codexPath,
		Cwd:     "/tmp/custom-workspace",
	}

	DetectAndConfigure(cfg)

	ag := cfg.Agents["codex"]
	if ag.Type != "cli" {
		t.Fatalf("expected codex to remain cli, got %q", ag.Type)
	}
	if ag.Cwd != "/tmp/custom-workspace" {
		t.Fatalf("expected codex cwd to be preserved, got %q", ag.Cwd)
	}
}

func writeExecutable(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}
	return path
}
