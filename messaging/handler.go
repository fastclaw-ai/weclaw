package messaging

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/fastclaw-ai/weclaw/agent"
	"github.com/fastclaw-ai/weclaw/ilink"
	"github.com/google/uuid"
)

// AgentFactory creates an agent by config name. Returns nil if the name is unknown.
type AgentFactory func(ctx context.Context, name string) agent.Agent

// SaveDefaultFunc persists the default agent name to config file.
type SaveDefaultFunc func(name string) error

// AgentMeta holds static config info about an agent (for /status display).
type AgentMeta struct {
	Name    string
	Type    string // "acp", "cli", "http"
	Command string // binary path or endpoint
	Model   string
}

// Handler processes incoming WeChat messages and dispatches replies.
type Handler struct {
	mu            sync.RWMutex
	defaultName   string
	agents        map[string]agent.Agent // name -> running agent
	agentMetas    []AgentMeta            // all configured agents (for /status)
	agentWorkDirs map[string]string      // agent name -> configured/runtime cwd
	customAliases map[string]string      // custom alias -> agent name (from config)
	factory       AgentFactory
	saveDefault   SaveDefaultFunc
	contextTokens sync.Map   // map[userID]contextToken
	saveDir       string     // directory to save images/files to
	seenMsgs      sync.Map   // map[int64]time.Time — dedup by message_id

	shellModeStates sync.Map // map[userID]*shellModeState — per-user shell mode state
}

// shellModeState holds per-user shell mode state.
type shellModeState struct {
	enabled bool   // whether shell mode is active
	cwd     string // current working directory
	baseDir string // base directory for path sandboxing (empty = no restriction)
}

// NewHandler creates a new message handler.
func NewHandler(factory AgentFactory, saveDefault SaveDefaultFunc) *Handler {
	return &Handler{
		agents:        make(map[string]agent.Agent),
		agentWorkDirs: make(map[string]string),
		factory:       factory,
		saveDefault:   saveDefault,
	}
}

// SetSaveDir sets the directory for saving images and files.
func (h *Handler) SetSaveDir(dir string) {
	h.saveDir = dir
}

// cleanSeenMsgs removes entries older than 5 minutes from the dedup cache.
func (h *Handler) cleanSeenMsgs() {
	cutoff := time.Now().Add(-5 * time.Minute)
	h.seenMsgs.Range(func(key, value any) bool {
		if t, ok := value.(time.Time); ok && t.Before(cutoff) {
			h.seenMsgs.Delete(key)
		}
		return true
	})
}

// SetCustomAliases sets custom alias mappings from config.
func (h *Handler) SetCustomAliases(aliases map[string]string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.customAliases = aliases
}

// SetAgentMetas sets the list of all configured agents (for /status).
func (h *Handler) SetAgentMetas(metas []AgentMeta) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.agentMetas = metas
}

// SetAgentWorkDirs sets the configured working directory for each agent.
func (h *Handler) SetAgentWorkDirs(workDirs map[string]string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.agentWorkDirs = make(map[string]string, len(workDirs))
	for name, dir := range workDirs {
		h.agentWorkDirs[name] = dir
	}
}

// SetDefaultAgent sets the default agent (already started).
func (h *Handler) SetDefaultAgent(name string, ag agent.Agent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.defaultName = name
	h.agents[name] = ag
	log.Printf("[handler] default agent ready: %s (%s)", name, ag.Info())
}

