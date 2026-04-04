package hub

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Hub manages shared context files for cross-agent collaboration.
type Hub struct {
	mu        sync.RWMutex
	sharedDir string // directory for shared context files
}

// New creates a new Hub with the given shared directory.
func New(sharedDir string) *Hub {
	os.MkdirAll(sharedDir, 0o755)
	return &Hub{sharedDir: sharedDir}
}

// DefaultDir returns the default hub shared directory path.
func DefaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "weclaw-hub", "shared")
	}
	return filepath.Join(home, ".weclaw", "hub", "shared")
}

// SharedDir returns the hub's shared directory path.
func (h *Hub) SharedDir() string {
	return h.sharedDir
}

// Save writes content to a file in the shared directory with YAML frontmatter.
// agentName identifies which agent produced the content.
func (h *Hub) Save(filename, content, agentName string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Sanitize filename
	filename = sanitizeFilename(filename)
	if !strings.HasSuffix(filename, ".md") {
		filename += ".md"
	}

	filePath := filepath.Join(h.sharedDir, filename)

	// Validate path is within shared directory (defense in depth)
	if err := h.validatePath(filePath); err != nil {
		return "", fmt.Errorf("invalid filename: %w", err)
	}

	// Build frontmatter
	timestamp := time.Now().Format("2006-01-02T15:04:05+08:00")
	frontmatter := fmt.Sprintf("---\nagent: %s\ntimestamp: %s\n---\n\n", agentName, timestamp)

	fullContent := frontmatter + content

	if err := os.WriteFile(filePath, []byte(fullContent), 0o644); err != nil {
		return "", fmt.Errorf("save hub file: %w", err)
	}

	return filePath, nil
}

// SaveRaw writes raw content to a file (no frontmatter) in the shared directory.
func (h *Hub) SaveRaw(filename, content string) (string, error) {
	filename = sanitizeFilename(filename)
	filePath := filepath.Join(h.sharedDir, filename)

	// Validate path is within shared directory (defense in depth)
	if err := h.validatePath(filePath); err != nil {
		return "", fmt.Errorf("invalid filename: %w", err)
	}

	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("save hub file: %w", err)
	}

	return filePath, nil
}

// ReadFile reads a specific file from the shared directory.
func (h *Hub) ReadFile(filename string) (string, error) {
	filename = sanitizeFilename(filename)
	filePath := filepath.Join(h.sharedDir, filename)

	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read hub file: %w", err)
	}

	return string(data), nil
}

// ReadAll reads all files from the shared directory and returns their combined content.
// Returns a formatted context string ready for injection into agent prompts.
func (h *Hub) ReadAll() (string, error) {
	entries, err := os.ReadDir(h.sharedDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil // empty hub is fine
		}
		return "", fmt.Errorf("read hub directory: %w", err)
	}

	if len(entries) == 0 {
		return "", nil
	}

	// Sort by modification time (oldest first)
	type fileEntry struct {
		name string
		info os.FileInfo
	}
	var files []fileEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileEntry{name: e.Name(), info: info})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].info.ModTime().Before(files[j].info.ModTime())
	})

	var sb strings.Builder
	sb.WriteString("=== Agent Hub Shared Context ===\n\n")

	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(h.sharedDir, f.name))
		if err != nil {
			continue
		}

		sb.WriteString(fmt.Sprintf("--- %s ---\n", f.name))
		sb.Write(data)
		sb.WriteString("\n\n")
	}

	sb.WriteString("=== End Hub Context ===\n")
	return sb.String(), nil
}

// List returns all filenames in the shared directory.
func (h *Hub) List() ([]string, error) {
	entries, err := os.ReadDir(h.sharedDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list hub directory: %w", err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		names = append(names, e.Name())
	}

	sort.Strings(names)
	return names, nil
}

// Clear removes all files from the shared directory.
func (h *Hub) Clear() (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	entries, err := os.ReadDir(h.sharedDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("clear hub directory: %w", err)
	}

	count := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(h.sharedDir, e.Name())
		if err := os.Remove(path); err != nil {
			continue
		}
		count++
	}

	return count, nil
}

