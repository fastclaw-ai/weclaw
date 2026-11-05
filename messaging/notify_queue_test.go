package messaging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnqueuePendingNotification(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	if err := EnqueuePendingNotification("user-1", "hello"); err != nil {
		t.Fatalf("EnqueuePendingNotification: %v", err)
	}
	if err := EnqueuePendingNotification("", "ignored"); err != nil {
		t.Fatalf("EnqueuePendingNotification(empty to): %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, ".weclaw", "pending-notifications.jsonl"))
	if err != nil {
		t.Fatalf("read queue: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], `"to":"user-1"`) || !strings.Contains(lines[0], `"text":"hello"`) {
		t.Fatalf("unexpected queue contents: %q", data)
	}
}

func TestSanitizeIncomingFileName(t *testing.T) {
	cases := map[string]string{
		"report.pdf":                 "report.pdf",
		"../../.ssh/authorized_keys": "authorized_keys",
		`..\..\evil.exe`:             "evil.exe",
		"a:b?c*.txt":                 "a-b-c-.txt",
		"..":                         "",
		"  ":                         "",
	}
	for in, want := range cases {
		if got := sanitizeIncomingFileName(in); got != want {
			t.Errorf("sanitizeIncomingFileName(%q) = %q, want %q", in, got, want)
		}
	}
}
