package main

import (
	"strings"
)

// commentMarkdown keeps line breaks visible in comments. The Markdown view
// treats ordinary newlines as soft breaks (spaces), while some callers also
// store escaped "\\n" sequences instead of actual newlines.
func commentMarkdown(text string) string {
	lines := strings.SplitAfter(text, "\n")
	var out strings.Builder
	insideFence := false
	fenceChar := byte(0)
	fenceSize := 0

	for _, part := range lines {
		line := strings.TrimSuffix(part, "\n")
		hasNewline := strings.HasSuffix(part, "\n")
		trimmed := strings.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)
		marker, markerSize := markdownFence(trimmed)

		if insideFence {
			out.WriteString(line)
			if hasNewline {
				out.WriteByte('\n')
			}
			if marker == fenceChar && markerSize >= fenceSize && strings.TrimSpace(trimmed[markerSize:]) == "" {
				insideFence = false
			}
			continue
		}
		if indent <= 3 && marker != 0 {
			out.WriteString(line)
			if hasNewline {
				out.WriteByte('\n')
			}
			insideFence = true
			fenceChar, fenceSize = marker, markerSize
			continue
		}

		line = unescapeCommentLineBreaks(line)
		segments := strings.Split(line, "\n")
		for i, segment := range segments {
			out.WriteString(segment)
			if hasNewline || i < len(segments)-1 {
				if strings.TrimSpace(segment) != "" {
					spaces := len(segment) - len(strings.TrimRight(segment, " "))
					if spaces < 2 {
						out.WriteString(strings.Repeat(" ", 2-spaces))
					}
				}
				out.WriteByte('\n')
			}
		}
	}
	return out.String()
}

func markdownFence(line string) (byte, int) {
	if len(line) < 3 || line[0] != '`' && line[0] != '~' {
		return 0, 0
	}
	char := line[0]
	size := 0
	for size < len(line) && line[size] == char {
		size++
	}
	if size < 3 {
		return 0, 0
	}
	return char, size
}

func unescapeCommentLineBreaks(line string) string {
	var out strings.Builder
	inlineTicks := 0
	for i := 0; i < len(line); {
		if line[i] == '`' {
			end := i + 1
			for end < len(line) && line[end] == '`' {
				end++
			}
			ticks := end - i
			if inlineTicks == 0 {
				inlineTicks = ticks
			} else if ticks == inlineTicks {
				inlineTicks = 0
			}
			out.WriteString(line[i:end])
			i = end
			continue
		}
		if inlineTicks == 0 && line[i] == '\\' {
			end := i
			for end < len(line) && line[end] == '\\' {
				end++
			}
			count := end - i
			if end < len(line) && line[end] == 'n' && count%2 == 1 {
				out.WriteString(line[i : end-1])
				out.WriteByte('\n')
				i = end + 1
				continue
			}
			out.WriteString(line[i:end])
			i = end
			continue
		}
		out.WriteByte(line[i])
		i++
	}
	return out.String()
}
