package agent

import (
	"strings"
	"testing"
)

func TestMessageWithAttachmentFallback(t *testing.T) {
	got := MessageWithAttachmentFallback("Analyze this", []Attachment{{
		Path:     "/tmp/image.png",
		MIMEType: "image/png",
		Name:     "image.png",
	}})

	for _, want := range []string{"Analyze this", "image.png: /tmp/image.png", "image/png"} {
		if !strings.Contains(got, want) {
			t.Fatalf("fallback prompt missing %q:\n%s", want, got)
		}
	}
}

func TestBuildPromptEntriesWithImageAttachment(t *testing.T) {
	entries := buildPromptEntries("Analyze this", []Attachment{{
		Path:     "/tmp/image.png",
		MIMEType: "image/png",
		Name:     "image.png",
	}})

	if len(entries) != 2 {
		t.Fatalf("entry count = %d, want 2", len(entries))
	}
	if entries[0].Type != "text" || !strings.Contains(entries[0].Text, "/tmp/image.png") {
		t.Fatalf("unexpected text entry: %+v", entries[0])
	}
	if entries[1].Type != "image" || entries[1].Path != "/tmp/image.png" || entries[1].MIMEType != "image/png" {
		t.Fatalf("unexpected image entry: %+v", entries[1])
	}
}
