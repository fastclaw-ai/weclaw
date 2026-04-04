package messaging

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fastclaw-ai/weclaw/agent"
	"github.com/fastclaw-ai/weclaw/hub"
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
	hub           *hub.Hub   // shared context for cross-agent collaboration
	lastReplies   sync.Map   // map[userID]string — last agent reply per user (for /save without message)
}

// NewHandler creates a new message handler.
func NewHandler(factory AgentFactory, saveDefault SaveDefaultFunc) *Handler {
	return &Handler{
		agents:        make(map[string]agent.Agent),
		agentWorkDirs: make(map[string]string),
		factory:       factory,
		saveDefault:   saveDefault,
		hub:           hub.New(hub.DefaultDir()),
	}
}

// SetHub sets a custom Hub instance (for testing or custom paths).
func (h *Handler) SetHub(hu *hub.Hub) {
	h.hub = hu
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

// isBuiltinCommand returns true if the text starts with a built-in weclaw command.
// These should NOT be parsed as agent name prefixes.
func isBuiltinCommand(text string) bool {
	for _, cmd := range []string{"/help", "/info", "/new", "/clear", "/cwd", "/save", "/hub"} {
		if text == cmd || strings.HasPrefix(text, cmd+" ") {
			return true
		}
	}
	return false
}

// parseCommand checks if text starts with "/" or "@" followed by agent name(s).
// Supports multiple agents: "@cc @cx hello" returns (["claude","codex"], "hello").
// Returns (agentNames, actualMessage). Aliases are resolved automatically.
// If no command prefix, returns (nil, originalText).
// Built-in commands (/help, /save, /hub, etc.) are NOT parsed as agent names.
func (h *Handler) parseCommand(text string) ([]string, string) {
	if !strings.HasPrefix(text, "/") && !strings.HasPrefix(text, "@") {
		return nil, text
	}

	// Built-in commands should not be parsed as agent names
	if isBuiltinCommand(text) {
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

		// If this portion is a built-in command, stop parsing agent names
		if isBuiltinCommand(rest) {
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
	} else if strings.HasPrefix(trimmed, "/save") {
		// Pre-parse agent prefix so "@agent /save ..." works correctly.
		parsedAgentNames, effectiveTrimmed := h.parseCommand(text)
		saveTrimmed := trimmed
		if len(parsedAgentNames) > 0 && strings.HasPrefix(effectiveTrimmed, "/save") {
			saveTrimmed = "/save @" + parsedAgentNames[0] + " " + strings.TrimPrefix(effectiveTrimmed, "/save")
		}
		reply := h.handleSave(ctx, client, msg, strings.TrimSpace(saveTrimmed), clientID)
		if err := SendTextReply(ctx, client, msg.FromUserID, reply, msg.ContextToken, clientID); err != nil {
			log.Printf("[handler] failed to send reply to %s: %v", msg.FromUserID, err)
		}
		return
	} else if strings.HasPrefix(trimmed, "/hub") {
		// Pre-parse agent prefix so "@agent /hub ..." works correctly.
		parsedAgentNames, effectiveTrimmed := h.parseCommand(text)
		hubTrimmed := trimmed
		if len(parsedAgentNames) > 0 && strings.HasPrefix(effectiveTrimmed, "/hub") {
			hubTrimmed = "/hub @" + parsedAgentNames[0] + " " + strings.TrimPrefix(effectiveTrimmed, "/hub")
		}
		reply := h.handleHub(ctx, client, msg, strings.TrimSpace(hubTrimmed), clientID)
		if err := SendTextReply(ctx, client, msg.FromUserID, reply, msg.ContextToken, clientID); err != nil {
			log.Printf("[handler] failed to send reply to %s: %v", msg.FromUserID, err)
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
		// Cache last reply for /save without message
		h.lastReplies.Store(msg.FromUserID, reply)
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
	// Cache last reply for /save without message
	h.lastReplies.Store(msg.FromUserID, reply)
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

// hubReplyHint is prepended to /save messages to instruct the agent to return full content directly.
const hubReplyHint = "[系统指令] 你只需要直接回复文本内容。不要创建、写入或保存任何文件。不要请求授权。直接输出你的完整回复即可。\n\n"

// handleSave processes the /save command: sends message to agent, saves reply to hub.
// Usage: /save {filename} {message} — or just /save {filename} when replying to context
func (h *Handler) handleSave(ctx context.Context, client *ilink.Client, msg ilink.WeixinMessage, trimmed, clientID string) string {
	// Parse: /save filename [message]
	// Also handles: /save @agent filename message
	parts := strings.Fields(trimmed)
	if len(parts) < 2 {
		return "用法: /save 文件名 消息内容\n例: /save round1.md 分析AI未来"
	}

	// Check if next token is an agent reference (@name or /name)
	var agentName string
	var filenameIdx int

	if (strings.HasPrefix(parts[1], "@") || strings.HasPrefix(parts[1], "/")) && !strings.Contains(parts[1], ".") {
		// parts[1] looks like an agent reference, not a filename
		resolved := h.resolveAlias(parts[1][1:])
		if h.isKnownAgent(resolved) {
			agentName = resolved
			filenameIdx = 2
		} else {
			filenameIdx = 1
		}
	} else {
		filenameIdx = 1
	}

	if len(parts) < filenameIdx+1 {
		return "用法: /save 文件名 消息内容\n例: /save round1.md 分析AI未来"
	}

	filename := parts[filenameIdx]
	message := strings.Join(parts[filenameIdx+1:], " ")

	// No message content → save last agent reply directly
	if message == "" {
		lastReply, ok := h.lastReplies.Load(msg.FromUserID)
		if !ok {
			return "没有找到上一条回复。请先与 agent 对话，或使用 /save 文件名 消息内容。"
		}
		content := lastReply.(string)
		savePath, err := h.hub.Save(filename, content, "user")
		if err != nil {
			log.Printf("[handler] hub save failed: %v", err)
			return "⚠️ 保存到 Hub 失败: " + err.Error()
		}
		log.Printf("[handler] saved last reply to hub: %s", savePath)
		return fmt.Sprintf("✅ 已保存上一条回复到 Hub: %s", filename)
	}

	// Has message content → send to agent, save agent's reply
	// Determine which agent to use
	var ag agent.Agent
	var useName string
	if agentName != "" {
		var err error
		ag, err = h.getAgent(ctx, agentName)
		if err != nil {
			return fmt.Sprintf("Agent %q 不可用: %v", agentName, err)
		}
		useName = agentName
	} else {
		ag = h.getDefaultAgent()
		if ag == nil {
			return "没有可用的 agent"
		}
		h.mu.RLock()
		useName = h.defaultName
		h.mu.RUnlock()
	}

	// Send typing
	go func() {
		if typingErr := SendTypingState(ctx, client, msg.FromUserID, msg.ContextToken); typingErr != nil {
			log.Printf("[handler] failed to send typing state: %v", typingErr)
		}
	}()

	// Use agent-specific conversationID to avoid polluting default session
	conversationID := "hub:" + useName + ":" + msg.FromUserID

	// Prepend hint to discourage agents from requesting file creation
	promptWithHint := hubReplyHint + message

	reply, err := h.chatWithAgent(ctx, ag, conversationID, promptWithHint)
	if err != nil {
		return fmt.Sprintf("Agent 错误: %v", err)
	}

	savePath, err := h.hub.Save(filename, reply, useName)
	if err != nil {
		log.Printf("[handler] hub save failed: %v", err)
		return reply + "\n\n⚠️ 保存到 Hub 失败: " + err.Error()
	}
	log.Printf("[handler] saved hub reply to: %s (agent=%s)", savePath, useName)

	return reply + fmt.Sprintf("\n\n✅ 已保存到 Hub: %s", filename)
}

// handleHub processes the /hub command: reads shared context and optionally sends to agent.
// Usage:
//
//	/hub {message}              — read all shared files, inject context, send to default agent
//	/hub {filename} {msg}       — read specific file, inject, send to agent
//	/hub ls                     — list files in hub
//	/hub clear                  — clear all hub files
//	/hub pipe <agent> <msg>     — chain: send to default agent, pipe result to target agent
func (h *Handler) handleHub(ctx context.Context, client *ilink.Client, msg ilink.WeixinMessage, trimmed, clientID string) string {
	// Parse: /hub [filename] [message] | /hub ls | /hub clear
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "/hub"))

	// No arguments → list files
	if rest == "" {
		files, err := h.hub.List()
		if err != nil {
			return fmt.Sprintf("读取 Hub 失败: %v", err)
		}
		if len(files) == 0 {
			return "Hub 是空的。使用 /save 文件名 消息 来保存内容。"
		}
		var sb strings.Builder
		sb.WriteString("📁 Hub 文件列表:\n")
		for _, f := range files {
			sb.WriteString(fmt.Sprintf("  • %s\n", f))
		}
		return sb.String()
	}

	// Sub-commands
	switch {
	case rest == "ls" || rest == "list":
		files, err := h.hub.ListWithInfo()
		if err != nil {
			return fmt.Sprintf("读取 Hub 失败: %v", err)
		}
		if len(files) == 0 {
			return "Hub 是空的。"
		}
		var sb strings.Builder
		sb.WriteString("📁 Hub 文件列表 (最新优先):\n")
		for i, f := range files {
			timeStr := f.ModTime.Format("01-02 15:04")
			sb.WriteString(fmt.Sprintf("  [%d] %s (%s)\n", i+1, f.Name, timeStr))
		}
		sb.WriteString("\n💡 使用 /hub cat <编号> 读取文件")
		return sb.String()

	case strings.HasPrefix(rest, "cat "):
		// /hub cat <number>
		parts := strings.Fields(rest)
		if len(parts) != 2 {
			return "用法: /hub cat <编号>\n示例: /hub cat 1"
		}
		var num int
		_, err := fmt.Sscanf(parts[1], "%d", &num)
		if err != nil || num < 1 {
			return fmt.Sprintf("无效的编号: %q，请使用数字", parts[1])
		}
		files, err := h.hub.ListWithInfo()
		if err != nil {
			return fmt.Sprintf("读取 Hub 失败: %v", err)
		}
		if num > len(files) {
			return fmt.Sprintf("编号超出范围，Hub 只有 %d 个文件", len(files))
		}
		targetFile := files[num-1].Name
		content, err := h.hub.ReadFile(targetFile)
		if err != nil {
			return fmt.Sprintf("读取文件失败: %v", err)
		}
		return fmt.Sprintf("📄 %s\n\n%s", targetFile, content)

	case rest == "clear":
		count, err := h.hub.Clear()
		if err != nil {
			return fmt.Sprintf("清空 Hub 失败: %v", err)
		}
		return fmt.Sprintf("🗑️ 已清空 Hub (%d 个文件)", count)

	case strings.HasPrefix(rest, "pipe "):
		// /hub pipe <target_agent> <message>
		// /hub pipe <target_agent> @<编号> <message>
		// /hub pipe <target_agent> @-1 <message>
		// /hub pipe <target_agent> @<文件名> <消息>
		parts := strings.Fields(rest)
		if len(parts) < 2 {
			return "用法: /hub pipe <目标agent> <消息>\n       /hub pipe <目标agent> @<编号> <消息>\n       /hub pipe <目标agent> @-1 <消息>\n       /hub pipe <目标agent> @<文件名> <消息>\n\n示例: /hub pipe gemini 分析量子计算\n      /hub pipe claude @1 继续分析\n      /hub pipe claude @-1 补充说明"
		}
		targetAgent := parts[1]
		var message string
		if len(parts) >= 3 && strings.HasPrefix(parts[2], "@") {
			message = strings.Join(parts[2:], " ")
		} else {
			message = strings.Join(parts[2:], " ")
			if message == "" {
				return "用法: /hub pipe <目标agent> <消息>\n       /hub pipe <目标agent> @<编号> <消息>\n\n示例: /hub pipe gemini 分析量子计算"
			}
		}
		return h.handlePipe(ctx, client, msg, targetAgent, message, clientID)
	}

	// Parse: could be "/hub filename message" or "/hub message"
	words := strings.Fields(rest)
	if len(words) == 0 {
		return "用法: /hub {文件名} {消息} 或 /hub {消息}"
	}

	var hubContext string
	var message string
	var targetAgentName string
	var saveFilename string

	// Check if first word is an agent reference
	wordIdx := 0
	if (strings.HasPrefix(words[0], "@") || strings.HasPrefix(words[0], "/")) && !strings.Contains(words[0], ".") {
		resolved := h.resolveAlias(words[0][1:])
		if h.isKnownAgent(resolved) {
			targetAgentName = resolved
			wordIdx = 1
		}
	}

	if wordIdx >= len(words) {
		return "用法: /hub {文件名} {消息} 或 /hub {消息}"
	}

	// Check if current first word is a known hub file
	if h.hub.Exists(words[wordIdx]) {
		ctx2, err := h.hub.ReadSpecific([]string{words[wordIdx]})
		if err != nil {
			return fmt.Sprintf("读取文件失败: %v", err)
		}
		hubContext = ctx2
		if len(words) > wordIdx+1 {
			message = strings.Join(words[wordIdx+1:], " ")
			if strings.HasSuffix(words[wordIdx], ".md") {
				saveFilename = words[wordIdx]
			}
		} else {
			message = ""
		}
	} else {
		ctx2, err := h.hub.ReadAll()
		if err != nil {
			return fmt.Sprintf("读取 Hub 失败: %v", err)
		}
		hubContext = ctx2
		message = strings.Join(words[wordIdx:], " ")
	}

	if message == "" {
		if hubContext == "" {
			return "Hub 是空的，没有可注入的上下文。"
		}
		return hubContext
	}

	// Determine target agent
	var ag agent.Agent
	var resolvedAgentName string
	if targetAgentName != "" {
		var err error
		ag, err = h.getAgent(ctx, targetAgentName)
		if err != nil {
			return fmt.Sprintf("Agent %q 不可用: %v", targetAgentName, err)
		}
		resolvedAgentName = targetAgentName
	} else {
		ag = h.getDefaultAgent()
		if ag == nil {
			return "没有可用的 agent"
		}
		h.mu.RLock()
		resolvedAgentName = h.defaultName
		h.mu.RUnlock()
	}

	// Send typing
	go func() {
		if typingErr := SendTypingState(ctx, client, msg.FromUserID, msg.ContextToken); typingErr != nil {
			log.Printf("[handler] failed to send typing state: %v", typingErr)
		}
	}()

	// Always use agent-specific conversationID to avoid polluting default session
	conversationID := "hub:" + resolvedAgentName + ":" + msg.FromUserID

	wrappedMessage := fmt.Sprintf(
		"【重要】请直接基于下方提供的材料回答问题。禁止使用任何工具（搜索、读文件、写文件等），不要访问文件系统，不要搜索网络。材料已完整提供给你，直接分析即可。\n\n---\n共享材料：\n%s\n---\n\n问题：%s",
		hubContext, message,
	)

	reply, err := h.chatWithAgent(ctx, ag, conversationID, wrappedMessage)
	if err != nil {
		return fmt.Sprintf("Agent 错误: %v", err)
	}

	// Auto-save reply if a specific .md file was referenced
	if saveFilename != "" {
		savePath, err := h.hub.Save(saveFilename, reply, resolvedAgentName)
		if err != nil {
			log.Printf("[handler] hub auto-save failed: %v", err)
		} else {
			log.Printf("[handler] auto-saved hub reply to: %s (agent=%s)", savePath, resolvedAgentName)
			return reply + fmt.Sprintf("\n\n✅ 已保存到 Hub: %s", saveFilename)
		}
	}

	return reply
}

// handlePipe implements agent chaining: send message to default agent, save reply, send to target agent.
// Supports @ reference syntax:
//
//	/hub pipe <agent> @<number> <msg> — use hub file by number
//	/hub pipe <agent> @-1 <msg>       — use latest file
//	/hub pipe <agent> @<filename> <msg> — use file by name (partial match supported)
func (h *Handler) handlePipe(ctx context.Context, client *ilink.Client, msg ilink.WeixinMessage, targetAgent, message, clientID string) string {
	log.Printf("[hub/pipe] starting pipe: target=%s, message=%q", targetAgent, truncate(message, 50))

	timestamp := time.Now().Format("20060102-150405")

	var reply1 string
	var filename string
	var sourceAgentName string

	// Check for @ reference syntax
	trimmedMsg := strings.TrimSpace(message)
	if strings.HasPrefix(trimmedMsg, "@") {
		refStr := trimmedMsg[1:] // strip @

		// Try numeric reference (@1, @-1, @2)
		var refNum int
		n, err := fmt.Sscanf(refStr, "%d", &refNum)

		if n == 1 && err == nil {
			files, ferr := h.hub.ListWithInfo()
			if ferr != nil {
				return fmt.Sprintf("❌ 读取 Hub 失败: %v", ferr)
			}
			if len(files) == 0 {
				return "❌ Hub 是空的，没有可引用的文件"
			}

			var targetFile string
			if refNum < 0 {
				// Relative: @-1=latest, @-2=second latest
				idx := -refNum - 1
				if idx >= len(files) {
					return fmt.Sprintf("❌ 相对编号超出范围，Hub 只有 %d 个文件", len(files))
				}
				targetFile = files[idx].Name
				sourceAgentName = fmt.Sprintf("Hub[@%d=最新]", refNum)
			} else {
				// Absolute: @1=latest, @2=second
				if refNum > len(files) {
					return fmt.Sprintf("❌ 编号超出范围，Hub 只有 %d 个文件", len(files))
				}
				targetFile = files[refNum-1].Name
				sourceAgentName = fmt.Sprintf("Hub[@%d]", refNum)
			}

			content, cerr := h.hub.ReadFile(targetFile)
			if cerr != nil {
				return fmt.Sprintf("❌ 读取文件 %s 失败: %v", targetFile, cerr)
			}
			reply1 = content
			filename = targetFile
			log.Printf("[hub/pipe] using hub reference @%s -> file %s", refStr, targetFile)
		} else {
			// Try filename reference @filename.md or partial match
			refFilename := refStr
			if spaceIdx := strings.Index(refStr, " "); spaceIdx > 0 {
				refFilename = refStr[:spaceIdx]
				// Remaining text after @filename becomes part of message context
				message = strings.TrimSpace(refStr[spaceIdx:])
			} else {
				refFilename = refStr
				message = ""
			}

			// Try exact match first
			if h.hub.Exists(refFilename) {
				content, cerr := h.hub.ReadFile(refFilename)
				if cerr != nil {
					return fmt.Sprintf("❌ 读取文件 %s 失败: %v", refFilename, cerr)
				}
				reply1 = content
				filename = refFilename
				sourceAgentName = fmt.Sprintf("Hub[%s]", refFilename)
				log.Printf("[hub/pipe] using hub file reference @%s", refFilename)
			} else {
				// Try partial match
				matchedFile, err := h.hub.FindByPartialName(refFilename)
				if err == nil {
					content, cerr := h.hub.ReadFile(matchedFile)
					if cerr != nil {
						return fmt.Sprintf("❌ 读取文件 %s 失败: %v", matchedFile, cerr)
					}
					reply1 = content
					filename = matchedFile
					sourceAgentName = fmt.Sprintf("Hub[%s]", refFilename)
					log.Printf("[hub/pipe] using hub partial match @%s -> file %s", refFilename, matchedFile)
				} else {
					return fmt.Sprintf("❌ 找不到匹配 %q 的文件\n\n💡 提示:\n- 使用 @<编号>: @1、@-1\n- 使用 @<部分文件名>: @gemini\n- 查看文件: /hub list", refFilename)
				}
			}
		}
	}

	// No @ reference → normal pipe: send to default agent first
	if reply1 == "" {
		sourceAgent := h.getDefaultAgent()
		if sourceAgent == nil {
			return "❌ 没有可用的默认 agent，请先设置默认 agent（如 /claude）"
		}

		h.mu.RLock()
		sourceAgentName = h.defaultName
		h.mu.RUnlock()

		log.Printf("[hub/pipe] step1: sending to default agent (%s)", sourceAgentName)
		var err error
		reply1, err = h.chatWithAgent(ctx, sourceAgent, msg.FromUserID, message)
		if err != nil {
			return fmt.Sprintf("❌ 第一步（默认 agent %s）失败: %v", sourceAgentName, err)
		}

		shortAgentName := sourceAgentName
		if idx := strings.LastIndex(sourceAgentName, "/"); idx >= 0 {
			shortAgentName = sourceAgentName[idx+1:]
		}
		filename = fmt.Sprintf("pipe_%s_%s.md", timestamp, shortAgentName)
		savePath, err := h.hub.Save(filename, reply1, sourceAgentName)
		if err != nil {
			log.Printf("[hub/pipe] save failed: %v", err)
			filename = ""
		} else {
			log.Printf("[hub/pipe] saved step1 reply to %s", savePath)
		}
	}

	// Get target agent
	targetAg, err := h.getAgent(ctx, targetAgent)
	if err != nil {
		return fmt.Sprintf("❌ 目标 agent %q 不可用: %v", targetAgent, err)
	}

	// Build prompt for target agent based on saved hub file
	var hubContext string
	if filename != "" {
		hubContext, err = h.hub.ReadSpecific([]string{filename})
		if err != nil {
			log.Printf("[hub/pipe] read saved file failed: %v", err)
			hubContext = ""
		}
	}

	if hubContext == "" {
		hubContext = fmt.Sprintf("上一步的回复：\n%s", reply1)
	}

	secondPrompt := fmt.Sprintf(
		"请基于以下内容，继续进行分析或给出你的观点：\n\n---\n%s\n---\n\n要求：直接输出分析结果，不要重复原文。",
		hubContext,
	)

	convID := "hub:" + targetAgent + ":" + msg.FromUserID
	log.Printf("[hub/pipe] step2: sending to target agent (%s)", targetAgent)
	reply2, err := h.chatWithAgent(ctx, targetAg, convID, secondPrompt)
	if err != nil {
		return fmt.Sprintf("❌ 第二步（目标 agent %s）失败: %v", targetAgent, err)
	}

	// Auto-save final result
	finalFilename := fmt.Sprintf("pipe_%s_%s_final.md", timestamp, targetAgent)
	finalSaved := false
	if _, err := h.hub.Save(finalFilename, reply2, targetAgent); err != nil {
		log.Printf("[hub/pipe] failed to save final reply: %v", err)
	} else {
		finalSaved = true
	}

	// Return result with file info
	result := reply2
	if filename != "" || finalSaved {
		files, _ := h.hub.ListWithInfo()

		var sourceNum, finalNum int
		for i, f := range files {
			if f.Name == filename {
				sourceNum = i + 1
			}
			if f.Name == finalFilename {
				finalNum = i + 1
			}
		}

		var fileInfo strings.Builder
		fileInfo.WriteString(fmt.Sprintf("\n\n📁 Pipe 流程: %s → %s", sourceAgentName, targetAgent))

		if filename != "" && sourceNum > 0 {
			fileInfo.WriteString(fmt.Sprintf("\n💾 源文件: [@%d] %s", sourceNum, filename))
		}
		if finalSaved && finalNum > 0 {
			fileInfo.WriteString(fmt.Sprintf("\n💾 结果: [@%d] %s", finalNum, finalFilename))
		}

		if finalNum > 0 {
			fileInfo.WriteString(fmt.Sprintf("\n\n💡 继续分析: /hub pipe <agent> @%d <消息>", finalNum))
		}

		result += fileInfo.String()
	}
	return result
}

func buildHelpText() string {
	return `🤖 WeClaw Agent Hub

📌 基本指令
  @agent msg       发给指定 agent
  @a @b msg        广播给多个 agent
  /new /clear      开始新会话
  /cwd /path       切换工作目录
  /info            显示当前 agent 信息
  /help            显示此帮助信息

🗂️ Hub 共享上下文
  /save <文件名> <消息>   让 agent 回答并保存到 Hub
  /save <文件名>          保存上一条回复到 Hub
  /hub                   列出 Hub 文件
  /hub ls                查看文件列表（带编号）
  /hub cat <编号>         查看文件内容
  /hub <消息>             注入所有 Hub 上下文，发给 agent
  /hub <文件名> <消息>    注入指定文件，发给 agent
  /hub clear             清空 Hub

🔗 Pipe 链式协作
  /hub pipe <agent> <消息>         默认agent→保存→目标agent
  /hub pipe <agent> @<编号> <消息>  直接用 Hub 文件作为输入
  /hub pipe <agent> @-1 <消息>     用最新 Hub 文件作为输入

Aliases: /cc(claude) /cx(codex) /cs(cursor) /km(kimi) /gm(gemini) /oc(openclaw) /qw(qwen)`
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
