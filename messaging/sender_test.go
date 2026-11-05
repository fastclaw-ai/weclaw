package messaging

import (
	"testing"
	"time"
)

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