package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Entering source-edit mode must show the description from its first line.
// A new TextArea puts the caret at the end of the text and reveals it,
// which for a long body leaves the head scrolled out of view; the editor
// resets the caret to the start when it opens.
func TestDetailBodyOpensAtTop(t *testing.T) {
	isolatePrefs(t)
	b := newBoard(seedBoard())

	// Give t-1 a body long enough that showing its tail hides its head.
	long := "产品方向：第一行必须可见。\n\n现状：\n" + strings.Repeat("- 中间行。\n", 20) + "最后一行。"
	for i := range b.tasks {
		if b.tasks[i].ID == "t-1" {
			b.tasks[i].Description = long
		}
	}

	tt := newBoardTester(b)
	tt.SetDark(true)
	waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
	b.openDetail(*b.task("t-1"))
	if !b.detail.bodyTop {
		t.Fatal("openDetail did not mark the body to open at its head")
	}
	tt.Frame()
	if err := tt.Click("编辑任务正文"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !b.detail.editBody {
		t.Fatalf("click did not activate body editor; region=%+v", func() ui.Rect { r, _ := tt.Find("编辑任务正文"); return r }())
	}
	if b.detail.bodyTop {
		t.Fatal("bodyTop survived the first frame; the body would not open at its head")
	}
	if _, ok := tt.Find("正文编辑器"); !ok {
		t.Fatal("body text area missing from the open detail")
	}
}

// The fix rests on SetTextSelection(0, 0) moving a fresh TextArea's caret
// to the head, where its reveal scrolls the view to the top. Without that
// call the caret sits at the end, the state that hid the description's
// first lines. The caret is read inside the next build: a captured Element
// is only valid while the frame it came from is being built.
func TestBodyEditorScrollsToHeadOnOpen(t *testing.T) {
	long := "产品方向：第一行必须可见。\n" + strings.Repeat("中间行。\n", 30) + "最后一行。"
	for _, tc := range []struct {
		name   string
		atHead bool
		want   int
	}{
		{"default caret at tail (the bug)", false, len([]rune(long))},
		{"SetTextSelection(0,0) caret at head", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := long
			var caret int = -1
			first := true
			tt := ui.NewTester(func(c *ui.Context) {
				input := ui.TextArea(c, &body).Label("任务正文").FillWidth()
				if first {
					if tc.atHead {
						input.SetTextSelection(0, 0)
					}
					first = false
				}
				if _, cc := input.TextAnchorCaret(); cc >= 0 {
					caret = cc
				}
			}, 600, 260)
			tt.Frame()
			if caret != tc.want {
				t.Fatalf("caret %d, want %d", caret, tc.want)
			}
		})
	}
}
