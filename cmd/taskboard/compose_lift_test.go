package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestTaskComposeSitsAboveCenter checks the create-task prompt rests above
// dead center — a dialog centered exactly covers the first row of cards —
// while still staying clear of the top bar and inside the window at the
// minimum size. Other forms keep DialogBase's centering.
func TestTaskComposeSitsAboveCenter(t *testing.T) {
	for _, size := range []struct {
		name string
		w, h int
	}{
		{"window", renderW, renderH},
		{"minimum", 960, 560},
	} {
		t.Run(size.name, func(t *testing.T) {
			isolatePrefs(t)
			b := newBoard(seedBoard())
			tt := ui.NewTester(b.view, size.w, size.h)
			waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
			settle(t, tt)

			b.openTaskFormIn("local", "c-todo")
			tt.Advance(200 * time.Millisecond)
			tt.Frame()

			pill, ok := tt.Find("任务标题")
			if !ok {
				t.Fatalf("no 任务标题 field; texts: %v", tt.Texts())
			}
			center := float32(size.h) / 2
			mid := pill.Y + pill.H/2
			// Above center, and by enough to read as deliberate rather than
			// as a layout accident: at least a tenth of the window height.
			if lift := center - mid; lift < float32(size.h)/10 {
				t.Errorf("pill center at y=%.0f, only %.0f above the %.0f center", mid, lift, center)
			}
			// Still a dialog on screen: its whole height stays in the window
			// and below the top bar, not pushed off the top edge.
			label, ok := tt.Find("目标项目")
			if !ok {
				t.Fatalf("no destination label; texts: %v", tt.Texts())
			}
			if label.Y < float32(size.h)/10 {
				t.Errorf("destination label at y=%.0f is crowded against the top bar", label.Y)
			}
			if pill.Y < 0 || pill.Y+pill.H > float32(size.h) {
				t.Errorf("pill at %+v leaves the %dx%d window", pill, size.w, size.h)
			}
		})
	}
}

// TestProjectFormStaysCentered guards the lift from leaking into the other
// sheet DialogBase centers: 新建项目 keeps its field near the middle. The
// field is the probe, not the title text, which the top bar also shows.
func TestProjectFormStaysCentered(t *testing.T) {
	isolatePrefs(t)
	b := newBoard(seedBoard())
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	b.openForm()
	tt.Frame()
	field, ok := tt.Find("项目名")
	if !ok {
		t.Fatalf("no 项目名 field; texts: %v", tt.Texts())
	}
	center := float32(renderH) / 2
	if mid := field.Y + field.H/2; mid < center-float32(renderH)/10 {
		t.Errorf("project form field at y=%.0f, want it near the %.0f center", mid, center)
	}
}
