package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// AgentInfo holds metadata about an agent for logging/debugging.
type AgentInfo struct {
	Name    string // e.g. "claude-acp", "claude", "gpt-4o"
	Type    string // e.g. "acp", "cli", "http"
	Model   string // e.g. "sonnet", "gpt-4o-mini"
	Command string // binary path, e.g. "/usr/local/bin/claude-agent-acp"
	PID     int    // subprocess PID (0 if not applicable, e.g. http agent)
}

// String returns a human-readable summary for logging.
func (i AgentInfo) String() string {
	s := fmt.Sprintf("name=%s, type=%s, model=%s, command=%s", i.Name, i.Type, i.Model, i.Command)
	if i.PID > 0 {
		s += fmt.Sprintf(", pid=%d", i.PID)
	}
	return s
}

// defaultWorkspace returns ~/.weclaw/workspace as the default working directory.
func defaultWorkspace() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	dir := filepath.Join(home, ".weclaw", "workspace")
	os.MkdirAll(dir, 0o755)
	return dir
}

// mergeEnv merges extra environment variables into the base environment.
func mergeEnv(base []string, extra map[string]string) ([]string, error) {
	if len(extra) == 0 {
		return base, nil
	}

	merged := append([]string(nil), base...)
	indexByKey := make(map[string]int, len(base))
	for i, entry := range merged {
		key, _, found := strings.Cut(entry, "=")
		if !found || key == "" {
			continue
		}
		indexByKey[key] = i
	}

	newKeys := make([]string, 0, len(extra))
	for key, value := range extra {
		if key == "" || strings.Contains(key, "=") {
			return nil, fmt.Errorf("invalid env key %q", key)
		}
		entry := key + "=" + value
		if idx, ok := indexByKey[key]; ok {
			merged[idx] = entry
			continue
		}
		newKeys = append(newKeys, key)
	}

	sort.Strings(newKeys)
	for _, key := range newKeys {
		merged = append(merged, key+"="+extra[key])
	}

	return merged, nil
}

// Agent is the interface for AI chat agents.
type Agent interface {
	// Chat sends a message to the agent and returns the response.
	// conversationID is used to maintain conversation history per user.
	Chat(ctx context.Context, conversationID string, message string) (string, error)

	// ResetSession clears the existing session for the given conversationID and
	// starts a new one. Returns the new session ID if immediately available
	// (ACP mode), or an empty string if the ID will be assigned on next Chat
	// (CLI mode) or is not applicable (HTTP mode).
	ResetSession(ctx context.Context, conversationID string) (string, error)

	// Info returns metadata about this agent.
	Info() AgentInfo

	// SetCwd changes the working directory for subsequent operations.
	SetCwd(cwd string)
}

// Attachment describes a local file attached to a user message.
type Attachment struct {
	Path     string
	MIMEType string
	Name     string
}

// AttachmentChatAgent can receive local file attachments in addition to text.
type AttachmentChatAgent interface {
	ChatWithAttachments(ctx context.Context, conversationID string, message string, attachments []Attachment) (string, error)
}

// ToolProgressEvent describes a tool-call progress update from an agent.
type ToolProgressEvent struct {
	Phase      string // "start" or "end"
	ToolName   string
	ToolCallID string
	Status     string // "completed", "failed", "blocked", or "unknown"
}

// ProgressChatAgent can be implemented by agents that expose tool-call progress
// for a single Chat call.
type ProgressChatAgent interface {
	ChatWithProgress(ctx context.Context, conversationID string, message string, cb func(ToolProgressEvent)) (string, error)
}

// AttachmentProgressChatAgent can receive attachments and emit progress for a
// single Chat call.
type AttachmentProgressChatAgent interface {
	ChatWithAttachmentsAndProgress(ctx context.Context, conversationID string, message string, attachments []Attachment, cb func(ToolProgressEvent)) (string, error)
}

func MessageWithAttachmentFallback(message string, attachments []Attachment) string {
	if len(attachments) == 0 {
		return message
	}

	var b strings.Builder
	if strings.TrimSpace(message) != "" {
		b.WriteString(message)
		b.WriteString("\n\n")
	}
	b.WriteString("The user attached the following local file(s):")
	for _, attachment := range attachments {
		b.WriteString("\n- ")
		if attachment.Name != "" {
			b.WriteString(attachment.Name)
			b.WriteString(": ")
		}
		b.WriteString(attachment.Path)
		if attachment.MIMEType != "" {
			b.WriteString(" (")
			b.WriteString(attachment.MIMEType)
			b.WriteString(")")
		}
	}
	b.WriteString("\nPlease inspect and process the attachment(s) as needed.")
	return b.String()
}
