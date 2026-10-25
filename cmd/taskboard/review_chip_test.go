package main

import (
	"path/filepath"
	"testing"

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
