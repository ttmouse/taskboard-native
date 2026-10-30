package main

import (
	"fmt"
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestHorizontalCardsKeepSymmetricInsets(t *testing.T) {
	for _, width := range []int{1280, 1800} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			isolatePrefs(t)
			fake := seedBoard()
			for i := range 30 {
				task := fake.tasks[0]
				task.ID = fmt.Sprintf("overflow-%d", i)
				task.ColumnID = "c-done"
				fake.tasks = append(fake.tasks, task)
			}
			b := newBoard(fake)
			defer b.stop()
			b.prefs.SidebarCollapsed = true
			tt := ui.NewTester(b.view, width, renderH)
			waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
			settle(t, tt)
			last := b.columns[len(b.columns)-1]
			panel, _ := tt.Find(last.Name + "列")
			if width == 1280 {
				if gap := float32(width) - panel.X - panel.W; math.Abs(float64(gap-8)) > 1 {
					t.Errorf("outer right margin %.1f; want 8", gap)
				}
			}
			tt.Scroll(panel.X+panel.W/2, panel.Y+100, 0, 120)
			if track := b.columnScroll(last.ID); track.state.Y <= 0 || !b.scrollbarActive() {
				t.Fatal("overflow column did not scroll with a visible scrollbar")
			}
			if width == renderW {
				tt.SetDark(true)
				tt.Frame()
				settle(t, tt)
				save(t, tt, "board-scrollbar-spacing.png")
			}
			track := b.columnScroll(last.ID)
			before := track.state.Y
			x, y := panel.X+panel.W, panel.Y+100
			tt.Move(x, y)
			tt.Press(x, y)
			tt.Move(x, y+100)
			tt.Frame()
			tt.Release(x, y+100)
			if track.state.Y <= before {
				t.Fatal("column scrollbar outside cards could not be dragged")
			}
			for _, col := range b.columns {
				panel, ok := tt.Find(col.Name + "列")
				if !ok {
					t.Fatalf("missing column %s", col.Name)
				}
				for _, task := range b.tasksIn(col.ID) {
					card := b.bounds[task.ID]
					left := card.X - panel.X
					right := panel.X + panel.W - card.X - card.W
					if card.W <= 0 || math.Abs(float64(left-right)) > 1 {
						t.Errorf("%s: card width %.1f, left inset %.1f, right inset %.1f", col.Name, card.W, left, right)
					}
					if math.Abs(float64(left-4)) > 1 || math.Abs(float64(right-4)) > 1 {
						t.Errorf("%s: insets %.1f/%.1f; want only 4px panel padding, no scrollbar gutter", col.Name, left, right)
					}
				}
			}
			if err := tt.Click("打开任务：对齐设计规格的间距与字号"); err != nil {
				t.Fatal(err)
			}
			waitFor(t, tt, "打开卡片详情", func() bool { return b.detail != nil && !b.detail.loading })
		})
	}
}
