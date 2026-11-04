package main

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// TestReviewCheckChipGeometry pins the 评审 ✓ chip's geometry: a 24×24 chip
// (a 16px check inside a 4px ring — 28 read as a big empty box), in the
// card's bottom-right footer in both title-only and full-card layouts.
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

	// Title-only, project view: keep the title compact and put actions below it.
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
	if title.Y+title.H > chip.Y {
		t.Errorf("title-only mode: chip is not below title: title=%+v chip=%+v", title, chip)
	}
	card := b.bounds["t-5"]
	if gap := card.X + card.W - chip.X - chip.W; gap < 0 || gap > 16 {
		t.Errorf("title-only mode: chip is not at bottom-right: gap=%v card=%+v chip=%+v", gap, card, chip)
	}
	if chip.W != 24 || chip.H != 24 {
		t.Errorf("title-only mode: chip %gx%g, want 24x24", chip.W, chip.H)
	}

	// Global view: title + project-name lines; actions remain in the footer.
	b.cur = curGlobal
	tt.Frame()
	settle(t, tt)
	chip, ok = tt.Find(label)
	if !ok {
		t.Fatalf("title-only global mode: 评审 chip missing")
	}
	bound := b.bounds["t-5"]
	if gap := bound.X + bound.W - chip.X - chip.W; gap < 0 || gap > 16 {
		t.Errorf("title-only global mode: chip is not at bottom-right: gap=%v card=%+v chip=%+v", gap, bound, chip)
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
	if title.Y+title.H > chip.Y {
		t.Errorf("title runs into the action row: title ends at %.1f, chip starts at %.1f", title.Y+title.H, chip.Y)
	}
	if gap := card.X + card.W - chip.X - chip.W; gap < 0 || gap > 16 {
		t.Errorf("chip is not at card bottom-right: gap=%v card=%+v chip=%+v", gap, card, chip)
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

func TestReviewCheckMovesTaskToDone(t *testing.T) {
	isolatePrefs(t)
	fake := seedBoard()
	b := newBoard(fake)
	tt := newBoardTester(b)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	b.prefs.TitleOnlyCards = true

	waitFor(t, tt, "评审任务", func() bool { return tt.HasText("验收通过，移到完成") })
	settle(t, tt)
	card := b.bounds["t-5"]
	tt.Move(card.X+card.W/2, card.Y+card.H/2)
	tt.Frame()
	chip, ok := tt.Find("验收通过，移到完成")
	if !ok {
		t.Fatal("评审通过操作缺失")
	}
	tt.Move(chip.X+chip.W/2, chip.Y+chip.H/2)
	tt.Frame()
	tt.ClickAt(chip.X+chip.W/2, chip.Y+chip.H/2)
	waitFor(t, tt, "任务移入完成列", func() bool {
		task := b.task("t-5")
		return task != nil && task.ColumnID == "c-done"
	})
	if got := b.task("t-5").ColumnID; got != "c-done" {
		t.Fatalf("task column = %q, want c-done", got)
	}
}
