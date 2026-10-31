package main

import (
	"github.com/egoist/mygo/ui"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxWorkspaceMentionTargets = 500

type workspaceMentionTarget struct {
	Path  string
	IsDir bool
}

func workspaceMentionPaths(text string) []string {
	var paths []string
	for rest := text; ; {
		start := strings.Index(rest, "@{")
		if start < 0 {
			return paths
		}
		end := strings.IndexByte(rest[start+2:], '}')
		if end < 0 {
			return paths
		}
		end += start + 2
		paths = append(paths, filepath.ToSlash(filepath.Clean(filepath.FromSlash(rest[start+2:end]))))
		rest = rest[end+1:]
	}
}

// workspaceMentionTargets returns bounded, relative paths without following
// symlinks or entering common generated/dependency directories.
func workspaceMentionTargets(root string) []workspaceMentionTarget {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	root, err := filepath.Abs(root)
	if err != nil || root == "." {
		return nil
	}
	var out []workspaceMentionTarget
	var walk func(string, string, int)
	walk = func(abs, rel string, depth int) {
		if len(out) >= maxWorkspaceMentionTargets || depth > 8 {
			return
		}
		entries, err := os.ReadDir(abs)
		if err != nil {
			return
		}
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, ".") || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			if entry.IsDir() && (name == "node_modules" || name == "vendor" || name == ".git") {
				continue
			}
			childRel := filepath.ToSlash(filepath.Join(rel, name))
			isDir := entry.IsDir()
			out = append(out, workspaceMentionTarget{Path: childRel, IsDir: isDir})
			if isDir {
				walk(filepath.Join(abs, name), childRel, depth+1)
			}
			if len(out) >= maxWorkspaceMentionTargets {
				return
			}
		}
	}
	walk(root, "", 0)
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Path) < strings.ToLower(out[j].Path)
	})
	return out
}

// workspaceMentionQuery recognizes a trailing @query token. It deliberately
// ignores email addresses and mentions embedded in paths or prose words.
func workspaceMentionQuery(text string) (string, bool) {
	trimmed := strings.TrimRight(text, " \t\n")
	i := strings.LastIndex(trimmed, "@")
	if i < 0 {
		return "", false
	}
	if i > 0 {
		prev := trimmed[i-1]
		if prev != ' ' && prev != '\n' && prev != '\t' {
			return "", false
		}
	}
	query := trimmed[i+1:]
	if strings.ContainsAny(query, " \t\n{}") {
		return "", false
	}
	return query, true
}

func insertWorkspaceMention(text, rel string) string {
	// Kept for tests and callers that still embed the token in plain text.
	trimmed := clearWorkspaceMentionQuery(text)
	token := "@{" + filepath.ToSlash(rel) + "}"
	if trimmed == "" {
		return token + " "
	}
	return trimmed + " " + token + " "
}

// clearWorkspaceMentionQuery drops a trailing @search token so the composer
// can keep file picks on badge chips instead of duplicating the path in text.
func clearWorkspaceMentionQuery(text string) string {
	trimmed := strings.TrimRight(text, " \t\n")
	if _, ok := workspaceMentionQuery(trimmed); ok {
		trimmed = strings.TrimRight(trimmed[:strings.LastIndex(trimmed, "@")], " \t\n")
	}
	return trimmed
}

func appendWorkspaceMention(files []string, rel string) []string {
	rel = filepath.ToSlash(rel)
	for _, existing := range files {
		if existing == rel {
			return files
		}
	}
	return append(files, rel)
}

// withWorkspaceMentions appends @{rel} tokens for send/create while the draft
// input itself stays free of those tokens (chips carry the selection).
func withWorkspaceMentions(text string, files []string) string {
	text = strings.TrimSpace(text)
	parts := make([]string, 0, len(files)+1)
	if text != "" {
		parts = append(parts, text)
	}
	for _, rel := range files {
		token := "@{" + filepath.ToSlash(rel) + "}"
		if text != "" && strings.Contains(text, token) {
			continue
		}
		parts = append(parts, token)
	}
	return strings.Join(parts, " ")
}

// stripWorkspaceMentionTokens removes @{…} tokens from draft text so the
// TextArea does not repeat paths already shown as chips.
func stripWorkspaceMentionTokens(text string) string {
	var out strings.Builder
	for rest := text; ; {
		start := strings.Index(rest, "@{")
		if start < 0 {
			out.WriteString(rest)
			break
		}
		out.WriteString(rest[:start])
		end := strings.IndexByte(rest[start+2:], '}')
		if end < 0 {
			out.WriteString(rest[start:])
			break
		}
		end += start + 2
		rest = rest[end+1:]
	}
	return strings.Join(strings.Fields(out.String()), " ")
}