// getAgent returns a running agent by name, or starts it on demand via factory.
func (h *Handler) getAgent(ctx context.Context, name string) (agent.Agent, error) {
	// Fast path: already running
	h.mu.RLock()
	ag, ok := h.agents[name]
	h.mu.RUnlock()
	if ok {
		return ag, nil
	}

	// Slow path: create on demand
	if h.factory == nil {
		return nil, fmt.Errorf("agent %q not found and no factory configured", name)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Double-check after acquiring write lock
	if ag, ok := h.agents[name]; ok {
		return ag, nil
	}

	log.Printf("[handler] starting agent %q on demand...", name)
	ag = h.factory(ctx, name)
	if ag == nil {
		return nil, fmt.Errorf("agent %q not available", name)
	}

	h.agents[name] = ag
	log.Printf("[handler] agent started on demand: %s (%s)", name, ag.Info())
	return ag, nil
}

// getDefaultAgent returns the default agent (may be nil if not ready yet).
func (h *Handler) getDefaultAgent() agent.Agent {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.defaultName == "" {
		return nil
	}
	return h.agents[h.defaultName]
}

// isKnownAgent checks if a name corresponds to a configured agent.
func (h *Handler) isKnownAgent(name string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	// Check running agents
	if _, ok := h.agents[name]; ok {
		return true
	}
	// Check configured agents (metas)
	for _, meta := range h.agentMetas {
		if meta.Name == name {
			return true
		}
	}
	return false
}

// agentAliases maps short aliases to agent config names.
var agentAliases = map[string]string{
	"cc":  "claude",
	"cx":  "codex",
	"oc":  "openclaw",
	"cs":  "cursor",
	"km":  "kimi",
	"gm":  "gemini",
	"ocd": "opencode",
	"pi":  "pi",
	"cp":  "copilot",
	"dr":  "droid",
	"if":  "iflow",
	"kr":  "kiro",
	"qw":  "qwen",
}

// resolveAlias returns the full agent name for an alias, or the original name if no alias matches.
// Checks custom aliases (from config) first, then built-in aliases.
func (h *Handler) resolveAlias(name string) string {
	h.mu.RLock()
	custom := h.customAliases
	h.mu.RUnlock()
	if custom != nil {
		if full, ok := custom[name]; ok {
			return full
		}
	}
	if full, ok := agentAliases[name]; ok {
		return full
	}
	return name
}

// parseCommand checks if text starts with "/" or "@" followed by agent name(s).
// Supports multiple agents: "@cc @cx hello" returns (["claude","codex"], "hello").
// Returns (agentNames, actualMessage). Aliases are resolved automatically.
// If no command prefix, returns (nil, originalText).
func (h *Handler) parseCommand(text string) ([]string, string) {
	if !strings.HasPrefix(text, "/") && !strings.HasPrefix(text, "@") {
		return nil, text
	}

	// Parse consecutive @name or /name tokens from the start
	var names []string
	rest := text
	for {
		rest = strings.TrimSpace(rest)
		if !strings.HasPrefix(rest, "/") && !strings.HasPrefix(rest, "@") {
			break
		}

		// Strip prefix
		after := rest[1:]
		idx := strings.IndexAny(after, " /@")
		var token string
		if idx < 0 {
			// Rest is just the name, no message
			token = after
			rest = ""
		} else if after[idx] == '/' || after[idx] == '@' {
			// Next token is another @name or /name
			token = after[:idx]
			rest = after[idx:]
		} else {
			// Space — name ends here
			token = after[:idx]
			rest = strings.TrimSpace(after[idx+1:])
		}

		if token != "" {
			names = append(names, h.resolveAlias(token))
		}

		if rest == "" {
			break
		}
	}

	// Deduplicate names preserving order
	seen := make(map[string]bool)
	unique := names[:0]
	for _, n := range names {
		if !seen[n] {
			seen[n] = true
			unique = append(unique, n)
		}
	}

	return unique, rest
}

// HandleMessage processes a single incoming message.
func (h *Handler) HandleMessage(ctx context.Context, client *ilink.Client, msg ilink.WeixinMessage) {
	// Only process user messages that are finished
	if msg.MessageType != ilink.MessageTypeUser {
		return
	}
	if msg.MessageState != ilink.MessageStateFinish {
		return
	}

	// Deduplicate by message_id to avoid processing the same message multiple times
	// (voice messages may trigger multiple finish-state updates)
	if msg.MessageID != 0 {
		if _, loaded := h.seenMsgs.LoadOrStore(msg.MessageID, time.Now()); loaded {
			return
		}
		// Clean up old entries periodically (fire-and-forget)
		go h.cleanSeenMsgs()
	}

	// Extract text from item list (text message or voice transcription)
	text := extractText(msg)
	if text == "" {
		if voiceText := extractVoiceText(msg); voiceText != "" {
			text = voiceText
			log.Printf("[handler] voice transcription from %s: %q", msg.FromUserID, truncate(text, 80))
		}
	}
	if text == "" {
		// Check for image message
		if img := extractImage(msg); img != nil && h.saveDir != "" {
			h.handleImageSave(ctx, client, msg, img)
			return
		}
		log.Printf("[handler] received non-text message from %s, skipping", msg.FromUserID)
		return
	}

	log.Printf("[handler] received from %s: %q", msg.FromUserID, truncate(text, 80))

	// Store context token for this user
	h.contextTokens.Store(msg.FromUserID, msg.ContextToken)

	// Generate a clientID for this reply (used to correlate typing → finish)
	clientID := NewClientID()

	// Check if user is in shell mode
	trimmedForShell := strings.TrimSpace(text)
	if state, ok := h.shellModeStates.Load(msg.FromUserID); ok && state != nil {
		sm := state.(*shellModeState)
		if sm.enabled {
			// Exit shell mode
			if trimmedForShell == "/q" {
				sm.enabled = false
				reply := "已退出命令行模式"
				if err := SendTextReply(ctx, client, msg.FromUserID, reply, msg.ContextToken, clientID); err != nil {
					log.Printf("[handler] failed to send reply to %s: %v", msg.FromUserID, err)
				}
				return
			}
			// Execute command in shell mode
			reply := h.handleShellWithState(ctx, sm, trimmedForShell)
			if reply != "" {
				if err := SendTextReply(ctx, client, msg.FromUserID, reply, msg.ContextToken, clientID); err != nil {
					log.Printf("[handler] failed to send reply to %s: %v", msg.FromUserID, err)
				}
			}
			return
		}
	}

	// Intercept URLs: save to Linkhoard directly without AI agent
	trimmed := strings.TrimSpace(text)
	if h.saveDir != "" && IsURL(trimmed) {
		rawURL := ExtractURL(trimmed)
		if rawURL != "" {
			log.Printf("[handler] saving URL to linkhoard: %s", rawURL)
			title, err := SaveLinkToLinkhoard(ctx, h.saveDir, rawURL)
			var reply string
			if err != nil {
				log.Printf("[handler] link save failed: %v", err)
				reply = fmt.Sprintf("保存失败: %v", err)
			} else {
				reply = fmt.Sprintf("已保存: %s", title)
			}
			if err := SendTextReply(ctx, client, msg.FromUserID, reply, msg.ContextToken, clientID); err != nil {
				log.Printf("[handler] failed to send reply to %s: %v", msg.FromUserID, err)
			}
			return
		}
	}

	// Built-in commands (no typing needed)
	if trimmed == "/info" {
		reply := h.buildStatus()
		if err := SendTextReply(ctx, client, msg.FromUserID, reply, msg.ContextToken, clientID); err != nil {
			log.Printf("[handler] failed to send reply to %s: %v", msg.FromUserID, err)
		}
		return
	} else if trimmed == "/help" {
		reply := buildHelpText()
		if err := SendTextReply(ctx, client, msg.FromUserID, reply, msg.ContextToken, clientID); err != nil {
			log.Printf("[handler] failed to send reply to %s: %v", msg.FromUserID, err)
		}
		return
	} else if trimmed == "/new" || trimmed == "/clear" {
		reply := h.resetDefaultSession(ctx, msg.FromUserID)
		if err := SendTextReply(ctx, client, msg.FromUserID, reply, msg.ContextToken, clientID); err != nil {
			log.Printf("[handler] failed to send reply to %s: %v", msg.FromUserID, err)
		}
		return
	} else if strings.HasPrefix(trimmed, "/cwd") {
		reply := h.handleCwd(trimmed)
		if err := SendTextReply(ctx, client, msg.FromUserID, reply, msg.ContextToken, clientID); err != nil {
			log.Printf("[handler] failed to send reply to %s: %v", msg.FromUserID, err)
		}
		return
	} else if trimmed == "/sh" || trimmed == "/$" {
		// Enter shell mode
		reply := h.enterShellMode(ctx, msg.FromUserID)
		if err := SendTextReply(ctx, client, msg.FromUserID, reply, msg.ContextToken, clientID); err != nil {
			log.Printf("[handler] failed to send reply to %s: %v", msg.FromUserID, err)
		}
		return
	} else if strings.HasPrefix(trimmed, "/sh ") || strings.HasPrefix(trimmed, "/$ ") {
		// Execute single command without entering shell mode
		reply := h.handleShell(ctx, trimmed)
		if reply != "" {
			if err := SendTextReply(ctx, client, msg.FromUserID, reply, msg.ContextToken, clientID); err != nil {
				log.Printf("[handler] failed to send reply to %s: %v", msg.FromUserID, err)
			}
		}
		return
	}

	// Route: "/agentname message" or "@agent1 @agent2 message" -> specific agent(s)
	agentNames, message := h.parseCommand(text)

	// No command prefix -> send to default agent
	if len(agentNames) == 0 {
		h.sendToDefaultAgent(ctx, client, msg, text, clientID)
		return
	}

	// No message -> switch default agent (only first name)
	if message == "" {
		if len(agentNames) == 1 && h.isKnownAgent(agentNames[0]) {
			reply := h.switchDefault(ctx, agentNames[0])
			if err := SendTextReply(ctx, client, msg.FromUserID, reply, msg.ContextToken, clientID); err != nil {
				log.Printf("[handler] failed to send reply to %s: %v", msg.FromUserID, err)
			}
		} else if len(agentNames) == 1 && !h.isKnownAgent(agentNames[0]) {
			// Unknown agent -> forward to default
			h.sendToDefaultAgent(ctx, client, msg, text, clientID)
		} else {
			reply := "Usage: specify one agent to switch, or add a message to broadcast"
			if err := SendTextReply(ctx, client, msg.FromUserID, reply, msg.ContextToken, clientID); err != nil {
				log.Printf("[handler] failed to send reply to %s: %v", msg.FromUserID, err)
			}
		}
		return
	}

	// Filter to known agents; if single unknown agent -> forward to default
	var knownNames []string
	for _, name := range agentNames {
		if h.isKnownAgent(name) {
			knownNames = append(knownNames, name)
		}
	}
	if len(knownNames) == 0 {
		// No known agents -> forward entire text to default agent
		h.sendToDefaultAgent(ctx, client, msg, text, clientID)
		return
	}

	// Send typing indicator
	go func() {
		if typingErr := SendTypingState(ctx, client, msg.FromUserID, msg.ContextToken); typingErr != nil {
			log.Printf("[handler] failed to send typing state: %v", typingErr)
		}
	}()

	if len(knownNames) == 1 {
		// Single agent
		h.sendToNamedAgent(ctx, client, msg, knownNames[0], message, clientID)
	} else {
		// Multi-agent broadcast: parallel dispatch, send replies as they arrive
		h.broadcastToAgents(ctx, client, msg, knownNames, message)
	}
}

// sendToDefaultAgent sends the message to the default agent and replies.
func (h *Handler) sendToDefaultAgent(ctx context.Context, client *ilink.Client, msg ilink.WeixinMessage, text, clientID string) {
	go func() {
		if typingErr := SendTypingState(ctx, client, msg.FromUserID, msg.ContextToken); typingErr != nil {
			log.Printf("[handler] failed to send typing state: %v", typingErr)
		}
	}()

	h.mu.RLock()
	defaultName := h.defaultName
	h.mu.RUnlock()

	ag := h.getDefaultAgent()
	var reply string
	if ag != nil {
		var err error
		reply, err = h.chatWithAgent(ctx, ag, msg.FromUserID, text)
		if err != nil {
			reply = fmt.Sprintf("Error: %v", err)
		}
	} else {
		log.Printf("[handler] agent not ready, using echo mode for %s", msg.FromUserID)
		reply = "[echo] " + text
	}

	h.sendReplyWithMedia(ctx, client, msg, defaultName, reply, clientID)
}

// sendToNamedAgent sends the message to a specific agent and replies.
func (h *Handler) sendToNamedAgent(ctx context.Context, client *ilink.Client, msg ilink.WeixinMessage, name, message, clientID string) {
	ag, agErr := h.getAgent(ctx, name)
	if agErr != nil {
		log.Printf("[handler] agent %q not available: %v", name, agErr)
		reply := fmt.Sprintf("Agent %q is not available: %v", name, agErr)
		SendTextReply(ctx, client, msg.FromUserID, reply, msg.ContextToken, clientID)
		return
	}

	reply, err := h.chatWithAgent(ctx, ag, msg.FromUserID, message)
	if err != nil {
		reply = fmt.Sprintf("Error: %v", err)
	}
	h.sendReplyWithMedia(ctx, client, msg, name, reply, clientID)
}

// broadcastToAgents sends the message to multiple agents in parallel.
// Each reply is sent as a separate message with the agent name prefix.
func (h *Handler) broadcastToAgents(ctx context.Context, client *ilink.Client, msg ilink.WeixinMessage, names []string, message string) {
	type result struct {
		name  string
		reply string
	}

	ch := make(chan result, len(names))

	for _, name := range names {
		go func(n string) {
			ag, err := h.getAgent(ctx, n)
			if err != nil {
				ch <- result{name: n, reply: fmt.Sprintf("Error: %v", err)}
				return
			}
			reply, err := h.chatWithAgent(ctx, ag, msg.FromUserID, message)
			if err != nil {
				ch <- result{name: n, reply: fmt.Sprintf("Error: %v", err)}
				return
			}
			ch <- result{name: n, reply: reply}
		}(name)
	}

	// Send replies as they arrive
	for range names {
		r := <-ch
		reply := fmt.Sprintf("[%s] %s", r.name, r.reply)
		clientID := NewClientID()
		h.sendReplyWithMedia(ctx, client, msg, r.name, reply, clientID)
	}
}

// sendReplyWithMedia sends a text reply and any extracted image URLs.
func (h *Handler) sendReplyWithMedia(ctx context.Context, client *ilink.Client, msg ilink.WeixinMessage, agentName, reply, clientID string) {
	imageURLs := ExtractImageURLs(reply)
	attachmentPaths := extractLocalAttachmentPaths(reply)
	allowedRoots := h.allowedAttachmentRoots(agentName)

	var sentPaths []string
	var failedPaths []string
	for _, attachmentPath := range attachmentPaths {
		if !isAllowedAttachmentPath(attachmentPath, allowedRoots) {
			log.Printf("[handler] rejected attachment outside allowed roots for agent %q: %s", agentName, attachmentPath)
			failedPaths = append(failedPaths, attachmentPath)
			continue
		}
		if err := SendMediaFromPath(ctx, client, msg.FromUserID, attachmentPath, msg.ContextToken); err != nil {
			log.Printf("[handler] failed to send attachment to %s: %v", msg.FromUserID, err)
			failedPaths = append(failedPaths, attachmentPath)
			continue
		}
		sentPaths = append(sentPaths, attachmentPath)
	}

	reply = rewriteReplyWithAttachmentResults(reply, sentPaths, failedPaths)

	if err := SendTextReply(ctx, client, msg.FromUserID, reply, msg.ContextToken, clientID); err != nil {
		log.Printf("[handler] failed to send reply to %s: %v", msg.FromUserID, err)
	}

	for _, imgURL := range imageURLs {
		if err := SendMediaFromURL(ctx, client, msg.FromUserID, imgURL, msg.ContextToken); err != nil {
			log.Printf("[handler] failed to send image to %s: %v", msg.FromUserID, err)
		}
	}
}

func (h *Handler) allowedAttachmentRoots(agentName string) []string {
	roots := []string{defaultAttachmentWorkspace()}

	h.mu.RLock()
	agentDir := h.agentWorkDirs[agentName]
	h.mu.RUnlock()

	if agentDir != "" {
		roots = append(roots, agentDir)
	}

	return roots
}

// chatWithAgent sends a message to an agent and returns the reply, with logging.
func (h *Handler) chatWithAgent(ctx context.Context, ag agent.Agent, userID, message string) (string, error) {
	info := ag.Info()
	log.Printf("[handler] dispatching to agent (%s) for %s", info, userID)

	start := time.Now()
	reply, err := ag.Chat(ctx, userID, message)
	elapsed := time.Since(start)

	if err != nil {
		log.Printf("[handler] agent error (%s, elapsed=%s): %v", info, elapsed, err)
		return "", err
	}

	log.Printf("[handler] agent replied (%s, elapsed=%s): %q", info, elapsed, truncate(reply, 100))
	return reply, nil
}

// switchDefault switches the default agent. Starts it on demand if needed.
// The change is persisted to config file.
func (h *Handler) switchDefault(ctx context.Context, name string) string {
	ag, err := h.getAgent(ctx, name)
	if err != nil {
		log.Printf("[handler] failed to switch default to %q: %v", name, err)
		return fmt.Sprintf("Failed to switch to %q: %v", name, err)
	}

	h.mu.Lock()
	old := h.defaultName
	h.defaultName = name
	h.agents[name] = ag
	h.mu.Unlock()

	// Persist to config file
	if h.saveDefault != nil {
		if err := h.saveDefault(name); err != nil {
			log.Printf("[handler] failed to save default agent to config: %v", err)
		} else {
			log.Printf("[handler] saved default agent %q to config", name)
		}
	}

	info := ag.Info()
	log.Printf("[handler] switched default agent: %s -> %s (%s)", old, name, info)
	return fmt.Sprintf("switch to %s", name)
}

// resetDefaultSession resets the session for the given userID on the default agent.
func (h *Handler) resetDefaultSession(ctx context.Context, userID string) string {
	ag := h.getDefaultAgent()
	if ag == nil {
		return "No agent running."
	}
	name := ag.Info().Name
	sessionID, err := ag.ResetSession(ctx, userID)
	if err != nil {
		log.Printf("[handler] reset session failed for %s: %v", userID, err)
		return fmt.Sprintf("Failed to reset session: %v", err)
	}
	if sessionID != "" {
		return fmt.Sprintf("已创建新的%s会话\n%s", name, sessionID)
	}
	return fmt.Sprintf("已创建新的%s会话", name)
}

// handleCwd handles the /cwd command. It updates the working directory for all running agents.
func (h *Handler) handleCwd(trimmed string) string {
	arg := strings.TrimSpace(strings.TrimPrefix(trimmed, "/cwd"))
	if arg == "" {
		// No path provided — show current cwd of default agent
		ag := h.getDefaultAgent()
		if ag == nil {
			return "No agent running."
		}
		info := ag.Info()
		return fmt.Sprintf("cwd: (check agent config)\nagent: %s", info.Name)
	}

	// Expand ~ to home directory
	if arg == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			arg = home
		}
	} else if strings.HasPrefix(arg, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			arg = filepath.Join(home, arg[2:])
		}
	}

	// Resolve to absolute path
	absPath, err := filepath.Abs(arg)
	if err != nil {
		return fmt.Sprintf("Invalid path: %v", err)
	}

	// Verify directory exists
	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Sprintf("Path not found: %s", absPath)
	}
	if !info.IsDir() {
		return fmt.Sprintf("Not a directory: %s", absPath)
	}

	// Update cwd on all running agents
	h.mu.RLock()
	agents := make(map[string]agent.Agent, len(h.agents))
	for name, ag := range h.agents {
		agents[name] = ag
	}
	h.mu.RUnlock()

	for name, ag := range agents {
		ag.SetCwd(absPath)
		log.Printf("[handler] updated cwd for agent %s: %s", name, absPath)
	}

	h.mu.Lock()
	for name := range agents {
		h.agentWorkDirs[name] = absPath
	}
	h.mu.Unlock()

	return fmt.Sprintf("cwd: %s", absPath)
}

