package messaging

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/fastclaw-ai/weclaw/ilink"
	"github.com/google/uuid"
)

// NewClientID generates a new unique client ID for message correlation.
func NewClientID() string {
	return uuid.New().String()
}

// SendTypingState sends a typing indicator to a user via the iLink sendtyping API.
// It first fetches a typing_ticket via getconfig, then sends the typing status.
func SendTypingState(ctx context.Context, client *ilink.Client, userID, contextToken string) error {
	// Get typing ticket
	configResp, err := client.GetConfig(ctx, userID, contextToken)
	if err != nil {
		return fmt.Errorf("get config for typing: %w", err)
	}
	if configResp.TypingTicket == "" {
		return fmt.Errorf("no typing_ticket returned from getconfig")
	}

	// Send typing
	if err := client.SendTyping(ctx, userID, configResp.TypingTicket, ilink.TypingStatusTyping); err != nil {
		return fmt.Errorf("send typing: %w", err)
	}

	log.Printf("[sender] sent typing indicator to %s", userID)
	return nil
}

// SendTextReply sends a text reply to a user through the iLink API.
// If clientID is empty, a new one is generated.
func SendTextReply(ctx context.Context, client *ilink.Client, toUserID, text, contextToken, clientID string) error {
	if clientID == "" {
		clientID = NewClientID()
	}

	filteredText := MarkdownToPlainText(text)

	req := buildSendMessageRequest(client, toUserID, contextToken, clientID, []ilink.MessageItem{
		{
			Type: ilink.ItemTypeText,
			TextItem: &ilink.TextItem{
				Text: filteredText,
			},
		},
	})

	if err := sendMessageRequest(ctx, client, req); err != nil {
		return err
	}

	log.Printf("[sender] sent reply to %s: %q", toUserID, truncate(text, 50))
	return nil
}

// SendMessageItem sends one structured iLink item as a standalone message.
func SendMessageItem(ctx context.Context, client *ilink.Client, toUserID string, item ilink.MessageItem, contextToken, clientID string) error {
	if clientID == "" {
		clientID = NewClientID()
	}

	req := buildSendMessageRequest(client, toUserID, contextToken, clientID, []ilink.MessageItem{item})
	if err := sendMessageRequest(ctx, client, req); err != nil {
		return err
	}

	log.Printf("[sender] sent item type=%d to %s", item.Type, toUserID)
	return nil
}

// SendToolCallStart sends an openclaw-weixin 2.4.x tool-call progress start item.
func SendToolCallStart(ctx context.Context, client *ilink.Client, toUserID, toolName, toolCallID, contextToken, clientID string) error {
	item := ilink.MessageItem{
		Type:         ilink.ItemTypeToolCallStart,
		CreateTimeMs: nowMillis(),
		IsCompleted:  false,
		ToolCallStartItem: &ilink.ToolCallStartItem{
			ToolName:   toolName,
			ToolCallID: toolCallID,
		},
	}
	return SendMessageItem(ctx, client, toUserID, item, contextToken, clientID)
}

// SendToolCallResult sends an openclaw-weixin 2.4.x tool-call progress result item.
func SendToolCallResult(ctx context.Context, client *ilink.Client, toUserID, toolName, toolCallID, status, contextToken, clientID string) error {
	item := ilink.MessageItem{
		Type:         ilink.ItemTypeToolCallResult,
		CreateTimeMs: nowMillis(),
		IsCompleted:  true,
		ToolCallResultItem: &ilink.ToolCallResultItem{
			ToolName:   toolName,
			ToolCallID: toolCallID,
			Status:     normalizeToolStatus(status),
		},
	}
	return SendMessageItem(ctx, client, toUserID, item, contextToken, clientID)
}

func buildSendMessageRequest(client *ilink.Client, toUserID, contextToken, clientID string, items []ilink.MessageItem) *ilink.SendMessageRequest {
	return &ilink.SendMessageRequest{
		Msg: ilink.SendMsg{
			FromUserID:   client.BotID(),
			ToUserID:     toUserID,
			ClientID:     clientID,
			MessageType:  ilink.MessageTypeBot,
			MessageState: ilink.MessageStateFinish,
			ItemList:     items,
			ContextToken: contextToken,
		},
		BaseInfo: ilink.BuildBaseInfo(),
	}
}

func sendMessageRequest(ctx context.Context, client *ilink.Client, req *ilink.SendMessageRequest) error {
	resp, err := client.SendMessage(ctx, req)
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	if resp.Ret != 0 {
		return fmt.Errorf("send message failed: ret=%d errmsg=%s", resp.Ret, resp.ErrMsg)
	}
	return nil
}

func nowMillis() int64 {
	return time.Now().UnixMilli()
}

func normalizeToolStatus(status string) string {
	switch status {
	case "completed", "failed", "blocked":
		return status
	default:
		return "unknown"
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
