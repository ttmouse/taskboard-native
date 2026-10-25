package main

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRenderReviewHint captures LOCAL-32's fix: in the density users run
// (title-only cards), hovering the 评审 ✓ shows "移到完成" inside its own
// card instead of hanging over the gap beside it.
func TestRenderReviewHint(t *testing.T) {
	isolatePrefs(t)
	b := newBoard(seedBoard())
	b.prefs.TitleOnlyCards = true
	b.cur = "local"
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("版本冲突时拉一次详情对齐") })
	settle(t, tt)

	const check = "验收通过，移到完成"
	chip, ok := tt.Find(check)
	if !ok {
		t.Fatalf("评审 chip missing")
	}
	tt.Move(chip.X+chip.W/2, chip.Y+chip.H/2)
	tt.Advance(800 * time.Millisecond)
	tt.Frame()
	if _, ok := tt.Find("移到完成"); !ok {
		t.Fatalf("hover shows no hint")
	}
	path := filepath.Join(renderDir(t), "review-hint-light.png")
	img := tt.Image()
	// The hint has to be readable on the light board, where the theme
	// leaves Inverse unset: a hint drawn white on a white card would pass
	// a "is it drawn" test while saying nothing.
	hint, _ := tt.Find("移到完成")
	sum, dark, n := 0, 0, 0
	for y := int(hint.Y); y < int(hint.Y+hint.H); y++ {
		for x := int(hint.X); x < int(hint.X+hint.W); x++ {
			r, _, _, _ := img.At(x, y).RGBA()
			sum += int(r >> 8)
			n++
			if r>>8 < 160 {
				dark++
			}
		}
	}
	mean := float64(sum) / float64(n)
	if mean > 110 || dark < n/6 {
		t.Errorf("hint is %0.f/255 bright with %d/%d dark pixels: unreadable on the light card", mean, dark, n)
	}
	// Crop to the two right columns around the card, so the hint's edges
	// against the card and the column gap are easy to see.
	card := b.bounds["t-5"]
	x0, y0 := int(card.X-220), int(card.Y-120)
	sub := img.SubImage(image.Rect(x0, y0, x0+620, y0+300))
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, sub); err != nil {
		t.Fatal(err)
	}
	t.Logf("saved %s (card %+v chip %+v)", path, card, chip)
}
