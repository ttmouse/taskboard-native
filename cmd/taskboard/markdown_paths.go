package main

import (
	"net/url"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// linkifyAbsolutePaths makes plain POSIX paths clickable in rendered Markdown
// without changing the stored comment or task text. Markdown links and code
// remain untouched.
func linkifyAbsolutePaths(src string) string {
	var out strings.Builder
	inFence := byte(0)
	fenceLen := 0
	for len(src) > 0 {
		line := src
		if i := strings.IndexByte(src, '\n'); i >= 0 {
			line, src = src[:i+1], src[i+1:]
		} else {
			src = ""
		}
		content := strings.TrimSuffix(line, "\n")
		trimmed := strings.TrimLeft(content, " ")
		if len(content)-len(trimmed) <= 3 && len(trimmed) >= 3 && (trimmed[0] == '`' || trimmed[0] == '~') {
			marker := trimmed[0]
			run := 0
			for run < len(trimmed) && trimmed[run] == marker {
				run++
			}
			if inFence == 0 && run >= 3 {
				inFence, fenceLen = marker, run
			} else if inFence == marker && run >= fenceLen && strings.TrimSpace(trimmed[run:]) == "" {
				inFence, fenceLen = 0, 0
			}
		}
		if inFence != 0 {
			out.WriteString(line)
		} else {
			out.WriteString(linkifyMarkdownLine(content))
			if strings.HasSuffix(line, "\n") {
				out.WriteByte('\n')
			}
		}
	}
	return out.String()
}

func linkifyMarkdownLine(line string) string {
	var out strings.Builder
	for i := 0; i < len(line); {
		if line[i] == '`' {
			run := byteRun(line, i, '`')
			if end := matchingRun(line, i+run, '`', run); end >= 0 {
				out.WriteString(line[i : end+run])
				i = end + run
				continue
			}
			out.WriteString(line[i:])
			break
		}
		if line[i] == '[' {
			if end := markdownLinkEnd(line, i); end > i {
				out.WriteString(line[i:end])
				i = end
				continue
			}
		}
		if line[i] == '/' && pathBoundaryBefore(line, i) {
			end := i + 1
			for end < len(line) {
				r, size := utf8.DecodeRuneInString(line[end:])
				if !pathRune(r) {
					break
				}
				end += size
			}
			if end > i+1 && line[end-1] == '.' && (end == len(line) || line[end] == ' ' || line[end] == '\t') {
				end-- // Keep a sentence-ending period outside the link.
			}
			path := line[i:end]
			if len(path) > 1 && !strings.HasPrefix(path, "/api/") {
				dest := (&url.URL{Scheme: "file", Path: filepath.Clean(path)}).String()
				out.WriteByte('[')
				out.WriteString(path)
				out.WriteString("](")
				out.WriteString(dest)
				out.WriteByte(')')
				i = end
				continue
			}
		}
		out.WriteByte(line[i])
		i++
	}
	return out.String()
}

func byteRun(s string, start int, b byte) int {
	i := start
	for i < len(s) && s[i] == b {
		i++
	}
	return i - start
}

func matchingRun(s string, start int, b byte, size int) int {
	for i := start; i < len(s); {
		if s[i] != b {
			i++
			continue
		}
		run := byteRun(s, i, b)
		if run == size {
			return i
		}
		i += run
	}
	return -1
}

func markdownLinkEnd(s string, start int) int {
	depth := 0
	close := -1
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				close = i
				i = len(s)
			}
		}
	}
	if close < 0 || close+1 >= len(s) || s[close+1] != '(' {
		return -1
	}
	depth = 1
	for i := close + 2; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

func pathBoundaryBefore(s string, i int) bool {
	if i == 0 {
		return true
	}
	prev, _ := utf8.DecodeLastRuneInString(s[:i])
	if prev == '/' || prev == ':' || pathRune(prev) {
		return false
	}
	return true
}

func pathRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || strings.ContainsRune("._~-/%+", r)
}
