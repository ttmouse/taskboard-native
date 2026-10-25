package main

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// TestDetailSheetLeftEdge pins LOCAL-33: every row of the 任务详情 sheet —
// the sheet title, the task title, the body, the 移动到列 select, the 评论
// label, the comment cards (avatar, author, text), the composer, and the
// 归档 footer — starts on one left edge inside the dialog.
//
// Before the fix the rows disagreed by up to 16 px: the title and body
// carried the input's own inset, the select drew its hairline a step in,
// the comment card's surface and its author sat inside a wider padding,
// and the composer inherited the 写评论 input's inset. Each row is checked
// against the sheet title's own text box, so a future Padding() cannot
// quietly re-break the alignment.
func TestDetailSheetLeftEdge(t *testing.T) {
	dir := t.TempDir()
	// Isolate prefs: frames can saveUIPrefs, and the prefs path derives
	// from TASKBOARD_DB — point it inside this test's dir.
	t.Setenv("TASKBOARD_DB", filepath.Join(dir, "tb.db"))
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })

	b := newBoard(seedBoard())
	tt := ui.NewTester(b.view, 1280, 800)
	tt.SetDark(true)
	waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
	b.openDetail(*b.task("t-1"))
	waitFor(t, tt, "评论载入", func() bool { return b.detail != nil && !b.detail.loading })
	settle(t, tt)

	head, ok := tt.Find("任务详情")
	if !ok {
		t.Fatal("detail sheet title missing")
	}
	left := head.X

	for _, row := range []string{
		"任务标题",  // title input
		"任务正文",  // body text area
		"移动到列",  // select (its hairline is its box)
		"评论",    // section label
		"linus", // comment author, behind the avatar
		"写评论",   // composer input
		"归档任务",  // footer button
	} {
		r, found := tt.Find(row)
		if !found {
			t.Errorf("%s missing", row)
			continue
		}
		if d := r.X - left; d < -0.5 || d > 0.5 {
			t.Errorf("%s left %.1f vs sheet text left %.1f (Δ%.1f), want aligned", row, r.X, left, d)
		}
	}

	// The avatar's initial sits in the middle of a 20 px circle; the circle
	// is the comment card's first child, so its halo should start on the
	// sheet's edge.
	initial, ok := tt.Find("L")
	if !ok {
		t.Fatal("comment avatar missing")
	}
	if d := initial.X - left; d < -0.5 || d > 8 {
		t.Errorf("comment avatar initial left %.1f vs sheet text left %.1f (Δ%.1f), want the 20 px avatar flush left", initial.X, left, d)
	}
}