// buildStatus returns a short status string showing the current default agent.
func (h *Handler) buildStatus() string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.defaultName == "" {
		return "agent: none (echo mode)"
	}

	ag, ok := h.agents[h.defaultName]
	if !ok {
		return fmt.Sprintf("agent: %s (not started)", h.defaultName)
	}

	info := ag.Info()
	return fmt.Sprintf("agent: %s\ntype: %s\nmodel: %s", h.defaultName, info.Type, info.Model)
}

func buildHelpText() string {
	return `Available commands:
@agent or /agent - Switch default agent
@agent msg or /agent msg - Send to a specific agent
@a @b msg - Broadcast to multiple agents
/new or /clear - Start a new session
/cwd /path - Switch workspace directory
/info - Show current agent info
/help - Show this help message
/sh <cmd> - Execute a shell command (e.g. /sh ls -la)
/sh or /$ - Enter persistent shell mode (/q to exit)

Aliases: /cc(claude) /cx(codex) /cs(cursor) /km(kimi) /gm(gemini) /oc(openclaw) /ocd(opencode) /pi(pi) /cp(copilot) /dr(droid) /if(iflow) /kr(kiro) /qw(qwen)`
}

func extractText(msg ilink.WeixinMessage) string {
	for _, item := range msg.ItemList {
		if item.Type == ilink.ItemTypeText && item.TextItem != nil {
			return item.TextItem.Text
		}
	}
	return ""
}

