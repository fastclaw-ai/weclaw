package messaging

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"bufio"

	"github.com/google/uuid"
	"golang.org/x/net/html"
)

var reURL = regexp.MustCompile(`https?://\S+`)

// IsURL checks if the text is (or starts with) a URL.
func IsURL(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://")
}

// ExtractURL extracts the first URL from text.
func ExtractURL(text string) string {
	match := reURL.FindString(text)
	return match
}

// LinkMetadata holds extracted metadata from a web page.
type LinkMetadata struct {
	Title       string
	Description string
	Author      string
	OGImage     string
	Published   string
	Body        string
}

// FetchLinkMetadata fetches a URL and extracts metadata from the HTML.
func FetchLinkMetadata(ctx context.Context, rawURL string) (*LinkMetadata, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://mp.weixin.qq.com/")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	doc, err := html.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse HTML: %w", err)
	}

	meta := &LinkMetadata{}
	extractMeta(doc, meta)

	// Fallback title from URL if empty
	if meta.Title == "" {
		meta.Title = rawURL
	}

	return meta, nil
}

// extractMeta walks the HTML tree and extracts metadata.
func extractMeta(n *html.Node, meta *LinkMetadata) {
	if n.Type == html.ElementNode {
		switch n.Data {
		case "meta":
			handleMeta(n, meta)
		case "title":
			if meta.Title == "" && n.FirstChild != nil {
				meta.Title = strings.TrimSpace(n.FirstChild.Data)
			}
		case "div":
			// WeChat article body
			for _, a := range n.Attr {
				if a.Key == "id" && a.Val == "js_content" {
					meta.Body = extractNodeText(n)
					return
				}
			}
		case "article":
			if meta.Body == "" {
				meta.Body = extractNodeText(n)
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		extractMeta(c, meta)
	}
}

// handleMeta extracts og: and other meta tag values.
func handleMeta(n *html.Node, meta *LinkMetadata) {
	var property, name, content string
	for _, a := range n.Attr {
		switch a.Key {
		case "property":
			property = a.Val
		case "name":
			name = a.Val
		case "content":
			content = a.Val
		}
	}
	if content == "" {
		return
	}
	switch {
	case property == "og:title" && meta.Title == "":
		meta.Title = content
	case property == "og:description" && meta.Description == "":
		meta.Description = content
	case property == "og:image" && meta.OGImage == "":
		meta.OGImage = content
	case property == "article:published_time" && meta.Published == "":
		meta.Published = content
	case name == "author" && meta.Author == "":
		meta.Author = content
	case name == "description" && meta.Description == "":
		meta.Description = content
	}
}

// extractText recursively extracts visible text from an HTML node.
func extractNodeText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (c.Data == "script" || c.Data == "style") {
			continue
		}
		text := extractNodeText(c)
		if text != "" {
			// Add paragraph breaks for block elements
			if c.Type == html.ElementNode {
				switch c.Data {
				case "p", "div", "br", "h1", "h2", "h3", "h4", "h5", "h6", "li", "section":
					sb.WriteString("\n\n")
				}
			}
			sb.WriteString(text)
		}
	}
	return sb.String()
}

// sanitizeFileName removes characters unsafe for filenames.
func sanitizeFileName(name string) string {
	replacer := strings.NewReplacer(
		"/", "", "\\", "", ":", "", "*", "",
		"?", "", "\"", "", "<", "", ">", "", "|", "",
	)
	result := replacer.Replace(name)
	// Trim and limit length
	result = strings.TrimSpace(result)
	if len(result) > 200 {
		result = result[:200]
	}
	if result == "" {
		result = "untitled"
	}
	return result
}

// isWeChatURL checks if a URL is a WeChat article.
func isWeChatURL(rawURL string) bool {
	return strings.Contains(rawURL, "mp.weixin.qq.com") || strings.Contains(rawURL, "weixin.qq.com/s/")
}

