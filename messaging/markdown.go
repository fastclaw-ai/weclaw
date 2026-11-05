package messaging

import "strings"

// StreamingMarkdownFilter strips unsupported markdown syntax while preserving
// constructs that Weixin text displays acceptably. It mirrors the official
// openclaw-weixin 2.4.x streaming filter closely enough for chunked output.
type StreamingMarkdownFilter struct {
	buf   string
	fence bool
	sol   bool
	inl   *inlineState
}

type inlineState struct {
	typ string
	acc string
}

// NewStreamingMarkdownFilter creates a markdown filter for outbound text.
func NewStreamingMarkdownFilter() *StreamingMarkdownFilter {
	return &StreamingMarkdownFilter{sol: true}
}

// Feed appends a text chunk and returns any filtered text now safe to emit.
func (f *StreamingMarkdownFilter) Feed(delta string) string {
	f.buf += delta
	return f.pump(false)
}

// Flush emits any buffered text at end of stream.
func (f *StreamingMarkdownFilter) Flush() string {
	return f.pump(true)
}

// FilterMarkdown applies the streaming markdown filter to a complete string.
func FilterMarkdown(text string) string {
	f := NewStreamingMarkdownFilter()
	return f.Feed(text) + f.Flush()
}

// MarkdownToPlainText is kept for compatibility. It now performs the
// openclaw-weixin 2.4.x style markdown filtering instead of full stripping.
func MarkdownToPlainText(text string) string {
	return strings.TrimSpace(FilterMarkdown(text))
}

func (f *StreamingMarkdownFilter) pump(eof bool) string {
	var out strings.Builder
	for f.buf != "" {
		bufLen := len(f.buf)
		sol := f.sol
		fence := f.fence
		inl := f.inl

		switch {
		case f.fence:
			out.WriteString(f.pumpFence(eof))
		case f.inl != nil:
			out.WriteString(f.pumpInline(eof))
		case f.sol:
			out.WriteString(f.pumpSOL(eof))
		default:
			out.WriteString(f.pumpBody(eof))
		}

		if len(f.buf) == bufLen && f.sol == sol && f.fence == fence && f.inl == inl {
			break
		}
	}

	if eof && f.inl != nil {
		markers := map[string]string{
			"image":   "![",
			"bold3":   "***",
			"italic":  "*",
			"ubold3":  "___",
			"uitalic": "_",
		}
		out.WriteString(markers[f.inl.typ])
		out.WriteString(f.inl.acc)
		f.inl = nil
	}

	return out.String()
}

func (f *StreamingMarkdownFilter) pumpFence(eof bool) string {
	if f.sol {
		if len(f.buf) < 3 && !eof {
			return ""
		}
		if strings.HasPrefix(f.buf, "```") {
			if nl := strings.IndexByte(f.buf[3:], '\n'); nl != -1 {
				nl += 3
				f.fence = false
				line := f.buf[:nl+1]
				f.buf = f.buf[nl+1:]
				f.sol = true
				return line
			}
			if eof {
				f.fence = false
				line := f.buf
				f.buf = ""
				return line
			}
			return ""
		}
		f.sol = false
	}

	if nl := strings.IndexByte(f.buf, '\n'); nl != -1 {
		chunk := f.buf[:nl+1]
		f.buf = f.buf[nl+1:]
		f.sol = true
		return chunk
	}
	chunk := f.buf
	f.buf = ""
	return chunk
}

func (f *StreamingMarkdownFilter) pumpSOL(eof bool) string {
	b := f.buf
	if b[0] == '\n' {
		f.buf = b[1:]
		return "\n"
	}

	if b[0] == '`' {
		if len(b) < 3 && !eof {
			return ""
		}
		if strings.HasPrefix(b, "```") {
			if nl := strings.IndexByte(b[3:], '\n'); nl != -1 {
				nl += 3
				f.fence = true
				line := b[:nl+1]
				f.buf = b[nl+1:]
				f.sol = true
				return line
			}
			if eof {
				f.buf = ""
				return b
			}
			return ""
		}
		f.sol = false
		return ""
	}

	if b[0] == '>' {
		f.sol = false
		return ""
	}

	if b[0] == '#' {
		n := 0
		for n < len(b) && b[n] == '#' {
			n++
		}
		if n == len(b) && !eof {
			return ""
		}
		if n >= 5 && n <= 6 && n < len(b) && b[n] == ' ' {
			f.buf = b[n+1:]
			f.sol = false
			return ""
		}
		f.sol = false
		return ""
	}

	if b[0] == ' ' || b[0] == '\t' {
		if !eof && strings.Trim(b, " \t") == "" {
			return ""
		}
		f.sol = false
		return ""
	}

	if b[0] == '-' || b[0] == '*' || b[0] == '_' {
		ch := b[0]
		j := 0
		for j < len(b) && (b[j] == ch || b[j] == ' ') {
			j++
		}
		if j == len(b) && !eof {
			return ""
		}
		if j == len(b) || b[j] == '\n' {
			count := 0
			for k := 0; k < j; k++ {
				if b[k] == ch {
					count++
				}
			}
			if count >= 3 {
				if j < len(b) {
					line := b[:j+1]
					f.buf = b[j+1:]
					f.sol = true
					return line
				}
				f.buf = ""
				return b
			}
		}
		f.sol = false
		return ""
	}

	f.sol = false
	return ""
}

