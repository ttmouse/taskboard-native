package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"taskboard-native/internal/model"
)

// TestVerticalRowsShowWholeCards: in the stacked board a card is as tall
// as its text. Rows used to get a viewport share (min 220) that clipped
// their list, so a card whose description ran past two lines was cut off
// part way through and the next card was sliced by the row's edge.
const longTitle = "把看板接上 SSE 事件流并就地更新卡片，不轮询、不整表重建，这条标题长到在窄卡里要折成三行"

// fontPX is the body font step the cards render the title at (fmd is the
// theme's size plus two, and the test theme's size is 13).
const fontPX = 13

func TestVerticalEmptyStatusesOnlyKeepHeadings(t *testing.T) {
	for _, height := range []int{560, 1200} {
		t.Run(fmt.Sprint(height), func(t *testing.T) {
			isolatePrefs(t)
			if err := saveUIPrefs(uiPrefs{BoardVertical: true}); err != nil {
				t.Fatal(err)
			}
			fake := seedBoard()
			fake.tasks = fake.tasks[4:] // Two empty statuses, one review card, then empty done.
			b := newBoard(fake)
			tt := ui.NewTester(b.view, renderW, height)
			waitFor(t, tt, "看板载入", func() bool { return tt.HasText("版本冲突") })
			settle(t, tt)
			var end float32
			for _, name := range []string{"待办", "进行中", "评审", "完成"} {
				row, ok := tt.Find(name + "列")
				if !ok {
					t.Fatalf("missing %s row", name)
				}
				if name != "评审" && row.H > 48 {
					t.Errorf("empty %s row is %.0f high; want only its heading", name, row.H)
				}
				if end > 0 && (row.Y < end || row.Y-end > 12) {
					t.Errorf("%s starts at %.0f after previous row ending at %.0f", name, row.Y, end)
				}
				end = row.Y + row.H
			}
			// 480 leaves room for the quick-add above the rows (~50) and still
			// keeps the short stack well above a 560 window.
			if end > 480 {
				t.Errorf("short board ends at %.0f; unused space should remain below it", end)
			}
		})
	}
}

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
	// Cards share the header's right edge. Overlay scrollbars must not
	// introduce a separate right gutter in either board orientation.
	head, ok := tt.Find("折叠待办")
	if !ok {
		t.Fatal("no 待办 header")
	}
	if d := (head.X + head.W) - (card.X + card.W); d > 1 || d < -1 {
		t.Errorf("card right %.1f vs header right %.1f (delta %.1f); want the same edge",
			card.X+card.W, head.X+head.W, d)
	}
	if d := card.X - head.X; d > 1 || d < -1 {
		t.Errorf("card left %.1f vs header left %.1f", card.X, head.X)
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

// TestVerticalGroupsFoldFromHeader: a stacked group folds from its header
// down to that header alone — the name and count stay, the cards leave —
// and the fold persists to ui-prefs.json. Clicking again unfolds.
func TestVerticalGroupsFoldFromHeader(t *testing.T) {
	isolatePrefs(t)
	if err := saveUIPrefs(uiPrefs{BoardVertical: true}); err != nil {
		t.Fatal(err)
	}
	b := newBoard(seedBoard())
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	if err := tt.Click("折叠待办"); err != nil {
		t.Fatalf("fold 待办: %v", err)
	}
	tt.Advance(400 * time.Millisecond)
	tt.Frame()
	row, ok := tt.Find("待办列")
	if !ok {
		t.Fatal("folded 待办 row vanished")
	}
	if row.H > 48 {
		t.Errorf("folded 待办 row is %.0f high; want only its header", row.H)
	}
	if tt.HasText("打开任务：把看板接上 SSE") {
		t.Error("folded group still shows its cards")
	}
	if !tt.HasText("2 个任务") || !tt.HasText("展开待办") {
		t.Errorf("folded header lost its count or open affordance; texts: %v", tt.Texts())
	}
	saved, ok := readUIPrefs()
	if !ok || !saved.CollapsedCols["c-todo"] {
		t.Errorf("fold not saved to ui-prefs.json: %+v", saved.CollapsedCols)
	}

	// Unfolding brings the cards back and clears the saved fold.
	if err := tt.Click("展开待办"); err != nil {
		t.Fatalf("unfold 待办: %v", err)
	}
	waitFor(t, tt, "卡片回到待办", func() bool { return tt.HasText("打开任务：把看板接上 SSE") })
	settle(t, tt)
	if saved, _ := readUIPrefs(); len(saved.CollapsedCols) != 0 {
		t.Errorf("unfold did not clear the saved fold: %+v", saved.CollapsedCols)
	}

	// A loaded fold folds at once: the window opens with 待办 already down.
	if err := saveUIPrefs(uiPrefs{BoardVertical: true, CollapsedCols: map[string]bool{"c-todo": true}}); err != nil {
		t.Fatal(err)
	}
	b = newBoard(seedBoard())
	tt = newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("展开待办") })
	if tt.HasText("打开任务：把看板接上 SSE") {
		t.Error("saved fold did not apply on load")
	}
}

// TestVerticalDropOnFoldedGroupMovesTask: a card dropped onto a folded
// group lands in that group and the group unfolds, so the landing is
// visible instead of hidden inside the fold. The drop path is dropOn →
// dropIntoGroup (the folded row has no rendered list to preview a slot
// with); the headless tester cannot drive a real drag onto the folded
// row, so this drives dropIntoGroup directly and asserts the same effects
// dropOn produces. 评审 is the folded group here: 完成 has its own bare-drop
// rule (top of column), which would mask the append placement.
func TestVerticalDropOnFoldedGroupMovesTask(t *testing.T) {
	isolatePrefs(t)
	if err := saveUIPrefs(uiPrefs{BoardVertical: true, CollapsedCols: map[string]bool{"c-review": true}}); err != nil {
		t.Fatal(err)
	}
	b := newBoard(seedBoard())
	defer b.stop()
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("展开评审") })
	settle(t, tt)

	var review *model.Column
	for i := range b.columns {
		if b.columns[i].ID == "c-review" {
			review = &b.columns[i]
		}
	}
	if review == nil {
		t.Fatal("no 评审 column")
	}
	b.dropIntoGroup("t-1", review)
	// The move lands optimistically in the model; wait for the view to
	// follow: the group reads folded no more and shows the moved card.
	waitFor(t, tt, "卡片落进展开的评审", func() bool {
		ts := b.task("t-1")
		return ts != nil && ts.ColumnID == "c-review" &&
			tt.HasText("折叠评审") && tt.HasText("打开任务：把看板接上 SSE")
	})
	if b.prefs.CollapsedCols["c-review"] {
		t.Error("drop did not unfold the group")
	}
	if saved, _ := readUIPrefs(); saved.CollapsedCols["c-review"] {
		t.Error("unfold was not saved")
	}
	// The moved card joins the group's bottom, after the task already there.
	if b.task("t-1").Position <= b.task("t-5").Position {
		t.Errorf("moved card position %.0f should sit after the group's earlier card at %.0f",
			b.task("t-1").Position, b.task("t-5").Position)
	}
}
