package ilink

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContextTokenStoreRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	botID := "bot@example"
	userID := "user@im.wechat"
	token := "context-token"

	if err := SaveContextToken(botID, userID, token); err != nil {
		t.Fatalf("SaveContextToken() error = %v", err)
	}

	got, err := LoadContextToken(botID, userID)
	if err != nil {
		t.Fatalf("LoadContextToken() error = %v", err)
	}
	if got != token {
		t.Fatalf("LoadContextToken() = %q, want %q", got, token)
	}

	path := filepath.Join(home, ".weclaw", "accounts", "bot-example.contexts.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("context token file was not written: %v", err)
	}
}

func TestClearContextTokens(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	botID := "bot@example"
	userID := "user@im.wechat"

	if err := SaveContextToken(botID, userID, "context-token"); err != nil {
		t.Fatalf("SaveContextToken() error = %v", err)
	}
	if err := ClearContextTokens(botID); err != nil {
		t.Fatalf("ClearContextTokens() error = %v", err)
	}

	got, err := LoadContextToken(botID, userID)
	if err != nil {
		t.Fatalf("LoadContextToken() error = %v", err)
	}
	if got != "" {
		t.Fatalf("LoadContextToken() = %q, want empty", got)
	}
}
