package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHTTPAgentChatWithAttachmentsUsesNativeMediaContent(t *testing.T) {
	for _, model := range []string{"MiniMax-M3.1-Flash", "MiniMax-M3"} {
		t.Run(model, func(t *testing.T) {
			testHTTPAgentChatWithAttachmentsUsesNativeMediaContent(t, model)
		})
	}
}

func testHTTPAgentChatWithAttachmentsUsesNativeMediaContent(t *testing.T, model string) {
	var request struct {
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"done"}}]}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	imagePath := filepath.Join(dir, "sample.png")
	videoPath := filepath.Join(dir, "sample.mp4")
	if err := os.WriteFile(imagePath, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(videoPath, []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}

	ag := NewHTTPAgent(HTTPAgentConfig{Endpoint: server.URL, Model: model})
	reply, err := ag.ChatWithAttachments(context.Background(), "conversation", "Describe the media.", []Attachment{
		{Path: imagePath, MIMEType: "image/png", Name: "sample.png"},
		{Path: videoPath, MIMEType: "video/mp4", Name: "sample.mp4"},
	})
	if err != nil {
		t.Fatalf("ChatWithAttachments returned error: %v", err)
	}
	if reply != "done" {
		t.Fatalf("reply = %q, want done", reply)
	}

	content, ok := request.Messages[len(request.Messages)-1].Content.([]any)
	if !ok {
		t.Fatalf("user content type = %T, want array", request.Messages[len(request.Messages)-1].Content)
	}
	if len(content) != 3 {
		t.Fatalf("content part count = %d, want 3", len(content))
	}
	assertContentPartType(t, content[0], "text", "text", "Describe the media.")
	assertContentPartType(t, content[1], "image_url", "image_url", "data:image/png;base64,aW1hZ2U=")
	assertContentPartType(t, content[2], "video_url", "video_url", "data:video/mp4;base64,dmlkZW8=")
}

func TestHTTPAgentChatWithAttachmentsFallsBackForTextOnlyModel(t *testing.T) {
	var content any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Content any `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		content = request.Messages[len(request.Messages)-1].Content
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"done"}}]}`))
	}))
	defer server.Close()

	ag := NewHTTPAgent(HTTPAgentConfig{Endpoint: server.URL, Model: "text-only-model"})
	_, err := ag.ChatWithAttachments(context.Background(), "conversation", "Describe this.", []Attachment{{
		Path:     "/tmp/sample.png",
		MIMEType: "image/png",
		Name:     "sample.png",
	}})
	if err != nil {
		t.Fatalf("ChatWithAttachments returned error: %v", err)
	}
	text, ok := content.(string)
	if !ok {
		t.Fatalf("user content type = %T, want string", content)
	}
	if !strings.Contains(text, "/tmp/sample.png") {
		t.Fatalf("fallback content does not contain attachment path: %q", text)
	}
}

func assertContentPartType(t *testing.T, raw any, wantType, wantPayload, wantValue string) {
	t.Helper()
	part, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("content part type = %T, want object", raw)
	}
	if part["type"] != wantType {
		t.Fatalf("content part type = %v, want %q", part["type"], wantType)
	}
	payload, ok := part[wantPayload]
	if !ok {
		t.Fatalf("content part missing %q: %#v", wantPayload, part)
	}
	if wantPayload == "text" {
		if payload != wantValue {
			t.Fatalf("text payload = %v, want %q", payload, wantValue)
		}
		return
	}
	media, ok := payload.(map[string]any)
	if !ok || media["url"] != wantValue {
		t.Fatalf("media payload = %#v, want URL %q", payload, wantValue)
	}
}
