package main

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Align plain text and outer surfaces while preserving the inner padding
// of filled containers. Negative select margins clip its rounded border.
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
		"任务标题", // title input
		"任务正文", // body text area
		"评论",   // section label
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

	for _, row := range []struct {
		label    string
		min, max float32
	}{
		{"移动到列", -0.5, 0.5},
		{"写评论", 9.5, 10.5},
		{b.detail.comments[0].Text, 11.5, 12.5},
	} {
		r, found := tt.Find(row.label)
		if !found {
			t.Errorf("%s missing", row.label)
			continue
		}
		if inset := r.X - left; inset < row.min || inset > row.max {
			t.Errorf("%s inset %.1f, want %.1f…%.1f", row.label, inset, row.min, row.max)
		}
	}

	// The initial is centered inside an avatar inset 12 px from the surface.
	initial, ok := tt.Find("L")
	if !ok {
		t.Fatal("comment avatar missing")
	}
	if d := initial.X - left; d < 11.5 || d > 20 {
		t.Errorf("comment avatar initial inset %.1f, want padded avatar", d)
	}
}