func extractImage(msg ilink.WeixinMessage) *ilink.ImageItem {
	for _, item := range msg.ItemList {
		if item.Type == ilink.ItemTypeImage && item.ImageItem != nil {
			return item.ImageItem
		}
	}
	return nil
}

func extractVoiceText(msg ilink.WeixinMessage) string {
	for _, item := range msg.ItemList {
		if item.Type == ilink.ItemTypeVoice && item.VoiceItem != nil && item.VoiceItem.Text != "" {
			return item.VoiceItem.Text
		}
	}
	return ""
}

func (h *Handler) handleImageSave(ctx context.Context, client *ilink.Client, msg ilink.WeixinMessage, img *ilink.ImageItem) {
	clientID := NewClientID()
	log.Printf("[handler] received image from %s, saving to %s", msg.FromUserID, h.saveDir)

	// Download image data
	var data []byte
	var err error

	if img.URL != "" {
		// Direct URL download
		data, _, err = downloadFile(ctx, img.URL)
	} else if img.Media != nil && img.Media.EncryptQueryParam != "" {
		// CDN encrypted download
		data, err = DownloadFileFromCDN(ctx, img.Media.EncryptQueryParam, img.Media.AESKey)
	} else {
		log.Printf("[handler] image has no URL or media info from %s", msg.FromUserID)
		return
	}

	if err != nil {
		log.Printf("[handler] failed to download image from %s: %v", msg.FromUserID, err)
		reply := fmt.Sprintf("Failed to save image: %v", err)
		_ = SendTextReply(ctx, client, msg.FromUserID, reply, msg.ContextToken, clientID)
		return
	}

	// Detect extension from content
	ext := detectImageExt(data)

	// Generate filename with timestamp
	ts := time.Now().Format("20060102-150405")
	fileName := fmt.Sprintf("%s%s", ts, ext)
	filePath := filepath.Join(h.saveDir, fileName)

	// Ensure save directory exists
	if err := os.MkdirAll(h.saveDir, 0o755); err != nil {
		log.Printf("[handler] failed to create save dir: %v", err)
		return
	}

	// Write image file
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		log.Printf("[handler] failed to write image: %v", err)
		reply := fmt.Sprintf("Failed to save image: %v", err)
		_ = SendTextReply(ctx, client, msg.FromUserID, reply, msg.ContextToken, clientID)
		return
	}

	// Write sidecar file
	sidecarPath := filePath + ".sidecar.md"
	sidecarContent := fmt.Sprintf("---\nid: %s\n---\n", uuid.New().String())
	if err := os.WriteFile(sidecarPath, []byte(sidecarContent), 0o644); err != nil {
		log.Printf("[handler] failed to write sidecar: %v", err)
	}

	log.Printf("[handler] saved image to %s (%d bytes)", filePath, len(data))
	reply := fmt.Sprintf("Saved: %s", fileName)
	if err := SendTextReply(ctx, client, msg.FromUserID, reply, msg.ContextToken, clientID); err != nil {
		log.Printf("[handler] failed to send reply to %s: %v", msg.FromUserID, err)
	}
}