func (f *StreamingMarkdownFilter) pumpBody(eof bool) string {
	for i := 0; i < len(f.buf); {
		c := f.buf[i]
		if c == '\n' {
			out := f.buf[:i+1]
			f.buf = f.buf[i+1:]
			f.sol = true
			return out
		}
		if c == '!' && i+1 < len(f.buf) && f.buf[i+1] == '[' {
			out := f.buf[:i]
			f.buf = f.buf[i+2:]
			f.inl = &inlineState{typ: "image"}
			return out
		}
		if c == '~' {
			i++
			continue
		}
		if c == '*' {
			if i+2 < len(f.buf) && f.buf[i+1] == '*' && f.buf[i+2] == '*' {
				out := f.buf[:i]
				f.buf = f.buf[i+3:]
				f.inl = &inlineState{typ: "bold3"}
				return out
			}
			if i+1 < len(f.buf) && f.buf[i+1] == '*' {
				i += 2
				continue
			}
			if i+1 < len(f.buf) && f.buf[i+1] != ' ' && f.buf[i+1] != '\n' {
				out := f.buf[:i]
				f.buf = f.buf[i+1:]
				f.inl = &inlineState{typ: "italic"}
				return out
			}
			i++
			continue
		}
		if c == '_' {
			if i+2 < len(f.buf) && f.buf[i+1] == '_' && f.buf[i+2] == '_' {
				out := f.buf[:i]
				f.buf = f.buf[i+3:]
				f.inl = &inlineState{typ: "ubold3"}
				return out
			}
			if i+1 < len(f.buf) && f.buf[i+1] == '_' {
				i += 2
				continue
			}
			if i+1 < len(f.buf) && f.buf[i+1] != ' ' && f.buf[i+1] != '\n' {
				out := f.buf[:i]
				f.buf = f.buf[i+1:]
				f.inl = &inlineState{typ: "uitalic"}
				return out
			}
			i++
			continue
		}
		i++
	}

	hold := 0
	if !eof {
		switch {
		case strings.HasSuffix(f.buf, "**"), strings.HasSuffix(f.buf, "__"):
			hold = 2
		case strings.HasSuffix(f.buf, "*"), strings.HasSuffix(f.buf, "_"), strings.HasSuffix(f.buf, "!"):
			hold = 1
		}
	}
	out := f.buf[:len(f.buf)-hold]
	if hold > 0 {
		f.buf = f.buf[len(f.buf)-hold:]
	} else {
		f.buf = ""
	}
	return out
}

func (f *StreamingMarkdownFilter) pumpInline(eof bool) string {
	if f.inl == nil {
		return ""
	}
	f.inl.acc += f.buf
	f.buf = ""

	switch f.inl.typ {
	case "bold3":
		return f.closeDelimited("***")
	case "ubold3":
		return f.closeDelimited("___")
	case "italic":
		return f.closeItalic("*")
	case "uitalic":
		return f.closeItalic("_")
	case "image":
		return f.closeImage(eof)
	}
	return ""
}

func (f *StreamingMarkdownFilter) closeDelimited(marker string) string {
	idx := strings.Index(f.inl.acc, marker)
	if idx == -1 {
		return ""
	}
	content := f.inl.acc[:idx]
	f.buf = f.inl.acc[idx+len(marker):]
	f.inl = nil
	if containsCJK(content) {
		return content
	}
	return marker + content + marker
}

func (f *StreamingMarkdownFilter) closeItalic(marker string) string {
	for j := 0; j < len(f.inl.acc); j++ {
		if f.inl.acc[j] == '\n' {
			out := marker + f.inl.acc[:j+1]
			f.buf = f.inl.acc[j+1:]
			f.inl = nil
			f.sol = true
			return out
		}
		if f.inl.acc[j] == marker[0] {
			if j+1 < len(f.inl.acc) && f.inl.acc[j+1] == marker[0] {
				j++
				continue
			}
			content := f.inl.acc[:j]
			f.buf = f.inl.acc[j+1:]
			f.inl = nil
			if containsCJK(content) {
				return content
			}
			return marker + content + marker
		}
	}
	return ""
}

func (f *StreamingMarkdownFilter) closeImage(_ bool) string {
	cb := strings.IndexByte(f.inl.acc, ']')
	if cb == -1 || cb+1 >= len(f.inl.acc) {
		return ""
	}
	if f.inl.acc[cb+1] != '(' {
		out := "![" + f.inl.acc[:cb+1]
		f.buf = f.inl.acc[cb+1:]
		f.inl = nil
		return out
	}
	cp := strings.IndexByte(f.inl.acc[cb+2:], ')')
	if cp == -1 {
		return ""
	}
	cp += cb + 2
	f.buf = f.inl.acc[cp+1:]
	f.inl = nil
	return ""
}

func containsCJK(text string) bool {
	for _, r := range text {
		if (r >= 0x2E80 && r <= 0x9FFF) ||
			(r >= 0xAC00 && r <= 0xD7AF) ||
			(r >= 0xF900 && r <= 0xFAFF) {
			return true
		}
	}
	return false
}
