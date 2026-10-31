package main

import (
	"strings"

	"github.com/egoist/mygo/ui/markdown/parser"
)

// bodyHasMarkdownTable reports whether body looks like it contains a GFM
// pipe table (header row + dash separator).
func bodyHasMarkdownTable(body string) bool {
	lines := strings.Split(body, "\n")
	for i := 0; i+1 < len(lines); i++ {
		a := strings.TrimSpace(lines[i])
		b := strings.TrimSpace(lines[i+1])
		if !strings.Contains(a, "|") || !strings.Contains(b, "|") {
			continue
		}
		sep := strings.ReplaceAll(strings.ReplaceAll(b, ":", ""), " ", "")
		sep = strings.Trim(sep, "|")
		if sep == "" {
			continue
		}
		ok := true
		for _, part := range strings.Split(sep, "|") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if strings.Trim(part, "-") != "" {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// bodyTableSpan is a byte range [Start, End) of a top-level GFM table in
// the body source, covering full lines so splice round-trips pipe markup.
type bodyTableSpan struct {
	Start, End int
}

// bodyTableSpans returns top-level table ranges in document order.
// parser.Block.End for tables often stops at the last cell's text, so each
// End is extended through the rest of that line (and its newline).
func bodyTableSpans(body string) []bodyTableSpan {
	if body == "" || !strings.Contains(body, "|") {
		return nil
	}
	doc := parser.Parse(body)
	var out []bodyTableSpan
	for i := 1; i < doc.Block(0).Next; i = doc.Block(i).Next {
		b := doc.Block(i)
		if b.Kind != parser.Table {
			continue
		}
		start, end := b.Start, extendToLineEnd(body, b.End)
		if start < 0 || end > len(body) || start >= end {
			continue
		}
		out = append(out, bodyTableSpan{Start: start, End: end})
	}
	return out
}

// extendToLineEnd moves end past the remainder of its line and one newline.
func extendToLineEnd(src string, end int) int {
	if end < 0 {
		return 0
	}
	if end > len(src) {
		return len(src)
	}
	for end < len(src) && src[end] != '\n' {
		end++
	}
	if end < len(src) && src[end] == '\n' {
		end++
	}
	return end
}

// spliceBodyRange replaces body[start:end] with repl.
func spliceBodyRange(body string, start, end int, repl string) string {
	if start < 0 {
		start = 0
	}
	if end > len(body) {
		end = len(body)
	}
	if start > end {
		start, end = end, start
	}
	if start > len(body) {
		start = len(body)
	}
	return body[:start] + repl + body[end:]
}
