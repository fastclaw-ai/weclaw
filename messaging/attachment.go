package messaging

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

var supportedAttachmentExts = []string{
	".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
	".zip", ".txt", ".csv",
	".png", ".jpg", ".jpeg", ".gif", ".webp",
	".mp4", ".mov",
}

func defaultAttachmentWorkspace() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Clean(os.TempDir())
	}
	return filepath.Join(home, ".weclaw", "workspace")
}

func extractLocalAttachmentPaths(text string) []string {
	var paths []string
	seen := make(map[string]struct{})

	for _, line := range strings.Split(text, "\n") {
		candidate := strings.TrimSpace(line)
		if candidate == "" || !filepath.IsAbs(candidate) {
			continue
		}
		if !isSupportedAttachmentPath(candidate) {
			continue
		}
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		paths = append(paths, candidate)
	}

	return paths
}

func isAllowedAttachmentPath(path string, allowedRoots []string) bool {
	cleanPath, err := canonicalizePath(path, true)
	if err != nil {
		return false
	}

	for _, root := range allowedRoots {
		if root == "" {
			continue
		}
		cleanRoot, err := canonicalizePath(root, false)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(cleanRoot, cleanPath)
		if err != nil {
			continue
		}
		if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))) {
			return true
		}
	}

	return false
}

func rewriteReplyWithAttachmentResults(reply string, sentPaths, failedPaths []string) string {
	sentMap := make(map[string]string, len(sentPaths))
	for _, path := range sentPaths {
		sentMap[path] = "已发送附件：" + filepath.Base(path)
	}

	lines := strings.Split(reply, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if replacement, ok := sentMap[trimmed]; ok {
			lines[i] = replacement
		}
	}

	rewritten := strings.Join(lines, "\n")

	var failureLines []string
	seenFailures := make(map[string]struct{})
	for _, path := range failedPaths {
		if _, ok := seenFailures[path]; ok {
			continue
		}
		seenFailures[path] = struct{}{}
		failureLines = append(failureLines, "附件发送失败："+filepath.Base(path))
	}
	if len(failureLines) == 0 {
		return rewritten
	}
	if strings.TrimSpace(rewritten) == "" {
		return strings.Join(failureLines, "\n")
	}
	return rewritten + "\n" + strings.Join(failureLines, "\n")
}

func isSupportedAttachmentPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return slices.Contains(supportedAttachmentExts, ext)
}

func canonicalizePath(path string, mustExist bool) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if realPath, err := filepath.EvalSymlinks(absPath); err == nil {
		return filepath.Clean(realPath), nil
	} else if mustExist {
		return "", err
	}
	return filepath.Clean(absPath), nil
}
