package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown/parser"
)

func TestFitMarkdownTableHandlesTableKind(t *testing.T) {
	src := "| 左列 | 中列很长很长很长很长 | 右列 |\n| --- | --- | --- |\n| a | b | c |\n"
	doc := parser.Parse(src)
	tableAt := -1
	for i := 1; i < doc.Block(0).Next; {
		b := doc.Block(i)
		if b.Kind == parser.Table {
			tableAt = i
			break
		}
		if b.Next <= i {
			break
		}
		i = b.Next
	}
	if tableAt < 0 {
		t.Fatal("parser did not produce a Table block")
	}
	board := &board{}
	var drew bool
	tt := ui.NewTester(func(c *ui.Context) {
		drew = board.fitMarkdownTable(c, doc, tableAt)
	}, 360, 240)
	tt.Frame()
	if !drew {
		t.Fatal("fitMarkdownTable returned false for a Table block")
	}
	if !tt.HasText("左列") || !tt.HasText("a") {
		t.Fatal("fit table did not show cell text")
	}
}

func TestDetailBodyShowsWideTableCells(t *testing.T) {
	isolatePrefs(t)
	f := seedBoard()
	wide := "| A | B | C | D | E |\n| --- | --- | --- | --- | --- |\n| " +
		strings.Repeat("宽", 8) + " | mid | x | y | z |\n"
	f.tasks[0].Description = wide
	b := newBoard(f)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
	b.openDetail(*b.task("t-1"))
	settle(t, tt)
	if !tt.HasText("A") || !tt.HasText("mid") {
		t.Fatalf("wide table cells missing from detail; body=%q", b.detail.body)
	}
}
