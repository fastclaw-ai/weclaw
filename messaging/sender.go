package messaging

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/fastclaw-ai/weclaw/ilink"
	"github.com/google/uuid"
)

const typingTicketTTL = 2 * time.Minute

type typingTicketCacheEntry struct {
	ticket    string
	expiresAt time.Time
}

var (
	typingTicketCache sync.Map
	nowFunc           = time.Now
)

// NewClientID generates a new unique client ID for message correlation.
func NewClientID() string {
	return uuid.New().String()
}

// SendTypingState sends a typing indicator to a user via the iLink sendtyping API.
// It first fetches a typing_ticket via getconfig, then sends the typing status.
func SendTypingState(ctx context.Context, client *ilink.Client, userID, contextToken string) error {
	if cachedTicket := getCachedTypingTicket(userID); cachedTicket != "" {
		if err := client.SendTyping(ctx, userID, cachedTicket, ilink.TypingStatusTyping); err == nil {
			log.Printf("[sender] sent typing indicator to %s", userID)
			return nil
		}
		invalidateCachedTypingTicket(userID)
	}

	// Get typing ticket
	configResp, err := client.GetConfig(ctx, userID, contextToken)
	if err != nil {
		return fmt.Errorf("get config for typing: %w", err)
	}
	if configResp.Ret != 0 {
		return fmt.Errorf("get config failed: ret=%d errmsg=%s", configResp.Ret, configResp.ErrMsg)
	}
	if configResp.TypingTicket == "" {
		return fmt.Errorf("no typing_ticket returned from getconfig")
	}

	// Send typing
	if err := client.SendTyping(ctx, userID, configResp.TypingTicket, ilink.TypingStatusTyping); err != nil {
		return fmt.Errorf("send typing: %w", err)
	}
	cacheTypingTicket(userID, configResp.TypingTicket)

	log.Printf("[sender] sent typing indicator to %s", userID)
	return nil
}

func getCachedTypingTicket(userID string) string {
	if userID == "" {
		return ""
	}

	v, ok := typingTicketCache.Load(userID)
	if !ok {
		return ""
	}

	entry, ok := v.(typingTicketCacheEntry)
	if !ok || entry.ticket == "" || !nowFunc().Before(entry.expiresAt) {
		typingTicketCache.Delete(userID)
		return ""
	}

	return entry.ticket
}

func cacheTypingTicket(userID, ticket string) {
	if userID == "" || ticket == "" {
		return
	}

	typingTicketCache.Store(userID, typingTicketCacheEntry{
		ticket:    ticket,
		expiresAt: nowFunc().Add(typingTicketTTL),
	})
}

func invalidateCachedTypingTicket(userID string) {
	if userID == "" {
		return
	}
	typingTicketCache.Delete(userID)
}

// SendTextReply sends a text reply to a user through the iLink API.
// If clientID is empty, a new one is generated.
func SendTextReply(ctx context.Context, client *ilink.Client, toUserID, text, contextToken, clientID string) error {
	if clientID == "" {
		clientID = NewClientID()
	}

	// Convert markdown to plain text for WeChat display
	plainText := MarkdownToPlainText(text)

	req := &ilink.SendMessageRequest{
		Msg: ilink.SendMsg{
			FromUserID:   client.BotID(),
			ToUserID:     toUserID,
			ClientID:     clientID,
			MessageType:  ilink.MessageTypeBot,
			MessageState: ilink.MessageStateFinish,
			ItemList: []ilink.MessageItem{
				{
					Type: ilink.ItemTypeText,
					TextItem: &ilink.TextItem{
						Text: plainText,
					},
				},
			},
			ContextToken: contextToken,
		},
		BaseInfo: ilink.BaseInfo{},
	}

	resp, err := client.SendMessage(ctx, req)
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	if resp.Ret != 0 {
		return fmt.Errorf("send message failed: ret=%d errmsg=%s", resp.Ret, resp.ErrMsg)
	}

	log.Printf("[sender] sent reply to %s: %q", toUserID, truncate(text, 50))
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
