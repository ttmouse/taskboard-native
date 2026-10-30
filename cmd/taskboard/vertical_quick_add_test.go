package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"taskboard-native/internal/model"
)

const quickAddLabel = "在「待办」中添加任务"

// TestVerticalQuickAddLinesUpWithCards: the field at the top of the
// stacked board shares the cards' left and right edges. It used to span
// the whole pane, four pixels wider on each side than the cards below,
// which read as a different, wider row.
func TestVerticalQuickAddLinesUpWithCards(t *testing.T) {
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

	field, ok := tt.Find(quickAddLabel)
	if !ok {
		t.Fatalf("no quick add; texts: %v", tt.Texts())
	}
	head, ok := tt.Find("折叠待办")
	if !ok {
		t.Fatal("no 待办 header")
	}
	card, ok := tt.Find("打开任务：把看板接上 SSE")
	if !ok {
		t.Fatalf("no card; texts: %v", tt.Texts())
	}
	if d := field.X - head.X; d > 1 || d < -1 {
		t.Errorf("field left %.1f vs header left %.1f", field.X, head.X)
	}
	if d := (field.X + field.W) - (head.X + head.W); d > 1 || d < -1 {
		t.Errorf("field right %.1f vs header right %.1f", field.X+field.W, head.X+head.W)
	}
	if d := field.X - card.X; d > 1 || d < -1 {
		t.Errorf("field left %.1f vs card left %.1f", field.X, card.X)
	}
	if d := (field.X + field.W) - (card.X + card.W); d > 1 || d < -1 {
		t.Errorf("field right %.1f vs card right %.1f", field.X+field.W, card.X+card.W)
	}
}

// TestVerticalQuickAdd creates from the field at the top of the stacked
// board, and does not offer that field on the horizontal board.
func TestVerticalQuickAdd(t *testing.T) {
	isolatePrefs(t)
	if err := saveUIPrefs(uiPrefs{BoardVertical: true}); err != nil {
		t.Fatal(err)
	}
	fake := seedBoard()
	before := len(fake.tasks)
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	if _, ok := tt.Find(quickAddLabel); !ok {
		t.Fatalf("vertical board has no quick add; texts: %v", tt.Texts())
	}

	focusQuickAdd(t, tt)
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if len(fake.tasks) != before {
		t.Fatal("blank Enter created a task")
	}
	if tt.HasText("不能为空") {
		t.Fatal("blank Enter toasted")
	}

	focusQuickAdd(t, tt)
	tt.Type("全局不该创建")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !tt.HasText("请先在侧栏选一个项目") {
		t.Fatalf("global Enter did not ask for a project; texts: %v", tt.Texts())
	}
	if b.quickTitle != "全局不该创建" {
		t.Fatalf("global Enter cleared the draft, got %q", b.quickTitle)
	}
	if hasTask(fake, "全局不该创建") {
		t.Fatal("global Enter created a task")
	}

	if err := tt.Click("切换到项目 local"); err != nil {
		t.Fatalf("choose project: %v", err)
	}
	waitFor(t, tt, "切换项目", func() bool { return b.cur == "local" })
	waitFor(t, tt, "看板列到达", func() bool { return len(b.columns) > 0 })
	if b.quickTitle != "全局不该创建" {
		t.Fatalf("switching project dropped the draft, got %q", b.quickTitle)
	}
	focusQuickAdd(t, tt)
	tt.Key(0, ui.KeyEnter)
	if b.quickTitle != "" {
		t.Fatalf("Enter left %q in the field", b.quickTitle)
	}
	waitFor(t, tt, "新增任务落到后端", func() bool { return hasTask(fake, "全局不该创建") })
	if got := taskColumn(fake, "全局不该创建"); got != "c-todo" {
		t.Fatalf("task landed in %q, want c-todo", got)
	}

	focusQuickAdd(t, tt)
	tt.Type("按 Esc 放弃")
	tt.Key(0, ui.KeyEscape)
	if b.quickTitle != "" {
		t.Fatalf("Escape left %q", b.quickTitle)
	}
	if hasTask(fake, "按 Esc 放弃") {
		t.Fatal("Escape created a task")
	}
}

func TestHorizontalBoardHasNoQuickAdd(t *testing.T) {
	isolatePrefs(t)
	b := newBoard(seedBoard())
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)
	if b.prefs.BoardVertical {
		t.Fatal("horizontal case loaded a vertical board")
	}
	if _, ok := tt.Find(quickAddLabel); ok {
		t.Fatal("horizontal board shows the vertical quick add")
	}
}

func focusQuickAdd(t *testing.T, tt *ui.Tester) {
	t.Helper()
	if err := tt.Click(quickAddLabel); err != nil {
		t.Fatalf("focus quick add: %v", err)
	}
	if !tt.Focused(quickAddLabel) {
		t.Fatal("quick add did not take focus")
	}
}

func taskColumn(f *fakeBoard, title string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, task := range f.tasks {
		if task.Title == title {
			return task.ColumnID
		}
	}
	return ""
}

// TestVerticalQuickAddEndToEnd: the whole path — switch to a project, type
// in the field, press Enter — lands a card in that project's 待办 and
// clears the field. It walks the same code the packaged app does.
func TestVerticalQuickAddEndToEnd(t *testing.T) {
	isolatePrefs(t)
	if err := saveUIPrefs(uiPrefs{BoardVertical: true}); err != nil {
		t.Fatal(err)
	}
	fake := seedBoard()
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	if err := tt.Click("切换到项目 local"); err != nil {
		t.Fatalf("choose project: %v", err)
	}
	waitFor(t, tt, "切换项目", func() bool { return b.cur == "local" })
	waitFor(t, tt, "看板列到达", func() bool { return len(b.columns) > 0 })

	focusQuickAdd(t, tt)
	tt.Type("端到端新建")
	tt.Key(0, ui.KeyEnter)

	waitFor(t, tt, "任务落到后端", func() bool { return hasTask(fake, "端到端新建") })
	if got := taskColumn(fake, "端到端新建"); got != "c-todo" {
		t.Fatalf("task landed in %q, want c-todo", got)
	}
	if !taskIsLastInColumn(fake, "端到端新建") {
		t.Fatal("new task did not land at the bottom of 待办")
	}
	if b.quickTitle != "" {
		t.Fatalf("field still holds %q after a successful create", b.quickTitle)
	}
	// The card shows up in the 待办 group of the live board.
	waitFor(t, tt, "卡片出现在待办", func() bool { return tt.HasText("打开任务：端到端新建") })
}

func taskIsLastInColumn(f *fakeBoard, title string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	var target model.Task
	found := false
	for _, task := range f.tasks {
		if task.Title == title {
			target = task
			found = true
			break
		}
	}
	if !found {
		return false
	}
	for _, task := range f.tasks {
		if task.ColumnID == target.ColumnID && !task.Archived && task.Position > target.Position {
			return false
		}
	}
	return true
}
