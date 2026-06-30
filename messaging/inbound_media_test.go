package messaging

import (
	"testing"

	"github.com/fastclaw-ai/weclaw/ilink"
)

func TestExtractImages(t *testing.T) {
	msg := ilink.WeixinMessage{
		ItemList: []ilink.MessageItem{
			{Type: ilink.ItemTypeText, TextItem: &ilink.TextItem{Text: "caption"}},
			{Type: ilink.ItemTypeImage, ImageItem: &ilink.ImageItem{URL: "https://example.com/a.png"}},
			{Type: ilink.ItemTypeImage, ImageItem: &ilink.ImageItem{URL: "https://example.com/b.png"}},
		},
	}

	images := extractImages(msg)
	if len(images) != 2 {
		t.Fatalf("len(images) = %d, want 2", len(images))
	}
}

func TestMimeTypeFromImageExt(t *testing.T) {
	cases := map[string]string{
		".png":  "image/png",
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".gif":  "image/gif",
		".webp": "image/webp",
		".bmp":  "image/bmp",
		".bin":  "application/octet-stream",
	}
	for ext, want := range cases {
		if got := mimeTypeFromImageExt(ext); got != want {
			t.Fatalf("mimeTypeFromImageExt(%q) = %q, want %q", ext, got, want)
		}
	}
}

func TestMediaSaveDirFallsBackToAttachmentWorkspace(t *testing.T) {
	h := NewHandler(nil, nil)
	if got := h.mediaSaveDir(); got == "" {
		t.Fatal("expected non-empty fallback media save dir")
	}

	h.SetSaveDir("/tmp/custom")
	if got := h.mediaSaveDir(); got != "/tmp/custom" {
		t.Fatalf("mediaSaveDir() = %q, want /tmp/custom", got)
	}
}
