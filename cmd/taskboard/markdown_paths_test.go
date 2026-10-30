package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

func TestLinkifyAbsolutePaths(t *testing.T) {
	path := "/Users/douba/Projects/taskboard-native/AGENTS.md"
	linked := "[" + path + "](file:///Users/douba/Projects/taskboard-native/AGENTS.md)"
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "bare path", in: path, want: linked},
		{name: "path in prose", in: "See " + path + " for details.", want: "See " + linked + " for details."},
		{name: "sentence punctuation", in: "Open " + path + ".", want: "Open " + linked + "."},
		{name: "fullwidth punctuation and following text", in: "派发 codex 执行（工作目录 " + path + "；发起人 豆爸）", want: "派发 codex 执行（工作目录 " + linked + "；发起人 豆爸）"},
		{name: "inline code", in: "`" + path + "`", want: "`" + path + "`"},
		{name: "fenced code", in: "```text\n" + path + "\n```", want: "```text\n" + path + "\n```"},
		{name: "existing link", in: "[guide](" + path + ")", want: "[guide](" + path + ")"},
		{name: "app image route", in: "/api/images/abc123", want: "/api/images/abc123"},
		{name: "url path", in: "https://example.com/a/b", want: "https://example.com/a/b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := linkifyAbsolutePaths(tt.in); got != tt.want {
				t.Fatalf("linkifyAbsolutePaths(%q) = %q; want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRenderedBarePathClick(t *testing.T) {
	path := "/Users/douba/Projects/taskboard-native/AGENTS.md"
	var clicked string
	tester := ui.NewTester(func(c *ui.Context) {
		markdown.View(c, linkifyAbsolutePaths(path), &markdown.Options{
			OnLink: func(target string) { clicked = target },
		})
	}, 600, 300)
	if err := tester.Click(path); err != nil {
		t.Fatal(err)
	}
	if clicked != "file:///Users/douba/Projects/taskboard-native/AGENTS.md" {
		t.Fatalf("click target = %q", clicked)
	}
}
