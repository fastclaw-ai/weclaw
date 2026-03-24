package config

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// agentCandidate defines one way to run an agent.
// Multiple candidates can map to the same agent name; the first detected wins.
type agentCandidate struct {
	Name      string   // agent name (e.g. "claude", "codex")
	Binary    string   // binary to look up in PATH
	Args      []string // extra args (e.g. ["acp"] for cursor)
	CheckArgs []string // optional capability probe args (must exit 0)
	Type      string   // "acp", "cli"
	Model     string   // default model
}

// agentCandidates is ordered by priority: for each agent name, earlier entries
// are preferred. E.g. claude ACP is tried before claude CLI.
var agentCandidates = []agentCandidate{
	// claude: prefer ACP, fallback to CLI
	{Name: "claude", Binary: "claude-agent-acp", Type: "acp", Model: "sonnet"},
	{Name: "claude", Binary: "claude", Type: "cli", Model: "sonnet"},
	// codex: prefer ACP, fallback to CLI
	{Name: "codex", Binary: "codex-acp", Type: "acp", Model: ""},
	{Name: "codex", Binary: "codex", Args: []string{"app-server", "--listen", "stdio://"}, CheckArgs: []string{"app-server", "--help"}, Type: "acp", Model: ""},
	{Name: "codex", Binary: "codex", Type: "cli", Model: ""},
	// ACP-only agents
	{Name: "cursor", Binary: "agent", Args: []string{"acp"}, Type: "acp", Model: ""},
	{Name: "kimi", Binary: "kimi", Args: []string{"acp"}, Type: "acp", Model: ""},
	{Name: "gemini", Binary: "gemini", Args: []string{"--acp"}, Type: "acp", Model: ""},
	{Name: "opencode", Binary: "opencode", Args: []string{"acp"}, Type: "acp", Model: ""},
	{Name: "openclaw", Binary: "openclaw", Type: "acp", Model: "openclaw:main"}, // args built dynamically
}

// defaultOrder defines the priority for choosing the default agent.
// Lower index = higher priority.
var defaultOrder = []string{
	"claude", "codex", "cursor", "kimi", "gemini", "opencode", "openclaw",
}

// DetectAndConfigure auto-detects local agents and populates the config.
// For each agent name, it picks the highest-priority candidate (acp > cli).
// Returns true if the config was modified.
func DetectAndConfigure(cfg *Config) bool {
	modified := false

	for _, candidate := range agentCandidates {
		path, err := exec.LookPath(candidate.Binary)
		if err != nil {
			continue
		}

		if len(candidate.CheckArgs) > 0 && !commandSupports(path, candidate.CheckArgs) {
			log.Printf("[config] skipping %s at %s (type=%s): capability probe failed (%v)",
				candidate.Name, path, candidate.Type, candidate.CheckArgs)
			continue
		}

		if existing, exists := cfg.Agents[candidate.Name]; exists {
			if !shouldUpgradeExisting(candidate.Name, existing, candidate) {
				continue
			}

			// Legacy codex CLI config upgrade path: prefer ACP when available.
			existing.Type = candidate.Type
			existing.Command = path
			existing.Args = cloneArgs(candidate.Args)
			if existing.Model == "" {
				existing.Model = candidate.Model
			}
			cfg.Agents[candidate.Name] = existing
			modified = true
			log.Printf("[config] upgraded %s to %s at %s", candidate.Name, candidate.Type, path)
			continue
		}

		log.Printf("[config] auto-detected %s at %s (type=%s)", candidate.Name, path, candidate.Type)
		cfg.Agents[candidate.Name] = AgentConfig{
			Type:    candidate.Type,
			Command: path,
			Args:    cloneArgs(candidate.Args),
			Model:   candidate.Model,
		}
		modified = true
	}

	// Special handling for openclaw: resolve gateway connection from
	// env vars -> ~/.openclaw/openclaw.json -> skip.
	if agCfg, exists := cfg.Agents["openclaw"]; exists && agCfg.Type == "acp" && len(agCfg.Args) == 0 {
		gwURL, gwToken, gwPassword := loadOpenclawGateway()
		if gwURL != "" {
			args := []string{"acp", "--url", gwURL, "--session", "agent:main:main"}
			if gwToken != "" {
				args = append(args, "--token", gwToken)
			} else if gwPassword != "" {
				args = append(args, "--password", gwPassword)
			}
			agCfg.Args = args
			cfg.Agents["openclaw"] = agCfg
			modified = true
			log.Printf("[config] openclaw ACP configured with gateway: %s", gwURL)
		} else {
			log.Printf("[config] openclaw binary found but no gateway config, skipping ACP")
			delete(cfg.Agents, "openclaw")
			modified = true
		}
	}

	// Fallback: if openclaw not configured, try HTTP via gateway config.
	if _, exists := cfg.Agents["openclaw"]; !exists {
		gwURL, gwToken, _ := loadOpenclawGateway()
		if gwURL != "" {
			// Convert ws(s):// to http(s):// for HTTP endpoint
			httpURL := gwURL
			httpURL = strings.Replace(httpURL, "wss://", "https://", 1)
			httpURL = strings.Replace(httpURL, "ws://", "http://", 1)
			endpoint := strings.TrimRight(httpURL, "/") + "/v1/chat/completions"
			log.Printf("[config] using openclaw HTTP fallback: %s", endpoint)
			cfg.Agents["openclaw"] = AgentConfig{
				Type:     "http",
				Endpoint: endpoint,
				APIKey:   gwToken,
				Model:    "openclaw:main",
			}
			modified = true
		}
	}

	// Pick the highest-priority default agent.
	if cfg.DefaultAgent == "" || !agentExists(cfg, cfg.DefaultAgent) {
		for _, name := range defaultOrder {
			if _, ok := cfg.Agents[name]; ok {
				if cfg.DefaultAgent != name {
					log.Printf("[config] setting default agent: %s", name)
					cfg.DefaultAgent = name
					modified = true
				}
				break
			}
		}
	}

	return modified
}