// ReadSpecific reads specific files from the shared directory.
// filenames is a list of filenames to read.
func (h *Hub) ReadSpecific(filenames []string) (string, error) {
	var sb strings.Builder
	sb.WriteString("=== Agent Hub Shared Context ===\n\n")

	for _, fname := range filenames {
		fname = sanitizeFilename(fname)
		data, err := os.ReadFile(filepath.Join(h.sharedDir, fname))
		if err != nil {
			sb.WriteString(fmt.Sprintf("--- %s (not found) ---\n\n", fname))
			continue
		}

		sb.WriteString(fmt.Sprintf("--- %s ---\n", fname))
		sb.Write(data)
		sb.WriteString("\n\n")
	}

	sb.WriteString("=== End Hub Context ===\n")
	return sb.String(), nil
}

// Exists checks if a file exists in the shared directory.
func (h *Hub) Exists(filename string) bool {
	filename = sanitizeFilename(filename)
	_, err := os.Stat(filepath.Join(h.sharedDir, filename))
	return err == nil
}

// FileInfo holds metadata about a hub file.
type FileInfo struct {
	Name    string
	ModTime time.Time
	Size    int64
}

// ListWithInfo returns all hub files sorted by modification time (newest first),
// with metadata including name, modification time, and size.
func (h *Hub) ListWithInfo() ([]FileInfo, error) {
	entries, err := os.ReadDir(h.sharedDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list hub directory: %w", err)
	}

	var files []FileInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, FileInfo{
			Name:    e.Name(),
			ModTime: info.ModTime(),
			Size:    info.Size(),
		})
	}

	// Sort by modification time, newest first
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime.After(files[j].ModTime)
	})

	return files, nil
}

// FindByPartialName finds a hub file by partial filename match (case-insensitive).
// Returns the exact filename if exactly one match is found, or an error if zero or multiple matches.
func (h *Hub) FindByPartialName(partial string) (string, error) {
	partial = strings.ToLower(strings.TrimSpace(partial))
	if partial == "" {
		return "", fmt.Errorf("empty search term")
	}

	files, err := h.List()
	if err != nil {
		return "", err
	}

	var matches []string
	for _, f := range files {
		if strings.Contains(strings.ToLower(f), partial) {
			matches = append(matches, f)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no file matching %q", partial)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ambiguous match %q: found %v", partial, matches)
	}
}

// BuildPrompt creates a prompt with hub context injected.
// If context is empty, returns the original message.
func BuildPrompt(context, message string) string {
	if context == "" {
		return message
	}
	return fmt.Sprintf("%s\n\n%s", context, message)
}

// sanitizeFilename removes path traversal attempts and dangerous characters.
// It ensures the returned filename is safe to use within the shared directory.
func sanitizeFilename(name string) string {
	// Remove null bytes
	name = strings.ReplaceAll(name, "\x00", "")
	name = strings.TrimSpace(name)

	// Reject empty, current dir, parent dir references
	if name == "" || name == "." || name == ".." {
		return "untitled.md"
	}

	// Reject any path containing path separators or parent dir references
	if strings.Contains(name, "/") || strings.Contains(name, "\\") ||
		strings.Contains(name, "..") {
		// Extract just the base name as fallback
		name = filepath.Base(name)
		// Double-check after Base extraction
		if name == "" || name == "." || name == ".." {
			return "untitled.md"
		}
	}

	return name
}

// validatePath ensures the given path is within the hub's shared directory.
// Returns an error if the path attempts to escape the shared directory.
func (h *Hub) validatePath(filePath string) error {
	// Get absolute paths for comparison
	absSharedDir, err := filepath.Abs(h.sharedDir)
	if err != nil {
		return fmt.Errorf("cannot resolve shared directory: %w", err)
	}

	absFilePath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("cannot resolve file path: %w", err)
	}

	// Clean paths to resolve any .. or .
	absSharedDir = filepath.Clean(absSharedDir)
	absFilePath = filepath.Clean(absFilePath)

	// Ensure file path starts with shared directory path
	if !strings.HasPrefix(absFilePath, absSharedDir+string(filepath.Separator)) &&
		absFilePath != absSharedDir {
		return fmt.Errorf("path escapes shared directory: %s", filePath)
	}

	return nil
}
