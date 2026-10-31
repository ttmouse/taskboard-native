package main

import "strings"

// bodyHasMarkdownTable reports whether body looks like it contains a GFM
// pipe table (header row + dash separator). Used to keep a rendered preview
// while the source editor is open so tables do not "disappear" into pipes.
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
