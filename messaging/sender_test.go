package messaging

import (
	"testing"
	"time"

	"github.com/fastclaw-ai/weclaw/ilink"
)

func TestBuildSendMessageRequestUsesOpenClaw244BaseInfo(t *testing.T) {
	client := ilink.NewClient(&ilink.Credentials{
		BotToken:   "token",
		ILinkBotID: "bot",
	})

	req := buildSendMessageRequest(client, "user", "ctx", "cid", []ilink.MessageItem{
		{Type: ilink.ItemTypeText, TextItem: &ilink.TextItem{Text: "hello"}},
	})

	if req.BaseInfo.ChannelVersion != "2.4.4" {
		t.Fatalf("channel version = %q, want 2.4.4", req.BaseInfo.ChannelVersion)
	}
	if req.BaseInfo.BotAgent != "WeClaw/2.4.4" {
		t.Fatalf("bot agent = %q, want WeClaw/2.4.4", req.BaseInfo.BotAgent)
	}
	if req.Msg.FromUserID != "bot" || req.Msg.ToUserID != "user" || req.Msg.ClientID != "cid" {
		t.Fatalf("unexpected msg routing: %+v", req.Msg)
	}
}

func TestToolCallItems(t *testing.T) {
	start := ilink.MessageItem{
		Type:         ilink.ItemTypeToolCallStart,
		CreateTimeMs: 1,
		IsCompleted:  false,
		ToolCallStartItem: &ilink.ToolCallStartItem{
			ToolName:   "exec",
			ToolCallID: "call-1",
		},
	}
	if start.Type != 11 || start.ToolCallStartItem.ToolName != "exec" {
		t.Fatalf("unexpected start item: %+v", start)
	}

	result := ilink.MessageItem{
		Type:         ilink.ItemTypeToolCallResult,
		CreateTimeMs: 2,
		IsCompleted:  true,
		ToolCallResultItem: &ilink.ToolCallResultItem{
			ToolName:   "exec",
			ToolCallID: "call-1",
			Status:     normalizeToolStatus("completed"),
		},
	}
	if result.Type != 12 || result.ToolCallResultItem.Status != "completed" {
		t.Fatalf("unexpected result item: %+v", result)
	}
}

func clearTypingTicketCache() {
	typingTicketCache.Range(func(key, _ any) bool {
		typingTicketCache.Delete(key)
		return true
	})
}

func TestTypingTicketCacheHit(t *testing.T) {
	clearTypingTicketCache()
	defer clearTypingTicketCache()

	originalNow := nowFunc
	defer func() { nowFunc = originalNow }()

	baseTime := time.Date(2026, 4, 7, 2, 0, 0, 0, time.UTC)
	nowFunc = func() time.Time { return baseTime }

	cacheTypingTicket("user-1", "ticket-1")

	if got := getCachedTypingTicket("user-1"); got != "ticket-1" {
		t.Fatalf("getCachedTypingTicket() = %q, want %q", got, "ticket-1")
	}
}

func TestTypingTicketCacheExpires(t *testing.T) {
	clearTypingTicketCache()
	defer clearTypingTicketCache()

	originalNow := nowFunc
	defer func() { nowFunc = originalNow }()

	baseTime := time.Date(2026, 4, 7, 2, 0, 0, 0, time.UTC)
	nowFunc = func() time.Time { return baseTime }
	cacheTypingTicket("user-2", "ticket-2")

	nowFunc = func() time.Time { return baseTime.Add(typingTicketTTL + time.Second) }
	if got := getCachedTypingTicket("user-2"); got != "" {
		t.Fatalf("getCachedTypingTicket() after expiry = %q, want empty string", got)
	}
}

func TestInvalidateTypingTicketCache(t *testing.T) {
	clearTypingTicketCache()
	defer clearTypingTicketCache()

	cacheTypingTicket("user-3", "ticket-3")
	invalidateCachedTypingTicket("user-3")

	if got := getCachedTypingTicket("user-3"); got != "" {
		t.Fatalf("getCachedTypingTicket() after invalidate = %q, want empty string", got)
	}
}
