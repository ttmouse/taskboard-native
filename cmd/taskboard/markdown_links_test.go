package main

import "testing"

func TestMarkdownExternalTarget(t *testing.T) {
	tests := []struct {
		name   string
		target string
		want   string
		ok     bool
	}{
		{name: "absolute local path", target: "/tmp/a file.md", want: "file:///tmp/a%20file.md", ok: true},
		{name: "file URL", target: "file:///tmp/a.md", want: "file:///tmp/a.md", ok: true},
		{name: "https link", target: "https://example.com/a", want: "https://example.com/a", ok: true},
		{name: "mailto link", target: "mailto:user@example.com", want: "mailto:user@example.com", ok: true},
		{name: "unsupported scheme", target: "javascript:alert(1)", ok: false},
		{name: "relative app route", target: "docs/readme.md", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := markdownExternalTarget(tt.target)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("markdownExternalTarget(%q) = %q, %v; want %q, %v", tt.target, got, ok, tt.want, tt.ok)
			}
		})
	}
}
