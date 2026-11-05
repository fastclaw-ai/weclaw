package messaging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractLocalAttachmentPaths(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "report.pdf")
	txtPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(pdfPath, []byte("pdf"), 0o644); err != nil {
		t.Fatalf("write pdf: %v", err)
	}
	if err := os.WriteFile(txtPath, []byte("txt"), 0o644); err != nil {
		t.Fatalf("write txt: %v", err)
	}

	reply := strings.Join([]string{
		"这里是内联路径，不应该命中 " + pdfPath,
		pdfPath,
		"1. " + txtPath,
		txtPath,
		"file://" + pdfPath,
		filepath.Join(dir, "missing.pdf"),
		filepath.Join(dir, "folder"),
	}, "\n")

	got := extractLocalAttachmentPaths(reply)
	if len(got) != 2 {
		t.Fatalf("expected 2 paths, got %d (%v)", len(got), got)
	}
	if got[0] != pdfPath {
		t.Fatalf("got[0] = %q, want %q", got[0], pdfPath)
	}
	if got[1] != txtPath {
		t.Fatalf("got[1] = %q, want %q", got[1], txtPath)
	}
}

func TestIsAllowedAttachmentPath(t *testing.T) {
	workspaceRoot := filepath.Join(t.TempDir(), "workspace")
	otherRoot := filepath.Join(t.TempDir(), "other")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.MkdirAll(otherRoot, 0o755); err != nil {
		t.Fatalf("mkdir other: %v", err)
	}

	allowedPath := filepath.Join(workspaceRoot, "artifacts", "report.pdf")
	deniedPath := filepath.Join(otherRoot, "report.pdf")
	if err := os.MkdirAll(filepath.Dir(allowedPath), 0o755); err != nil {
		t.Fatalf("mkdir allowed dir: %v", err)
	}
	if err := os.WriteFile(allowedPath, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write allowed file: %v", err)
	}
	if err := os.WriteFile(deniedPath, []byte("no"), 0o644); err != nil {
		t.Fatalf("write denied file: %v", err)
	}

	if !isAllowedAttachmentPath(allowedPath, []string{workspaceRoot}) {
		t.Fatalf("expected %q to be allowed", allowedPath)
	}
	if isAllowedAttachmentPath(deniedPath, []string{workspaceRoot}) {
		t.Fatalf("expected %q to be denied", deniedPath)
	}
}

func TestRewriteReplyWithAttachmentResults(t *testing.T) {
	sentPath := "/tmp/report.pdf"
	failedPath := "/tmp/archive.zip"
	reply := strings.Join([]string{
		"已生成文件：",
		sentPath,
		"这里再次内联提到 " + sentPath + "，不应该被替换。",
		failedPath,
	}, "\n")

	got := rewriteReplyWithAttachmentResults(reply, []string{sentPath}, []string{failedPath})

	if strings.Contains(got, "\n"+sentPath+"\n") {
		t.Fatalf("expected sent path line to be replaced, got %q", got)
	}
	if !strings.Contains(got, "已发送附件：report.pdf") {
		t.Fatalf("expected sent replacement, got %q", got)
	}
	if !strings.Contains(got, "这里再次内联提到 "+sentPath+"，不应该被替换。") {
		t.Fatalf("expected inline path to remain, got %q", got)
	}
	if !strings.Contains(got, failedPath) {
		t.Fatalf("expected failed path to remain, got %q", got)
	}
	if !strings.Contains(got, "附件发送失败：archive.zip") {
		t.Fatalf("expected failure note, got %q", got)
	}
}
