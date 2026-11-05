package messaging

import (
	"strings"
	"testing"
)

func TestStreamingMarkdownFilterPreservesSupportedMarkdown(t *testing.T) {
	input := "## Title\n\n**bold** and `code`\n\n| A | B |\n|---|---|\n| 1 | 2 |\n```go\nfmt.Println(1)\n```"

	got := MarkdownToPlainText(input)

	for _, want := range []string{"## Title", "**bold**", "`code`", "| A | B |", "```go"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected filtered markdown to contain %q, got:\n%s", want, got)
		}
	}
}

func TestStreamingMarkdownFilterRemovesImages(t *testing.T) {
	got := MarkdownToPlainText("before ![alt](https://example.com/a.png) after")
	want := "before  after"
	if got != want {
		t.Fatalf("MarkdownToPlainText() = %q, want %q", got, want)
	}
}

func TestStreamingMarkdownFilterStripsCJKItalicMarkers(t *testing.T) {
	got := MarkdownToPlainText("*中文* and *latin*")
	want := "中文 and *latin*"
	if got != want {
		t.Fatalf("MarkdownToPlainText() = %q, want %q", got, want)
	}
}
