package main

import "testing"

func TestCommentMarkdownPreservesNewlines(t *testing.T) {
	got := commentMarkdown("summary\ndetails: \\n- first\\n- second")
	want := "summary  \ndetails:  \n- first  \n- second"
	if got != want {
		t.Fatalf("commentMarkdown() = %q, want %q", got, want)
	}
}

func TestCommentMarkdownKeepsCodeLiteral(t *testing.T) {
	input := "inline `\\n`\n```go\nfmt.Println(\"\\n\")\n```\n"
	got := commentMarkdown(input)
	want := "inline `\\n`  \n```go\nfmt.Println(\"\\n\")\n```\n"
	if got != want {
		t.Fatalf("commentMarkdown() = %q, want %q", got, want)
	}
}
