package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestComposeEnterCloses: plain Enter in the create-task pill creates the
// card and closes the sheet. The overlay used to stay up, which read as
// "Enter did nothing".
func TestComposeEnterCloses(t *testing.T) {
	isolatePrefs(t)
	fake := seedBoard()
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	b.openTaskFormIn("local", "c-todo")
	tt.Frame()
	if b.form == nil || !b.form.open {
		t.Fatal("form not open")
	}
	tt.Type("回车创建并关闭")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if b.form != nil && b.form.open {
		t.Fatalf("sheet still open after Enter; texts: %v", tt.Texts())
	}
	waitFor(t, tt, "回车创建任务落到后端", func() bool { return hasTask(fake, "回车创建并关闭") })
}

// TestComposeShiftEnterStays: Shift+Enter creates and keeps the sheet for
// the next title with an empty field.
func TestComposeShiftEnterStays(t *testing.T) {
	isolatePrefs(t)
	fake := seedBoard()
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	b.openTaskFormIn("local", "c-todo")
	tt.Frame()
	tt.Type("第一条")
	tt.Key(ui.Shift, ui.KeyEnter)
	tt.Frame()
	if b.form == nil || !b.form.open {
		t.Fatalf("sheet closed on Shift+Enter; form=%v", b.form != nil)
	}
	if b.form.name != "" {
		t.Fatalf("draft not cleared: %q", b.form.name)
	}
	waitFor(t, tt, "连续创建任务落到后端", func() bool { return hasTask(fake, "第一条") })
}

func hasTask(f *fakeBoard, title string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, task := range f.tasks {
		if task.Title == title {
			return true
		}
	}
	return false
}
