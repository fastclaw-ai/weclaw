package hub

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHubSaveAndRead(t *testing.T) {
	dir := t.TempDir()
	h := New(dir)

	// Save a file
	path, err := h.Save("round1_claude.md", "Hello from Claude", "claude")
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	expectedPath := filepath.Join(dir, "round1_claude.md")
	if path != expectedPath {
		t.Errorf("expected path %q, got %q", expectedPath, path)
	}

	// Read it back
	content, err := h.ReadFile("round1_claude.md")
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if content == "" {
		t.Error("expected non-empty content")
	}

	// Check frontmatter
	if !contains(content, "agent: claude") {
		t.Error("expected frontmatter with agent: claude")
	}

	// Check body
	if !contains(content, "Hello from Claude") {
		t.Error("expected body content")
	}
}

func TestHubReadAll(t *testing.T) {
	dir := t.TempDir()
	h := New(dir)

	// Save multiple files
	_, _ = h.Save("round1.md", "First round content", "claude")
	_, _ = h.Save("round2.md", "Second round content", "gemini")

	// Read all
	ctx, err := h.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	if !contains(ctx, "round1.md") || !contains(ctx, "round2.md") {
		t.Error("expected both files in context")
	}

	if !contains(ctx, "Agent Hub Shared Context") {
		t.Error("expected hub header")
	}
}

func TestHubReadAllEmpty(t *testing.T) {
	dir := t.TempDir()
	h := New(dir)

	ctx, err := h.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll on empty dir failed: %v", err)
	}
	if ctx != "" {
		t.Errorf("expected empty context, got %q", ctx)
	}
}

func TestHubList(t *testing.T) {
	dir := t.TempDir()
	h := New(dir)

	_, _ = h.Save("a.md", "content a", "claude")
	_, _ = h.Save("b.md", "content b", "gemini")

	files, err := h.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(files) != 2 {
		t.Errorf("expected 2 files, got %d", len(files))
	}
}

func TestHubClear(t *testing.T) {
	dir := t.TempDir()
	h := New(dir)

	_, _ = h.Save("a.md", "content a", "claude")
	_, _ = h.Save("b.md", "content b", "gemini")

	count, err := h.Clear()
	if err != nil {
		t.Fatalf("Clear failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected to clear 2 files, got %d", count)
	}

	files, _ := h.List()
	if len(files) != 0 {
		t.Errorf("expected 0 files after clear, got %d", len(files))
	}
}

func TestHubReadSpecific(t *testing.T) {
	dir := t.TempDir()
	h := New(dir)

	_, _ = h.Save("round1.md", "Round 1 content", "claude")
	_, _ = h.Save("round2.md", "Round 2 content", "gemini")

	ctx, err := h.ReadSpecific([]string{"round1.md"})
	if err != nil {
		t.Fatalf("ReadSpecific failed: %v", err)
	}

	if !contains(ctx, "Round 1 content") {
		t.Error("expected round1 content")
	}
	if contains(ctx, "Round 2 content") {
		t.Error("should not contain round2 content")
	}
}

func TestHubExists(t *testing.T) {
	dir := t.TempDir()
	h := New(dir)

	_, _ = h.Save("exists.md", "content", "claude")

	if !h.Exists("exists.md") {
		t.Error("expected file to exist")
	}
	if h.Exists("nope.md") {
		t.Error("expected file to not exist")
	}
}

func TestBuildPrompt(t *testing.T) {
	// With context
	result := BuildPrompt("some context", "my message")
	if result != "some context\n\nmy message" {
		t.Errorf("unexpected prompt: %q", result)
	}

	// Without context
	result = BuildPrompt("", "my message")
	if result != "my message" {
		t.Errorf("unexpected prompt without context: %q", result)
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal.md", "normal.md"},
		{"../../../etc/passwd", "passwd"},
		{"", "untitled.md"},
		{".", "untitled.md"},
		{"..", "untitled.md"},
		{"  spaces  ", "spaces"},
		{"file/with/slash", "slash"},
		// Note: backslash is valid in filenames on macOS/Linux, only rejected on Windows
		{"..hidden", "..hidden"}, // starts with .. but is not exactly .., so it's valid
	}

	for _, tt := range tests {
		result := sanitizeFilename(tt.input)
		if result != tt.expected {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// TestPathTraversalProtection tests that path traversal attacks are blocked
func TestPathTraversalProtection(t *testing.T) {
	dir := t.TempDir()
	h := New(dir)

	// Create a file outside the hub directory to test against
	outsideDir := filepath.Join(dir, "..", "outside")
	os.MkdirAll(outsideDir, 0755)
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	os.WriteFile(outsideFile, []byte("secret"), 0644)

	// Attempt to save with path traversal - should be sanitized
	_, err := h.Save("../../../outside/secret.txt", "content", "claude")
	// Should either save to sanitized path or fail validation
	if err != nil {
		// If it errors, it should be a validation error
		if !contains(err.Error(), "invalid filename") && !contains(err.Error(), "path escapes") {
			t.Errorf("unexpected error type: %v", err)
		}
	}

	// Verify the outside file wasn't modified
	content, _ := os.ReadFile(outsideFile)
	if string(content) != "secret" {
		t.Error("outside file was modified - path traversal protection failed!")
	}
}

// TestValidatePath tests the path validation function directly
func TestValidatePath(t *testing.T) {
	dir := t.TempDir()
	h := New(dir)

	// Valid path inside shared directory
	validPath := filepath.Join(dir, "test.md")
	if err := h.validatePath(validPath); err != nil {
		t.Errorf("valid path rejected: %v", err)
	}

	// Invalid path outside shared directory
	invalidPath := filepath.Join(dir, "..", "outside", "test.md")
	if err := h.validatePath(invalidPath); err == nil {
		t.Error("path traversal should have been rejected")
	}

	// Path with symlink-like traversal
	traversalPath := filepath.Join(dir, "subdir", "..", "..", "etc", "passwd")
	if err := h.validatePath(traversalPath); err == nil {
		t.Error("traversal path should have been rejected")
	}
}

func TestDefaultDir(t *testing.T) {
	dir := DefaultDir()
	if dir == "" {
		t.Error("expected non-empty default dir")
	}
	if !contains(dir, ".weclaw") {
		t.Errorf("expected .weclaw in path, got %q", dir)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Test that the shared directory is created
func TestNewCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "subdir", "hub")
	h := New(dir)

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("expected directory to be created")
	}

	// Hub should be functional
	path, err := h.Save("test.md", "test", "claude")
	if err != nil {
		t.Fatalf("Save to new dir failed: %v", err)
	}
	if path == "" {
		t.Error("expected non-empty path")
	}
}
