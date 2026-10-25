package main

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

// TestReviewCheckChipGeometry pins the 评审 ✓ chip's geometry: a 24×24 chip
// (a 16px check inside a 4px ring — 28 read as a big empty box), and, in
// title-only cards, centered on the card's midline. One-line cards then
// line the chip up with the title; two-line (global) cards center it on
// the card instead of riding the top corner.
func TestReviewCheckChipGeometry(t *testing.T) {
	dir := t.TempDir()
	// Isolate prefs: frames can saveUIPrefs (sidebar width), and the prefs
	// path derives from TASKBOARD_DB — point it inside this test's dir.
	t.Setenv("TASKBOARD_DB", filepath.Join(dir, "tb.db"))
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })

	fake := seedBoard()
	b := newBoard(fake)
	tt := newBoardTester(b)

	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	const label = "验收通过，移到完成"
	chip, ok := tt.Find(label)
	if !ok {
		t.Fatalf("full mode: 评审 chip missing")
	}
	if chip.W != 24 || chip.H != 24 {
		t.Errorf("full mode: chip %gx%g, want 24x24", chip.W, chip.H)
	}

	// Title-only, project view: one-line cards, so the chip centered on the
	// card must sit on the title's midline — the alignment the board shows.
	b.prefs.TitleOnlyCards = true
	b.cur = "local"
	b.post(func() {})
	tt.Frame()
	settle(t, tt)

	chip, ok = tt.Find(label)
	if !ok {
		t.Fatalf("title-only mode: 评审 chip missing")
	}
	title, ok := tt.Find("版本冲突时拉一次详情对齐")
	if !ok {
		t.Fatalf("title-only mode: 评审 card title missing")
	}
	chipMid := chip.Y + chip.H/2
	titleMid := title.Y + title.H/2
	if d := chipMid - titleMid; d < -1.5 || d > 1.5 {
		t.Errorf("title-only mode: chip midline %.1f vs title midline %.1f (Δ%.1f), want |Δ| ≤ 1.5", chipMid, titleMid, d)
	}
	if chip.W != 24 || chip.H != 24 {
		t.Errorf("title-only mode: chip %gx%g, want 24x24", chip.W, chip.H)
	}

	// Global view: title + project-name lines; the chip centers on the card.
	b.cur = curGlobal
	tt.Frame()
	settle(t, tt)
	chip, ok = tt.Find(label)
	if !ok {
		t.Fatalf("title-only global mode: 评审 chip missing")
	}
	bound := b.bounds["t-5"]
	chipMid = chip.Y + chip.H/2
	cardMid := bound.Y + bound.H/2
	if d := chipMid - cardMid; d < -1.5 || d > 1.5 {
		t.Errorf("title-only global mode: chip midline %.1f vs card midline %.1f (Δ%.1f), want |Δ| ≤ 1.5", chipMid, cardMid, d)
	}
}

// TestReviewChipStaysInsideCard pins LOCAL-32: the title-only 评审 ✓ is a
// card overlay, and Attach's second anchor is the chip's own point — it
// must be AnchorRight (chip's right edge at the card's, then s1 in).
// AnchorCenter pinned the chip's center to the card's edge and left half
// of the chip hanging over the column gap, which read as the button
// running outside its card. The chip must also never cover the title: the
// resting layout reserves the chip's zone at the row's end. And the chip
// carries no floating tip — its words live in the a11y label.
func TestReviewChipStaysInsideCard(t *testing.T) {
	isolatePrefs(t)
	b := newBoard(seedBoard())
	b.prefs.TitleOnlyCards = true
	b.cur = "local"
	tt := newBoardTester(b)
	tt.SetDark(true)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("版本冲突时拉一次详情对齐") })
	settle(t, tt)

	const label = "验收通过，移到完成"
	chip, ok := tt.Find(label)
	if !ok {
		t.Fatalf("评审 chip missing; texts: %v", tt.Texts())
	}
	card := b.bounds["t-5"]
	if chip.X < card.X || chip.X+chip.W > card.X+card.W {
		t.Errorf("chip x %.1f..%.1f leaves its card %.1f..%.1f", chip.X, chip.X+chip.W, card.X, card.X+card.W)
	}
	if chip.Y < card.Y || chip.Y+chip.H > card.Y+card.H {
		t.Errorf("chip y %.1f..%.1f leaves its card %.1f..%.1f", chip.Y, chip.Y+chip.H, card.Y, card.Y+card.H)
	}
	title, ok := tt.Find("版本冲突时拉一次详情对齐")
	if !ok {
		t.Fatalf("评审 title missing")
	}
	if title.X+title.W > chip.X {
		t.Errorf("title runs under the chip: title ends %.1f, chip starts %.1f", title.X+title.W, chip.X)
	}

	// Hovering shows the chip but no floating tip: the themed Tooltip put
	// a "移到完成" bubble half outside the card (LOCAL-32); the words stay
	// in the a11y label now.
	tt.Move(chip.X+chip.W/2, chip.Y+chip.H/2)
	tt.Advance(800 * time.Millisecond)
	tt.Frame()
	if _, ok := tt.Find("移到完成"); ok {
		t.Errorf("hovering the chip still raises a floating 移到完成 tip")
	}

	// Acceptance shot: the hovered chip inside its card, dark, as users
	// run the board.
	img := tt.Image()
	x0, y0 := int(card.X)-230, int(card.Y)-70
	sub := img.SubImage(image.Rect(x0, y0, x0+500, y0+200))
	path := filepath.Join(renderDir(t), "review-chip-hover.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, sub); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
}