// adoptWorkspaceMentionTokens moves any @{rel} still sitting in draft text
// onto the chip list and clears them from the TextArea.
func adoptWorkspaceMentionTokens(text string, files []string) (string, []string) {
	paths := workspaceMentionPaths(text)
	if len(paths) == 0 {
		return text, files
	}
	for _, rel := range paths {
		files = appendWorkspaceMention(files, rel)
	}
	return stripWorkspaceMentionTokens(text), files
}

// expandWorkspaceMentions converts selected relative tokens to ordinary
// Markdown file links. Invalid or missing paths stay as plain text.
func expandWorkspaceMentions(text, root string) string {
	if root == "" {
		return text
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return text
	}
	if resolvedRoot, err := filepath.EvalSymlinks(root); err == nil {
		root = resolvedRoot
	}
	var out strings.Builder
	for rest := text; ; {
		start := strings.Index(rest, "@{")
		if start < 0 {
			out.WriteString(rest)
			break
		}
		out.WriteString(rest[:start])
		end := strings.IndexByte(rest[start+2:], '}')
		if end < 0 {
			out.WriteString(rest[start:])
			break
		}
		end += start + 2
		rel := filepath.Clean(filepath.FromSlash(rest[start+2 : end]))
		abs := filepath.Join(root, rel)
		inside, err := filepath.Rel(root, abs)
		if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) || filepath.IsAbs(inside) {
			out.WriteString(rest[start : end+1])
			rest = rest[end+1:]
			continue
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			out.WriteString(rest[start : end+1])
			rest = rest[end+1:]
			continue
		}
		inside, err = filepath.Rel(root, resolved)
		if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) || filepath.IsAbs(inside) {
			out.WriteString(rest[start : end+1])
			rest = rest[end+1:]
			continue
		}
		label := filepath.ToSlash(rel)
		if info, err := os.Stat(resolved); err == nil && info.IsDir() {
			label = "📁 " + label
		} else {
			label = "📄 " + label
		}
		dest := (&url.URL{Scheme: "file", Path: abs}).String()
		out.WriteByte('[')
		out.WriteString(strings.ReplaceAll(strings.ReplaceAll(label, "\\", "\\\\"), "]", "\\]"))
		out.WriteString("](")
		out.WriteString(dest)
		out.WriteByte(')')
		rest = rest[end+1:]
	}
	return out.String()
}

// displayWorkspaceMentions gives card titles a compact visible label while
// keeping the stored relative token intact for edits and synchronization.
func displayWorkspaceMentions(text, root string) string {
	if root == "" {
		return text
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return text
	}
	if resolvedRoot, err := filepath.EvalSymlinks(root); err == nil {
		root = resolvedRoot
	}
	var out strings.Builder
	for rest := text; ; {
		start := strings.Index(rest, "@{")
		if start < 0 {
			out.WriteString(rest)
			break
		}
		out.WriteString(rest[:start])
		end := strings.IndexByte(rest[start+2:], '}')
		if end < 0 {
			out.WriteString(rest[start:])
			break
		}
		end += start + 2
		token := rest[start : end+1]
		rel := filepath.Clean(filepath.FromSlash(rest[start+2 : end]))
		abs := filepath.Join(root, rel)
		resolved, err := filepath.EvalSymlinks(abs)
		inside, relErr := filepath.Rel(root, resolved)
		if err != nil || relErr != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) || filepath.IsAbs(inside) {
			out.WriteString(token)
		} else if info, err := os.Stat(resolved); err == nil && info.IsDir() {
			out.WriteString("📁 " + filepath.ToSlash(rel))
		} else {
			out.WriteString("📄 " + filepath.ToSlash(rel))
		}
		rest = rest[end+1:]
	}
	return out.String()
}

// workspaceMentionInputWidth sizes a text field to its content so file chips
// can sit beside it. Empty drafts keep a readable minimum width.
func workspaceMentionInputWidth(c *ui.Context, t *ui.Theme, text string, fontSize float32) float32 {
	const minW float32 = 120
	pad := t.Space(4)
	sample := text
	if sample == "" {
		sample = " "
	}
	w, _ := c.MeasureText(0, ui.Span{Text: sample, Size: fontSize})
	if w+pad < minW {
		return minW
	}
	return w + pad
}