// FetchViaJina fetches a URL via Jina Reader API and returns metadata + markdown body.
func FetchViaJina(ctx context.Context, rawURL string) (*LinkMetadata, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	jinaURL := "https://r.jina.ai/" + rawURL
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jinaURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/plain")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Jina HTTP %d", resp.StatusCode)
	}

	meta := &LinkMetadata{}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	// Parse Jina header lines: "Title:", "URL Source:", "Published Time:", then "Markdown Content:"
	inBody := false
	var body strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if inBody {
			body.WriteString(line)
			body.WriteString("\n")
			continue
		}
		if strings.HasPrefix(line, "Title: ") {
			meta.Title = strings.TrimPrefix(line, "Title: ")
		} else if strings.HasPrefix(line, "Published Time: ") {
			meta.Published = strings.TrimPrefix(line, "Published Time: ")
		} else if line == "Markdown Content:" {
			inBody = true
		}
	}

	if meta.Title == "" {
		meta.Title = rawURL
	}
	meta.Body = strings.TrimSpace(body.String())

	// Check for Jina failure (CAPTCHA, empty content)
	if meta.Body == "" || strings.Contains(meta.Body, "环境异常") || strings.Contains(meta.Body, "CAPTCHA") {
		return nil, fmt.Errorf("Jina returned empty or blocked content")
	}

	return meta, nil
}

// SaveLinkToLinkhoard fetches a URL and saves it as a Linkhoard-compatible markdown file.
// WeChat articles use direct fetch with browser headers; other sites use Jina Reader.
func SaveLinkToLinkhoard(ctx context.Context, saveDir, rawURL string) (string, error) {
	var meta *LinkMetadata
	var err error

	if isWeChatURL(rawURL) {
		meta, err = FetchLinkMetadata(ctx, rawURL)
	} else {
		meta, err = FetchViaJina(ctx, rawURL)
		if err != nil {
			// Fallback to direct fetch
			log.Printf("[linkhoard] Jina failed (%v), falling back to direct fetch", err)
			meta, err = FetchLinkMetadata(ctx, rawURL)
		}
	}
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}

	// Ensure save directory exists
	if err := os.MkdirAll(saveDir, 0o755); err != nil {
		return "", fmt.Errorf("create dir: %w", err)
	}

	// Build frontmatter
	title := sanitizeFileName(meta.Title)
	created := time.Now().UTC().Format(time.RFC3339)
	itemID := uuid.New().String()

	// Normalize body text
	body := strings.TrimSpace(meta.Body)
	// Collapse excessive newlines
	for strings.Contains(body, "\n\n\n") {
		body = strings.ReplaceAll(body, "\n\n\n", "\n\n")
	}

	// Build author field
	authorField := "author: []\n"
	if meta.Author != "" {
		authorField = fmt.Sprintf("author:\n  - '[[%s]]'\n", meta.Author)
	}

	// Build markdown content
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("title: '%s'\n", strings.ReplaceAll(meta.Title, "'", "''")))
	sb.WriteString(fmt.Sprintf("source: '%s'\n", rawURL))
	sb.WriteString(fmt.Sprintf("published: '%s'\n", meta.Published))
	sb.WriteString(fmt.Sprintf("created: '%s'\n", created))
	sb.WriteString(fmt.Sprintf("description: '%s'\n", strings.ReplaceAll(meta.Description, "'", "''")))
	if meta.OGImage != "" {
		sb.WriteString(fmt.Sprintf("openGraphImage: '%s'\n", meta.OGImage))
	}
	sb.WriteString(authorField)
	sb.WriteString("---\n\n")
	if body != "" {
		sb.WriteString(body)
		sb.WriteString("\n")
	}

	// Write markdown file
	filePath := filepath.Join(saveDir, title+".md")
	if err := os.WriteFile(filePath, []byte(sb.String()), 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	// Write sidecar
	sidecarPath := filePath + ".sidecar.md"
	sidecarContent := fmt.Sprintf("---\nid: %s\n---\n", itemID)
	if err := os.WriteFile(sidecarPath, []byte(sidecarContent), 0o644); err != nil {
		log.Printf("[linkhoard] failed to write sidecar: %v", err)
	}

	log.Printf("[linkhoard] saved %q to %s", meta.Title, filePath)
	return meta.Title, nil
}