func detectImageExt(data []byte) string {
	if len(data) < 4 {
		return ".bin"
	}
	// PNG: 89 50 4E 47
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return ".png"
	}
	// JPEG: FF D8 FF
	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return ".jpg"
	}
	// GIF: 47 49 46
	if data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 {
		return ".gif"
	}
	// WebP: 52 49 46 46 ... 57 45 42 50
	if len(data) >= 12 && data[0] == 0x52 && data[1] == 0x49 && data[8] == 0x57 && data[9] == 0x45 {
		return ".webp"
	}
	// BMP: 42 4D
	if data[0] == 0x42 && data[1] == 0x4D {
		return ".bmp"
	}
	return ".jpg" // default to jpg for WeChat images
}

// =============================================================================
// Shell Mode — /sh and /$ commands
// =============================================================================

// handleShell processes /sh or /$ command to execute a single shell command.
func (h *Handler) handleShell(ctx context.Context, trimmed string) string {
	// Extract command: "/sh ls -la" -> "ls -la" or "/$ ls -la" -> "ls -la"
	var cmdStr string
	if strings.HasPrefix(trimmed, "/sh ") {
		cmdStr = strings.TrimPrefix(trimmed, "/sh ")
	} else {
		cmdStr = strings.TrimPrefix(trimmed, "/$ ")
	}
	cmdStr = strings.TrimSpace(cmdStr)

	if cmdStr == "" {
		return "用法: /sh <命令> 或 /$ <命令>\n示例: /sh ls -la\n可用命令: ls, cat, pwd, find, grep, head, tail 等"
	}

	// === Shortcut aliases ===
	if cmdStr == "ll" {
		cmdStr = "ls -lh"
	} else if cmdStr == ".." {
		cmdStr = "cd .."
	} else if cmdStr == "..." {
		cmdStr = "cd ../.."
	}

	// === Security: Check for dangerous operators ===
	dangerousPatterns := []string{">", ">>", "<", "|", "&&", "||", ";", "`", "$("}
	for _, pattern := range dangerousPatterns {
		if strings.Contains(cmdStr, pattern) {
			return fmt.Sprintf("❌ 出于安全考虑，不允许使用特殊字符: %s\n如需复杂操作，请在本地终端执行", pattern)
		}
	}

	// === Command whitelist for security ===
	allowedCommands := map[string]bool{
		"ls": true, "pwd": true, "cd": true, "cat": true, "head": true, "tail": true,
		"grep": true, "find": true, "wc": true, "du": true, "df": true,
		"file": true, "stat": true, "date": true, "echo": true, "basename": true,
		"dirname": true, "realpath": true, "readlink": true, "which": true,
		"tree": true, "nl": true, "sort": true, "uniq": true, "cut": true,
		"awk": true, "sed": true, "tr": true, "xargs": true,
	}

	// Extract the base command
	parts := strings.Fields(cmdStr)
	if len(parts) > 0 {
		baseCmd := parts[0]
		if !allowedCommands[baseCmd] {
			return fmt.Sprintf("❌ 命令不在白名单中: %s\n允许的命令: ls pwd cd cat head tail grep find wc du df file stat date echo basename dirname realpath readlink which tree nl sort uniq cut awk sed tr xargs\n快捷指令: ll(=ls -lh) ..(=cd ..) ...(=cd ../..)", baseCmd)
		}
	}

	// === Auto-add flags to ls for better output ===
	if strings.HasPrefix(cmdStr, "ls") {
		if !strings.Contains(cmdStr, "-C") && !strings.Contains(cmdStr, "-l") && !strings.Contains(cmdStr, "-1") {
			if cmdStr == "ls" {
				cmdStr = "ls -C"
				if isLinux() {
					cmdStr += " --group-directories-first"
				}
			} else {
				rest := strings.TrimPrefix(cmdStr, "ls")
				cmdStr = "ls -C"
				if isLinux() {
					cmdStr += " --group-directories-first"
				}
				cmdStr += rest
			}
		} else if strings.Contains(cmdStr, "-C") && isLinux() && !strings.Contains(cmdStr, "--group-directories-first") {
			cmdStr += " --group-directories-first"
		}
	}

	// === Execute command ===
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		output := stderr.String()
		if output == "" {
			output = stdout.String()
		}
		if len(output) > 3000 {
			output = output[:3000] + "\n... (输出已截断)"
		}
		return fmt.Sprintf("❌ 命令执行失败:\n%s", output)
	}

	output := stdout.String()
	if stderr.String() != "" {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	// Clean ANSI escape codes for WeChat
	output = cleanANSI(output)

	if len(output) > 4000 {
		output = output[:4000] + "\n... (输出已截断)"
	}

	if output == "" {
		return "✅ 命令执行成功，无输出"
	}

	cwd, _ := os.Getwd()
	return formatShellOutput(cwd, output)
}

