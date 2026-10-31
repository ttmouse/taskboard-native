package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

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

func TestBodyTableSpansAndSplice(t *testing.T) {
	body := "前言\n\n| a | b |\n| --- | --- |\n| 1 | 2 |\n\n后记\n\n| x | y |\n| --- | --- |\n| 3 | 4 |\n"
	spans := bodyTableSpans(body)
	if len(spans) != 2 {
		t.Fatalf("spans=%v want 2", spans)
	}
	first := body[spans[0].Start:spans[0].End]
	if !bodyHasMarkdownTable(first) {
		t.Fatalf("first span not a table: %q", first)
	}
	if got := body[spans[1].Start:spans[1].End]; !bodyHasMarkdownTable(got) {
		t.Fatalf("second span not a table: %q", got)
	}
	if spans[0].Start != len("前言\n\n") {
		t.Fatalf("first start=%d want %d (%q before)", spans[0].Start, len("前言\n\n"), body[:spans[0].Start])
	}

	repl := "| A | B |\n| --- | --- |\n| 9 | 8 |\n"
	out := spliceBodyRange(body, spans[0].Start, spans[0].End, repl)
	wantPrefix := "前言\n\n" + repl
	if len(out) < len(wantPrefix) || out[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("splice prefix=%q want prefix %q", out, wantPrefix)
	}
	spans2 := bodyTableSpans(out)
	if len(spans2) != 2 {
		t.Fatalf("after splice spans=%v", spans2)
	}
	out2 := spliceBodyRange(out, spans2[1].Start, spans2[1].End, "| X | Y |\n| --- | --- |\n| 0 | 0 |\n")
	if len(bodyTableSpans(out2)) != 2 {
		t.Fatalf("final spans=%v", bodyTableSpans(out2))
	}
}

func TestSpliceBodyRangeEdges(t *testing.T) {
	if got := spliceBodyRange("abc", 0, 3, "x"); got != "x" {
		t.Fatalf("got %q", got)
	}
	if got := spliceBodyRange("abc", 1, 2, "X"); got != "aXc" {
		t.Fatalf("got %q", got)
	}
	if got := spliceBodyRange("abc", 3, 3, "!"); got != "abc!" {
		t.Fatalf("got %q", got)
	}
}

func TestDetailTableBlockOpensSourceEditor(t *testing.T) {
	isolatePrefs(t)
	f := seedBoard()
	f.tasks[0].Description = "前言\n\n| a | b |\n| --- | --- |\n| 1 | 2 |\n\n后记"
	b := newBoard(f)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
	b.openDetail(*b.task("t-1"))
	settle(t, tt)
	if err := tt.Click("编辑表格"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !b.detail.editTable {
		t.Fatal("click did not open table block editor")
	}
	if b.detail.editBody {
		t.Fatal("table click must not open full-body editor")
	}
	if _, ok := tt.Find("表格编辑器"); !ok {
		t.Fatal("table source TextArea missing")
	}
	if !tt.HasText("后记") {
		t.Fatal("non-table blocks should stay rendered")
	}
	// Helper commit (Esc while unfocused would dismiss the whole detail overlay).
	b.detail.tableEdit = "| A | B |\n| --- | --- |\n| 9 | 8 |\n"
	b.detail.commitTableEdit()
	tt.Frame()
	if b.detail.editTable {
		t.Fatal("table editor still open after commit")
	}
	if !strings.Contains(b.detail.body, "| A | B |") {
		t.Fatalf("splice did not write back: %q", b.detail.body)
	}
	if !tt.HasText("A") || !tt.HasText("9") {
		t.Fatal("edited table should render after commit")
	}
}


func TestDetailTableEscCommitsWithoutClosingDetail(t *testing.T) {
	isolatePrefs(t)
	f := seedBoard()
	f.tasks[0].Description = "前言\n\n| a | b |\n| --- | --- |\n| 1 | 2 |\n\n后记"
	b := newBoard(f)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
	b.openDetail(*b.task("t-1"))
	settle(t, tt)
	if err := tt.Click("编辑表格"); err != nil {
		t.Fatal(err)
	}
	settle(t, tt)
	if !b.detail.editTable {
		t.Fatal("expected table editor open")
	}
	b.detail.tableEdit = "| A | B |\n| --- | --- |\n| 9 | 8 |\n"
	// Focused AutoFocus editor: Esc should commit via OnTextKey, not close detail.
	if !tt.Focused("表格编辑器") {
		t.Fatal("table editor should AutoFocus")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if b.detail == nil {
		t.Fatal("Esc must not dismiss detail while committing table edit")
	}
	if b.detail.editTable {
		t.Fatal("Esc should leave table edit mode")
	}
	if !strings.Contains(b.detail.body, "| A | B |") {
		t.Fatalf("Esc did not splice draft: %q", b.detail.body)
	}
	if !tt.HasText("后记") {
		t.Fatal("other blocks should stay rendered after Esc")
	}
}



func TestDetailPlainSegmentEditsInPlaceWithTables(t *testing.T) {
	isolatePrefs(t)
	f := seedBoard()
	f.tasks[0].Description = "前言\n\n| a | b |\n| --- | --- |\n| 1 | 2 |\n\n后记"
	b := newBoard(f)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
	b.openDetail(*b.task("t-1"))
	settle(t, tt)
	if err := tt.Click("编辑正文块"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !b.detail.editTable {
		t.Fatal("plain segment should open block editor")
	}
	if b.detail.editBody {
		t.Fatal("must not open full-body editor when tables exist")
	}
	if _, ok := tt.Find("正文块编辑器"); !ok {
		t.Fatal("plain block TextArea missing")
	}
	if b.detail.tableTop {
		t.Fatal("tableTop should clear after first frame (caret moved to head)")
	}
	if !tt.HasText("1") && !tt.HasText("a") {
		t.Fatal("table block should stay rendered while plain segment is edited")
	}
}

func TestDetailTableEditorCaretAtHead(t *testing.T) {
	isolatePrefs(t)
	f := seedBoard()
	f.tasks[0].Description = "前言\n\n| a | b |\n| --- | --- |\n| 1 | 2 |\n\n后记"
	b := newBoard(f)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
	b.openDetail(*b.task("t-1"))
	settle(t, tt)
	if err := tt.Click("编辑表格"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !b.detail.editTable {
		t.Fatal("expected table editor open")
	}
	if b.detail.tableTop {
		t.Fatal("tableTop should clear after first frame; caret would stay at tail")
	}
	if !tt.Focused("表格编辑器") {
		t.Fatal("table editor should AutoFocus")
	}
}
