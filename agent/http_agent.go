package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ChatMessage represents a single message in a conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type chatContentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *chatMediaURL `json:"image_url,omitempty"`
	VideoURL *chatMediaURL `json:"video_url,omitempty"`
}

type chatMediaURL struct {
	URL string `json:"url"`
}

// HTTPAgent is an OpenAI-compatible chat completions API client.
type HTTPAgent struct {
	endpoint     string
	apiKey       string
	headers      map[string]string
	model        string
	systemPrompt string
	httpClient   *http.Client
	mu           sync.Mutex
	history      map[string][]ChatMessage // conversationID -> messages
	maxHistory   int
}

// DefaultHTTPTimeout is the request timeout for HTTP agents when the config
// does not override it. 60s was too tight for agent turns that include
// context compaction or a slow model call (observed via WeChat timeouts).
const DefaultHTTPTimeout = 180 * time.Second

// HTTPAgentConfig holds configuration for the HTTP agent.
type HTTPAgentConfig struct {
	Endpoint     string
	APIKey       string
	Headers      map[string]string
	Model        string
	SystemPrompt string
	MaxHistory   int
	Timeout      time.Duration
}

// NewHTTPAgent creates a new OpenAI-compatible HTTP agent.
func NewHTTPAgent(cfg HTTPAgentConfig) *HTTPAgent {
	if cfg.MaxHistory == 0 {
		cfg.MaxHistory = 20
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-4o-mini"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultHTTPTimeout
	}
	return &HTTPAgent{
		endpoint:     cfg.Endpoint,
		apiKey:       cfg.APIKey,
		headers:      cfg.Headers,
		model:        cfg.Model,
		systemPrompt: cfg.SystemPrompt,
		httpClient:   &http.Client{Timeout: cfg.Timeout},
		history:      make(map[string][]ChatMessage),
		maxHistory:   cfg.MaxHistory,
	}
}

// Info returns metadata about this agent.
func (a *HTTPAgent) Info() AgentInfo {
	return AgentInfo{
		Name:    "http",
		Type:    "http",
		Model:   a.model,
		Command: a.endpoint,
	}
}

// SetCwd is a no-op for HTTP agents (they have no working directory).
func (a *HTTPAgent) SetCwd(_ string) {}

// ResetSession clears the conversation history for the given conversationID.
// HTTP agents have no server-side session ID, so an empty string is returned.
func (a *HTTPAgent) ResetSession(_ context.Context, conversationID string) (string, error) {
	a.mu.Lock()
	delete(a.history, conversationID)
	a.mu.Unlock()
	return "", nil
}

// Chat sends a message to the OpenAI-compatible API and returns the response.
func (a *HTTPAgent) Chat(ctx context.Context, conversationID string, message string) (string, error) {
	return a.chat(ctx, conversationID, message)
}

// ChatWithAttachments sends supported media as native content parts.
func (a *HTTPAgent) ChatWithAttachments(ctx context.Context, conversationID string, message string, attachments []Attachment) (string, error) {
	if len(attachments) == 0 {
		return a.Chat(ctx, conversationID, message)
	}
	switch a.model {
	case "MiniMax-M3.1-Flash", "MiniMax-M3":
	default:
		return a.Chat(ctx, conversationID, MessageWithAttachmentFallback(message, attachments))
	}

	content, err := buildNativeMediaContent(message, attachments)
	if err != nil {
		return "", err
	}
	return a.chat(ctx, conversationID, content)
}

func (a *HTTPAgent) chat(ctx context.Context, conversationID string, content any) (string, error) {
	a.mu.Lock()
	messages := a.buildMessages(conversationID, content)
	a.mu.Unlock()

	reqBody := map[string]interface{}{
		"model":    a.model,
		"messages": messages,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if a.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
	}
	for k, v := range a.headers {
		req.Header.Set(k, v)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	reply := result.Choices[0].Message.Content

	// Save to history
	a.mu.Lock()
	a.history[conversationID] = append(a.history[conversationID],
		ChatMessage{Role: "user", Content: content},
		ChatMessage{Role: "assistant", Content: reply},
	)
	// Trim history
	if len(a.history[conversationID]) > a.maxHistory*2 {
		a.history[conversationID] = a.history[conversationID][len(a.history[conversationID])-a.maxHistory*2:]
	}
	a.mu.Unlock()

	return reply, nil
}

func (a *HTTPAgent) buildMessages(conversationID string, content any) []ChatMessage {
	var messages []ChatMessage
	if a.systemPrompt != "" {
		messages = append(messages, ChatMessage{Role: "system", Content: a.systemPrompt})
	}
	if hist, ok := a.history[conversationID]; ok {
		messages = append(messages, hist...)
	}
	messages = append(messages, ChatMessage{Role: "user", Content: content})
	return messages
}

func buildNativeMediaContent(message string, attachments []Attachment) ([]chatContentPart, error) {
	parts := make([]chatContentPart, 0, len(attachments)+1)
	if strings.TrimSpace(message) != "" {
		parts = append(parts, chatContentPart{Type: "text", Text: message})
	}

	for _, attachment := range attachments {
		mediaType := attachment.MIMEType
		if mediaType == "" {
			mediaType = mime.TypeByExtension(filepath.Ext(attachment.Path))
		}
		mediaType = strings.ToLower(strings.TrimSpace(strings.Split(mediaType, ";")[0]))
		if !strings.HasPrefix(mediaType, "image/") && !strings.HasPrefix(mediaType, "video/") {
			return nil, fmt.Errorf("unsupported native media type %q", mediaType)
		}

		data, err := os.ReadFile(attachment.Path)
		if err != nil {
			return nil, fmt.Errorf("read attachment %q: %w", attachment.Path, err)
		}
		media := &chatMediaURL{URL: "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data)}
		part := chatContentPart{Type: "image_url", ImageURL: media}
		if strings.HasPrefix(mediaType, "video/") {
			part = chatContentPart{Type: "video_url", VideoURL: media}
		}
		parts = append(parts, part)
	}

	return parts, nil
}
