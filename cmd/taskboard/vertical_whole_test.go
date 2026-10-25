package main

import (
	"strings"
	"testing"
	"time"
)

// TestVerticalRowsShowWholeCards: in the stacked board a card is as tall
// as its text. Rows used to get a viewport share (min 220) that clipped
// their list, so a card whose description ran past two lines was cut off
// part way through and the next card was sliced by the row's edge.
const longTitle = "把看板接上 SSE 事件流并就地更新卡片，不轮询、不整表重建，这条标题长到在窄卡里要折成三行"

// fontPX is the body font step the cards render the title at (fmd is the
// theme's size plus two, and the test theme's size is 13).
const fontPX = 13

func TestVerticalRowsShowWholeCards(t *testing.T) {
	isolatePrefs(t)
	if err := saveUIPrefs(uiPrefs{BoardVertical: true}); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("这是一段很长的任务描述，用来把卡片撑高。", 6)
	fake := seedBoard()
	for i := range fake.tasks {
		fake.tasks[i].Description = long
	}
	fake.tasks[0].Title = longTitle
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)
	tt.Advance(400 * time.Millisecond)
	tt.Frame()
	if !b.prefs.BoardVertical {
		t.Fatal("board is not stacked")
	}

	row, ok := tt.Find("待办列")
	if !ok {
		t.Fatalf("no 待办 row; texts: %v", tt.Texts())
	}
	first, ok := tt.Find("打开任务：" + longTitle)
	if !ok {
		t.Fatalf("no first card; texts: %v", tt.Texts())
	}
	if !tt.HasText("很长") {
		t.Fatal("no description on the card")
	}
	// The row is taller than one horizontal card laid on its side: the
	// cards were cut at a fixed viewport share (220) before.
	if row.H <= 220 {
		t.Errorf("row is %.0f tall, want it to grow with its cards", row.H)
	}
	// A sideways card clamps its description at two lines; a stacked card
	// shows it whole, so the row has to be taller than the clamp allows
	// and the card has to fit inside it.
	if first.H <= 100 {
		t.Errorf("card is %.0f tall, expected the whole description", first.H)
	}
	// Nothing hangs below the row the card lives in: the row grew with it.
	if first.Y+first.H > row.Y+row.H+1 {
		t.Errorf("card ends at %.0f, past its %.0f row — clipped", first.Y+first.H, row.Y+row.H)
	}
	second, ok := tt.Find("打开任务：对齐设计规格的间距与字号")
	if ok && second.Y < first.Y+first.H {
		t.Errorf("second card starts at %.0f, inside the first card that ends at %.0f",
			second.Y, first.Y+first.H)
	}
}

// TestVerticalColumnKeepsPaneWidth: every stacked row spans the pane and
// shares one left edge, and no card reaches past its row's edge.
func TestVerticalColumnKeepsPaneWidth(t *testing.T) {
	isolatePrefs(t)
	if err := saveUIPrefs(uiPrefs{BoardVertical: true}); err != nil {
		t.Fatal(err)
	}
	b := newBoard(seedBoard())
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)
	tt.Advance(400 * time.Millisecond)
	tt.Frame()

	// The row takes the whole pane between the sidebar and the window's
	// right padding, and every stacked row shares one left edge.
	row, ok := tt.Find("待办列")
	if !ok {
		t.Fatalf("no 待办 row; texts: %v", tt.Texts())
	}
	card, ok := tt.Find("打开任务：把看板接上 SSE")
	if !ok {
		t.Fatalf("no card; texts: %v", tt.Texts())
	}
	if card.X+card.W > row.X+row.W+1 {
		t.Errorf("card ends at %.0f, past its row at %.0f", card.X+card.W, row.X+row.W)
	}
	next, ok := tt.Find("进行中列")
	if !ok {
		t.Fatal("no 进行中 row")
	}
	if next.X != row.X {
		t.Errorf("rows start at %.0f and %.0f, want one left edge", row.X, next.X)
	}
	if pane := float32(renderW) - row.X - 16; row.W < pane-8 {
		t.Errorf("row is %.0f wide, want the %.0f pane", row.W, pane)
	}
}
