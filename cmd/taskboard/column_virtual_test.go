package main

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
	"taskboard-native/internal/model"
)

// TestColumnVirtualizesOffscreenCards checks a tall column only keeps full
// taskCard labels for rows near the scroll viewport; far rows are height
// placeholders (no "打开任务：" a11y label) after the scroll has a real size.
func TestColumnVirtualizesOffscreenCards(t *testing.T) {
	isolatePrefs(t)
	fake := seedBoard()
	const n = 80
	for i := 0; i < n; i++ {
		fake.tasks = append(fake.tasks, model.Task{
			ID:        fmt.Sprintf("virt-%03d", i),
			ProjectID: "local",
			ColumnID:  "c-todo",
			Title:     fmt.Sprintf("虚拟列表卡片 %03d", i),
			Position:  float64(3000 + i),
		})
	}
	b := newBoard(fake)
	defer b.stop()
	b.prefs.SidebarCollapsed = true
	b.prefs.TitleOnlyCards = true
	// Short window so most of the 80 cards sit below the viewport.
	tt := ui.NewTester(b.view, renderW, 520)
	waitFor(t, tt, "首卡载入", func() bool { return tt.HasText("虚拟列表卡片 000") })
	settle(t, tt)

	track := b.columnScroll("c-todo")
	if track == nil {
		t.Fatal("missing todo column scroll state")
	}
	// Drive a layout pass with a known scroll height: settle already framed;
	// scroll down so the window is definitely sized, then check labels.
	panel, ok := tt.Find("待办列")
	if !ok {
		t.Fatal("missing 待办列")
	}
	tt.Scroll(panel.X+panel.W/2, panel.Y+120, 0, 400)
	settle(t, tt)

	visible := 0
	missingFar := 0
	for i := 0; i < n; i++ {
		label := fmt.Sprintf("打开任务：虚拟列表卡片 %03d", i)
		if tt.HasText(label) || findLabel(tt, label) {
			visible++
		} else if i > 40 {
			missingFar++
		}
	}
	if visible == 0 {
		t.Fatal("expected some in-view cards to keep full labels")
	}
	if visible >= n {
		t.Fatalf("expected off-screen cards to drop full card labels, still see %d/%d", visible, n)
	}
	if missingFar < 10 {
		t.Fatalf("expected many far cards without labels, missingFar=%d visible=%d", missingFar, visible)
	}
	// Top card after scroll may be gone; a mid card near the new viewport should remain.
	if track.state.Y <= 0 {
		t.Fatal("column did not scroll")
	}
}

func findLabel(tt *ui.Tester, label string) bool {
	_, ok := tt.Find(label)
	return ok
}
