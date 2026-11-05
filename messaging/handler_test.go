package messaging

import (
	"context"
	"strings"
	"testing"

	"github.com/fastclaw-ai/weclaw/agent"
)

type fakeAgent struct {
	resetConversationIDs []string
	lastCwd              string
	infoName             string
}

func (f *fakeAgent) Chat(context.Context, string, string) (string, error) {
	return "", nil
}

func (f *fakeAgent) ResetSession(_ context.Context, conversationID string) (string, error) {
	f.resetConversationIDs = append(f.resetConversationIDs, conversationID)
	return "reset-session", nil
}

func (f *fakeAgent) Info() agent.AgentInfo {
	name := f.infoName
	if name == "" {
		name = "fake"
	}
	return agent.AgentInfo{Name: name}
}

func (f *fakeAgent) SetCwd(cwd string) {
	f.lastCwd = cwd
}

func newTestHandler() *Handler {
	return &Handler{agents: make(map[string]agent.Agent)}
}

func TestParseCommand_NoPrefix(t *testing.T) {
	h := newTestHandler()
	names, msg := h.parseCommand("hello world")
	if len(names) != 0 {
		t.Errorf("expected nil names, got %v", names)
	}
	if msg != "hello world" {
		t.Errorf("expected full text, got %q", msg)
	}
}

func TestParseCommand_SlashWithAgent(t *testing.T) {
	h := newTestHandler()
	names, msg := h.parseCommand("/claude explain this code")
	if len(names) != 1 || names[0] != "claude" {
		t.Errorf("expected [claude], got %v", names)
	}
	if msg != "explain this code" {
		t.Errorf("expected 'explain this code', got %q", msg)
	}
}

func TestParseCommand_AtPrefix(t *testing.T) {
	h := newTestHandler()
	names, msg := h.parseCommand("@claude explain this code")
	if len(names) != 1 || names[0] != "claude" {
		t.Errorf("expected [claude], got %v", names)
	}
	if msg != "explain this code" {
		t.Errorf("expected 'explain this code', got %q", msg)
	}
}

func TestParseCommand_MultiAgent(t *testing.T) {
	h := newTestHandler()
	names, msg := h.parseCommand("@cc @cx hello")
	if len(names) != 2 || names[0] != "claude" || names[1] != "codex" {
		t.Errorf("expected [claude codex], got %v", names)
	}
	if msg != "hello" {
		t.Errorf("expected 'hello', got %q", msg)
	}
}

func TestParseCommand_MultiAgentDedup(t *testing.T) {
	h := newTestHandler()
	names, msg := h.parseCommand("@cc @cc hello")
	if len(names) != 1 || names[0] != "claude" {
		t.Errorf("expected [claude] (deduped), got %v", names)
	}
	if msg != "hello" {
		t.Errorf("expected 'hello', got %q", msg)
	}
}

func TestParseCommand_SwitchOnly(t *testing.T) {
	h := newTestHandler()
	names, msg := h.parseCommand("/claude")
	if len(names) != 1 || names[0] != "claude" {
		t.Errorf("expected [claude], got %v", names)
	}
	if msg != "" {
		t.Errorf("expected empty message, got %q", msg)
	}
}

func TestParseCommand_Alias(t *testing.T) {
	h := newTestHandler()
	names, msg := h.parseCommand("/cc write a function")
	if len(names) != 1 || names[0] != "claude" {
		t.Errorf("expected [claude] from /cc alias, got %v", names)
	}
	if msg != "write a function" {
		t.Errorf("expected 'write a function', got %q", msg)
	}
}

func TestParseCommand_CustomAlias(t *testing.T) {
	h := newTestHandler()
	h.customAliases = map[string]string{"ai": "claude", "c": "claude"}
	names, msg := h.parseCommand("/ai hello")
	if len(names) != 1 || names[0] != "claude" {
		t.Errorf("expected [claude] from custom alias, got %v", names)
	}
	if msg != "hello" {
		t.Errorf("expected 'hello', got %q", msg)
	}
}

func TestResolveAlias(t *testing.T) {
	h := newTestHandler()
	tests := map[string]string{
		"cc":  "claude",
		"cx":  "codex",
		"oc":  "openclaw",
		"cs":  "cursor",
		"km":  "kimi",
		"gm":  "gemini",
		"ocd": "opencode",
	}
	for alias, want := range tests {
		got := h.resolveAlias(alias)
		if got != want {
			t.Errorf("resolveAlias(%q) = %q, want %q", alias, got, want)
		}
	}
	if got := h.resolveAlias("unknown"); got != "unknown" {
		t.Errorf("resolveAlias(unknown) = %q, want %q", got, "unknown")
	}
	h.customAliases = map[string]string{"cc": "custom-claude"}
	if got := h.resolveAlias("cc"); got != "custom-claude" {
		t.Errorf("resolveAlias(cc) with custom = %q, want custom-claude", got)
	}
}

func TestBuildHelpText(t *testing.T) {
	text := buildHelpText()
	if text == "" {
		t.Error("help text is empty")
	}
	if !strings.Contains(text, "/info") {
		t.Error("help text should mention /info")
	}
	if !strings.Contains(text, "/help") {
		t.Error("help text should mention /help")
	}
}

func TestHandleCwdResetsRunningAgentSession(t *testing.T) {
	tempDir := t.TempDir()
	h := newTestHandler()
	ag := &fakeAgent{}
	h.agents["copilot"] = ag

	reply := h.handleCwd(context.Background(), "wechat-user", "/cwd "+tempDir)

	if reply != "cwd: "+tempDir {
		t.Fatalf("handleCwd() reply = %q, want %q", reply, "cwd: "+tempDir)
	}
	if ag.lastCwd != tempDir {
		t.Fatalf("agent cwd = %q, want %q", ag.lastCwd, tempDir)
	}
	if len(ag.resetConversationIDs) != 1 || ag.resetConversationIDs[0] != "wechat-user" {
		t.Fatalf("reset conversation IDs = %v, want [wechat-user]", ag.resetConversationIDs)
	}
}

func TestSetDefaultAgentDoesNotClobberNewerDefault(t *testing.T) {
	h := newTestHandler()
	codex := &fakeAgent{infoName: "codex"}
	copilot := &fakeAgent{infoName: "copilot"}

	h.defaultName = "codex"
	h.agents["codex"] = codex

	h.SetDefaultAgent("copilot", copilot)

	if h.defaultName != "codex" {
		t.Fatalf("defaultName = %q, want %q", h.defaultName, "codex")
	}
	if got := h.agents["codex"]; got != codex {
		t.Fatalf("codex agent overwritten: got %#v, want %#v", got, codex)
	}
	if got := h.agents["copilot"]; got != copilot {
		t.Fatalf("copilot agent not registered: got %#v, want %#v", got, copilot)
	}
}
