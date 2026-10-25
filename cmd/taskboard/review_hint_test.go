package main

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
)

// TestReviewHintStaysOnItsCard pins LOCAL-32: the ✓ chip's "移到完成"
// hint used to be a themed Tooltip, centered above the 24px chip, which
// in the title-only 评审 column hung half outside the card — over the
// gap between columns and the last line of the card above — so the
// button read as having drifted off its card. The hint must lie inside
// the card it acts on, and still be whole (not clipped to a sliver).
func TestReviewHintStaysOnItsCard(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TASKBOARD_DB", filepath.Join(dir, "tb.db"))
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })

	b := newBoard(seedBoard())
	b.prefs.TitleOnlyCards = true
	b.cur = "local"
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("版本冲突时拉一次详情对齐") })
	settle(t, tt)

	check := "验收通过，移到完成"
	chip, ok := tt.Find(check)
	if !ok {
		t.Fatalf("评审 chip missing; texts: %v", tt.Texts())
	}
	tt.Move(chip.X+chip.W/2, chip.Y+chip.H/2)
	tt.Advance(800 * 1000 * 1000) // past the tooltip's rest delay
	tt.Frame()

	hint, ok := tt.Find("移到完成")
	if !ok {
		t.Fatalf("hovering the chip shows no hint; texts: %v", tt.Texts())
	}
	card := b.bounds["t-5"]
	if card.W <= 0 {
		t.Fatalf("card bounds missing: %+v", card)
	}
	if hint.X < card.X || hint.X+hint.W > card.X+card.W {
		t.Errorf("hint x %0.f..%0.f leaves its card %0.f..%0.f", hint.X, hint.X+hint.W, card.X, card.X+card.W)
	}
	if hint.Y < card.Y || hint.Y+hint.H > card.Y+card.H {
		t.Errorf("hint y %0.f..%0.f leaves its card %0.f..%0.f", hint.Y, hint.Y+hint.H, card.Y, card.Y+card.H)
	}
	// It reads: dark ink on the light board, not a white tip on the card
	// (a theme without Inverse used to give exactly that).
	img := tt.Image()
	sum, ink, n := 0, 0, 0
	for y := int(hint.Y); y < int(hint.Y+hint.H); y++ {
		for x := int(hint.X); x < int(hint.X+hint.W); x++ {
			r, _, _, _ := img.At(x, y).RGBA()
			sum += int(r >> 8)
			n++
			if r>>8 < 160 {
				ink++
			}
		}
	}
	if mean := float64(sum) / float64(n); mean > 110 || ink < n/6 {
		t.Errorf("hint is %0.f/255 bright with %d/%d ink pixels: unreadable", mean, ink, n)
	}
	// A whole word, not a clipped sliver: the tip is one line of text.
	if hint.W < 40 || hint.H < 12 {
		t.Errorf("hint collapsed to %0.fx%0.f", hint.W, hint.H)
	}

	// Off hover the hint is gone, so a resting board shows no floating label.
	// Several frames: the pointer leaving reaches the chip's hover on the
	// frame that processes it.
	tt.Move(chip.X+chip.W/2+400, chip.Y+300)
	for i := 0; i < 3; i++ {
		tt.Advance(50 * 1000 * 1000)
		tt.Frame()
	}
	if r, ok := tt.Find("移到完成"); ok {
		t.Errorf("hint stayed after the pointer left the chip: %+v", r)
	}
}

// A full card (not title-only) carries the chip in the title row; its hint
// must stay on that card too.
func TestReviewHintStaysOnFullCard(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TASKBOARD_DB", filepath.Join(dir, "tb.db"))
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })

	b := newBoard(seedBoard())
	b.prefs.TitleOnlyCards = false
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("版本冲突时拉一次详情对齐") })
	settle(t, tt)
	chip, ok := tt.Find("验收通过，移到完成")
	if !ok {
		t.Fatalf("评审 chip missing; texts: %v", tt.Texts())
	}
	tt.Move(chip.X+chip.W/2, chip.Y+chip.H/2)
	tt.Advance(800 * 1000 * 1000)
	tt.Frame()
	hint, ok := tt.Find("移到完成")
	if !ok {
		t.Fatalf("hovering the chip shows no hint")
	}
	card := b.bounds["t-5"]
	if hint.X < card.X || hint.X+hint.W > card.X+card.W || hint.Y < card.Y || hint.Y+hint.H > card.Y+card.H {
		t.Errorf("hint %+v leaves its card %+v", hint, card)
	}
}
