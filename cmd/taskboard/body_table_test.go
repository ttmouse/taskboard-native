package main

import "testing"

func TestBodyHasMarkdownTable(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"plain text\nno table", false},
		{"| a | b |\n| --- | --- |\n| 1 | 2 |", true},
		{"|a|b|\n|---|---|\n|1|2|", true},
		{"| a | b |\n| :---: | ---: |\n| 1 | 2 |", true},
		{"not | a table alone", false},
		{"| only header |", false},
	}
	for _, tc := range cases {
		if got := bodyHasMarkdownTable(tc.in); got != tc.want {
			t.Fatalf("bodyHasMarkdownTable(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}
