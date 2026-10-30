package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// The create pill is white in both appearances, and the ↑ sits on a light
// disc at its right end. mygo's RGBA alpha is 0–1; passing a byte-scale 18
// clamps to opaque black and the dark arrow disappears into the disc. This
// samples the button in the frame, which a label check cannot see.
func TestComposeSendArrowStaysReadable(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			isolatePrefs(t)
			b := newBoard(seedBoard())
			tt := ui.NewTester(b.view, 1280, 800)
			tt.SetDark(dark)
			waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
			settle(t, tt)

			b.openTaskFormIn("local", "c-todo")
			tt.Advance(600 * time.Millisecond)
			tt.Frame()

			btn, ok := tt.Find("创建任务")
			if !ok {
				t.Fatalf("send button missing; texts: %v", tt.Texts())
			}
			img := tt.Image()
			minL, maxL := 255, 0
			darkPx, lightPx := 0, 0
			x0, y0 := int(btn.X)+2, int(btn.Y)+2
			x1, y1 := int(btn.X+btn.W)-2, int(btn.Y+btn.H)-2
			if x1 <= x0 || y1 <= y0 {
				t.Fatalf("send button too small to sample: %+v", btn)
			}
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					r, g, bl, _ := img.At(x, y).RGBA()
					luma := int((r + g + bl) / 3 >> 8)
					if luma < minL {
						minL = luma
					}
					if luma > maxL {
						maxL = luma
					}
					if luma < 80 {
						darkPx++
					}
					if luma > 180 {
						lightPx++
					}
				}
			}
			if lightPx == 0 || maxL < 200 {
				t.Fatalf("send disc is not a light wash (max luma %d, light px %d); a clamped alpha paints it black", maxL, lightPx)
			}
			if darkPx == 0 || minL > 80 {
				t.Fatalf("no dark ↑ on the disc (min luma %d, dark px %d)", minL, darkPx)
			}
			if maxL-minL < 120 {
				t.Fatalf("↑ contrast is %d luma, want at least 120 (min %d max %d)", maxL-minL, minL, maxL)
			}
		})
	}
}
