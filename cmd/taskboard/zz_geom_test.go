package main

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
)

// sheetEdges scans a vertical line at x for the sheet's surface (the
// theme Background, brighter than the dimmed backdrop) and returns its top
// and bottom.
func sheetEdges(tt *ui.Tester, x, h int) (int, int) {
	img := tt.Image()
	top, bottom := -1, -1
	for y := 0; y < h; y++ {
		r, g, b, _ := img.At(x, y).RGBA()
		// Sheet surface in the light theme is near-white (>= 240).
		if r>>8 >= 240 && g>>8 >= 240 && b>>8 >= 240 {
			if top < 0 {
				top = y
			}
			bottom = y
		}
	}
	return top, bottom
}

func TestGeomSheet(t *testing.T) {
	for _, win := range [][]int{{1280, 560}, {1280, 800}, {1280, 620}} {
		isolatePrefs(t)
		b := newBoard(seedBoard())
		tt := ui.NewTester(b.view, win[0], win[1])
		waitFor(t, tt, "载入", func() bool { return b.task("t-1") != nil })
		b.openDetail(*b.task("t-1"))
		waitFor(t, tt, "评论", func() bool { return !b.detail.loading })
		tt.Advance(700e6)
		tt.Frame()
		top, bottom := sheetEdges(tt, win[0]/2, win[1])
		gy, gh, _ := rectOf(tt, "归档任务")
		sy, sh, _ := rectOf(tt, "保存任务")
		fmt.Printf("win=%dx%d sheet y=%d..%d | 归档 %.0f..%.0f | 保存 %.0f..%.0f\n",
			win[0], win[1], top, bottom, gy, gy+gh, sy, sy+sh)
	}
}

func rectOf(tt *ui.Tester, label string) (float32, float32, bool) {
	r, ok := tt.Find(label)
	return r.Y, r.H, ok
}