// loadOpenclawGateway resolves openclaw gateway connection info.
// Priority: env vars > ~/.openclaw/openclaw.json.
// Returns (url, token, password). url="" means not configured.
func loadOpenclawGateway() (gwURL, gwToken, gwPassword string) {
	// 1. Environment variables take priority
	gwURL = os.Getenv("OPENCLAW_GATEWAY_URL")
	gwToken = os.Getenv("OPENCLAW_GATEWAY_TOKEN")
	gwPassword = os.Getenv("OPENCLAW_GATEWAY_PASSWORD")
	if gwURL != "" {
		return
	}

	// 2. Read from ~/.openclaw/openclaw.json
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	data, err := os.ReadFile(filepath.Join(home, ".openclaw", "openclaw.json"))
	if err != nil {
		return
	}

	var ocCfg struct {
		Gateway struct {
			Port int    `json:"port"`
			Mode string `json:"mode"`
			Auth struct {
				Mode     string `json:"mode"`
				Token    string `json:"token"`
				Password string `json:"password"`
			} `json:"auth"`
			Remote struct {
				URL   string `json:"url"`
				Token string `json:"token"`
			} `json:"remote"`
		} `json:"gateway"`
	}
	if err := json.Unmarshal(data, &ocCfg); err != nil {
		log.Printf("[config] failed to parse openclaw config: %v", err)
		return
	}

	gw := ocCfg.Gateway

	// Remote gateway (gateway.remote.url)
	if gw.Remote.URL != "" {
		gwURL = gw.Remote.URL
		gwToken = gw.Remote.Token
		return
	}

	// Local gateway (gateway.port + gateway.auth)
	if gw.Port > 0 {
		gwURL = fmt.Sprintf("ws://127.0.0.1:%d", gw.Port)
		switch gw.Auth.Mode {
		case "token":
			gwToken = gw.Auth.Token
		case "password":
			gwPassword = gw.Auth.Password
		}
		return
	}

	return
}

func agentExists(cfg *Config, name string) bool {
	_, ok := cfg.Agents[name]
	return ok
}

func commandSupports(binary string, args []string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

func shouldUpgradeExisting(name string, existing AgentConfig, candidate agentCandidate) bool {
	if name != "codex" || existing.Type != "cli" || candidate.Type != "acp" {
		return false
	}
	return isLegacyCodexCLIConfig(existing)
}

func isLegacyCodexCLIConfig(cfg AgentConfig) bool {
	if cfg.Command == "" {
		return false
	}
	if base := strings.ToLower(filepath.Base(cfg.Command)); base != "codex" && base != "codex.exe" {
		return false
	}

	// Keep explicit customizations untouched.
	if cfg.Cwd != "" || cfg.Model != "" || cfg.SystemPrompt != "" {
		return false
	}
	if cfg.Endpoint != "" || cfg.APIKey != "" || cfg.MaxHistory != 0 {
		return false
	}

	// Default/legacy codex CLI flags that are ACP-safe to replace.
	if len(cfg.Args) == 0 {
		return true
	}
	return len(cfg.Args) == 1 && cfg.Args[0] == "--skip-git-repo-check"
}

func cloneArgs(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	out := make([]string, len(args))
	copy(out, args)
	return out
}
