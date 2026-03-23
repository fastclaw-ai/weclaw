package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// agentCandidate defines one way to run an agent.
// Multiple candidates can map to the same agent name; the first detected wins.
type agentCandidate struct {
	Name   string   // agent name (e.g. "claude", "codex")
	Binary string   // binary to look up in PATH
	Args   []string // extra args (e.g. ["acp"] for cursor)
	Type   string   // "acp", "cli"
	Model  string   // default model
}

// agentCandidates is ordered by priority: for each agent name, earlier entries
// are preferred. E.g. claude ACP is tried before claude CLI.
var agentCandidates = []agentCandidate{
	// claude: prefer ACP, fallback to CLI
	{Name: "claude", Binary: "claude-agent-acp", Type: "acp", Model: "sonnet"},
	{Name: "claude", Binary: "claude", Type: "cli", Model: "sonnet"},
	// codex: prefer ACP, fallback to CLI
	{Name: "codex", Binary: "codex-acp", Type: "acp", Model: ""},
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
		// Skip if this agent name is already configured
		if _, exists := cfg.Agents[candidate.Name]; exists {
			continue
		}

		path, err := lookPath(candidate.Binary)
		if err != nil {
			continue
		}

		log.Printf("[config] auto-detected %s at %s (type=%s)", candidate.Name, path, candidate.Type)
		cfg.Agents[candidate.Name] = AgentConfig{
			Type:    candidate.Type,
			Command: path,
			Args:    candidate.Args,
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

// lookPath finds a binary by name. It first tries exec.LookPath (fast, uses
// current PATH). If that fails, it falls back to resolving via a login shell
// which sources the user's profile (~/.zshrc, ~/.bashrc) — this picks up
// binaries installed through version managers like nvm, mise, etc. that only
// add their paths in interactive shells.
func lookPath(binary string) (string, error) {
	// Fast path: binary is in current PATH
	if p, err := exec.LookPath(binary); err == nil {
		return p, nil
	}

	// Fallback: resolve via login interactive shell (sources .zshrc/.bashrc)
	shell := "zsh"
	if runtime.GOOS != "darwin" {
		shell = "bash"
	}
	out, err := exec.Command(shell, "-lic", "which "+binary).Output()
	if err != nil {
		return "", fmt.Errorf("not found: %s", binary)
	}
	p := strings.TrimSpace(string(out))
	if p == "" || strings.Contains(p, "not found") {
		return "", fmt.Errorf("not found: %s", binary)
	}
	log.Printf("[config] resolved %s via login shell: %s", binary, p)
	return p, nil
}