// enterShellMode enters shell mode for the user.
func (h *Handler) enterShellMode(ctx context.Context, userID string) string {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "/"
	}

	state := &shellModeState{
		enabled: true,
		cwd:     cwd,
		baseDir: "",
	}
	h.shellModeStates.Store(userID, state)

	prompt := shellPrompt(cwd)
	return fmt.Sprintf("✅ 已进入命令行模式\n%s\n\n提示: 直接输入命令即可，无需 /sh 前缀\n输入 /q 退出命令行模式", prompt)
}

// handleShellWithState executes a command in shell mode with persistent state.
func (h *Handler) handleShellWithState(ctx context.Context, state *shellModeState, cmdStr string) string {
	if cmdStr == "" {
		return ""
	}

	// === Shortcut aliases ===
	if cmdStr == "ll" {
		cmdStr = "ls -lh"
	} else if cmdStr == ".." {
		cmdStr = "cd .."
	} else if cmdStr == "..." {
		cmdStr = "cd ../.."
	}

	// === Security: Check for dangerous operators ===
	dangerousPatterns := []string{">", ">>", "<", "|", "&&", "||", ";", "`", "$("}
	for _, pattern := range dangerousPatterns {
		if strings.Contains(cmdStr, pattern) {
			return fmt.Sprintf("❌ 出于安全考虑，不允许使用特殊字符: %s", pattern)
		}
	}

	// === Command whitelist for security ===
	allowedCommands := map[string]bool{
		"ls": true, "pwd": true, "cd": true, "cat": true, "head": true, "tail": true,
		"grep": true, "find": true, "wc": true, "du": true, "df": true,
		"file": true, "stat": true, "date": true, "echo": true, "basename": true,
		"dirname": true, "realpath": true, "readlink": true, "which": true,
		"tree": true, "nl": true, "sort": true, "uniq": true, "cut": true,
		"awk": true, "sed": true, "tr": true, "xargs": true,
	}

	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return ""
	}

	baseCmd := parts[0]
	if !allowedCommands[baseCmd] {
		return fmt.Sprintf("❌ 命令不在白名单中: %s\n允许的命令: ls pwd cd cat head tail grep find wc du df file stat date echo basename dirname realpath readlink which tree nl sort uniq cut awk sed tr xargs", baseCmd)
	}

	// === Handle cd specially (update state.cwd) ===
	if baseCmd == "cd" {
		var targetDir string
		if len(parts) < 2 {
			home, err := os.UserHomeDir()
			if err != nil {
				return "❌ 无法获取 home 目录"
			}
			targetDir = home
		} else {
			targetDir = parts[1]
		}

		if !filepath.IsAbs(targetDir) {
			targetDir = filepath.Join(state.cwd, targetDir)
		}

		targetDir = filepath.Clean(targetDir)

		// === Symlink escape protection ===
		resolved, err := filepath.EvalSymlinks(targetDir)
		if err != nil {
			return fmt.Sprintf("❌ 目录不存在: %s", targetDir)
		}
		targetDir = resolved

		// === Sandbox check ===
		if state.baseDir != "" {
			if !strings.HasPrefix(targetDir, state.baseDir) {
				return fmt.Sprintf("❌ 不允许访问沙盒目录之外: %s", targetDir)
			}
		}

		info, err := os.Stat(targetDir)
		if err != nil {
			return fmt.Sprintf("❌ 目录不存在: %s", targetDir)
		}
		if !info.IsDir() {
			return fmt.Sprintf("❌ 不是目录: %s", targetDir)
		}

		state.cwd = targetDir

		// Auto-ls after cd
		lsCmd := "ls -C"
		if isLinux() {
			lsCmd += " --group-directories-first"
		}
		lscmd := exec.CommandContext(ctx, "sh", "-c", lsCmd)
		lscmd.Dir = state.cwd
		var lsOut bytes.Buffer
		lscmd.Stdout = &lsOut
		_ = lscmd.Run()
		lsOutput := cleanANSI(lsOut.String())

		prompt := shellPrompt(state.cwd)
		if lsOutput == "" {
			return fmt.Sprintf("%s\n(空目录)", prompt)
		}
		lsOutput = strings.TrimRight(lsOutput, "\n")
		return fmt.Sprintf("%s\n```\n%s\n```", prompt, lsOutput)
	}

	// === Auto-add flags to ls ===
	if strings.HasPrefix(cmdStr, "ls") {
		if !strings.Contains(cmdStr, "-C") && !strings.Contains(cmdStr, "-l") && !strings.Contains(cmdStr, "-1") {
			if cmdStr == "ls" {
				cmdStr = "ls -C"
				if isLinux() {
					cmdStr += " --group-directories-first"
				}
			} else {
				rest := strings.TrimPrefix(cmdStr, "ls")
				cmdStr = "ls -C"
				if isLinux() {
					cmdStr += " --group-directories-first"
				}
				cmdStr += rest
			}
		}
	}

	// === cat file size protection (50KB limit) ===
	if baseCmd == "cat" && len(parts) >= 2 {
		filePath := parts[1]
		if !filepath.IsAbs(filePath) {
			filePath = filepath.Join(state.cwd, filePath)
		}
		if info, err := os.Stat(filePath); err == nil {
			const maxSize = 50 * 1024
			if info.Size() > maxSize {
				return fmt.Sprintf("⚠️ 文件过大 (%.1f MB)\n💡 建议使用:\n   tail -n 100 %s  # 查看末尾\n   head -n 100 %s  # 查看开头\n   grep \"关键词\" %s  # 搜索内容",
					float64(info.Size())/(1024*1024), filepath.Base(filePath), filepath.Base(filePath), filepath.Base(filePath))
			}
		}
	}

	// === Execute command ===
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = state.cwd

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		output := stderr.String()
		if output == "" {
			output = stdout.String()
		}
		if len(output) > 3000 {
			output = output[:3000] + "\n... (输出已截断)"
		}
		return fmt.Sprintf("❌ 命令执行失败:\n%s", output)
	}

	output := stdout.String()
	if stderr.String() != "" {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	output = cleanANSI(output)

	if len(output) > 4000 {
		output = output[:4000] + "\n... (输出已截断)"
	}

	if output == "" {
		return "✅ 命令执行成功，无输出"
	}

	return formatShellOutput(state.cwd, output)
}

// cleanANSI removes ANSI escape codes from output.
func cleanANSI(s string) string {
	re := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	return re.ReplaceAllString(s, "")
}

// isLinux returns true if the OS is Linux.
func isLinux() bool {
	return runtime.GOOS == "linux"
}

// shellPrompt generates a shell prompt string for the given directory.
func shellPrompt(cwd string) string {
	return fmt.Sprintf("%s:#", cwd)
}

// formatShellOutput wraps output with shell prompt prefix and markdown code block.
func formatShellOutput(cwd string, output string) string {
	if output == "" {
		return ""
	}
	prompt := shellPrompt(cwd)
	output = strings.TrimRight(output, "\n")
	return fmt.Sprintf("%s\n```\n%s\n```", prompt, output)
}
