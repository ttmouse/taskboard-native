package main

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"taskboard-native/internal/client"
	"taskboard-native/internal/model"

	"github.com/egoist/mygo/ui"
)

const (
	renderW = 1280
	renderH = 800
)

// renderDir is where the acceptance screenshots land.
func renderDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("..", "..", "docs", "assets", "render")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	return dir
}

// save writes the tester's last frame as a PNG and checks its size.
func save(t *testing.T, tt *ui.Tester, name string) string {
	t.Helper()
	path := filepath.Join(renderDir(t), name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	img := tt.Image()
	b := img.Bounds()
	if b.Dx() != renderW || b.Dy() != renderH {
		t.Fatalf("%s is %dx%d, want %dx%d", name, b.Dx(), b.Dy(), renderW, renderH)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
	return path
}

// waitFor frames the view until cond holds, so background requests land.
func waitFor(t *testing.T, tt *ui.Tester, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s; showing %v", what, tt.Texts())
		}
		time.Sleep(2 * time.Millisecond)
		tt.Frame()
	}
}

// newBoardTester builds a window-sized tester around the board.
func newBoardTester(b *board) *ui.Tester {
	return ui.NewTester(b.view, renderW, renderH)
}

// TestRenderBoardLightAndDark renders the full board in both appearances
// and checks the Chinese chrome is on screen.
func TestRenderBoardLightAndDark(t *testing.T) {
	fake := seedBoard()
	b := newBoard(fake)
	tt := newBoardTester(b)

	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	waitFor(t, tt, "事件流连接", func() bool { return tt.HasText("实时同步已连接") })

	for _, want := range []string{
		"local", "待办", "进行中", "评审", "完成",
		"刷新", "新建任务", "在「待办」中添加任务",
		"把看板接上 SSE", "原生窗口渲染验收", "版本冲突时拉一次详情对齐",
		"1 条评论",
	} {
		if !tt.HasText(want) {
			t.Errorf("light frame lacks %q; texts: %v", want, tt.Texts())
		}
	}
	if got := save(t, tt, "board-light.png"); got != filepath.Join(renderDir(t), "board-light.png") {
		t.Fatalf("saved to %q", got)
	}

	// The appearance change animates backgrounds; let the transition
	// finish so the snapshot shows the settled dark palette.
	tt.SetDark(true)
	tt.Frame()
	tt.Advance(400 * time.Millisecond)
	tt.Frame()
	for _, want := range []string{"待办", "把看板接上 SSE", "新建任务"} {
		if !tt.HasText(want) {
			t.Errorf("dark frame lacks %q", want)
		}
	}
	save(t, tt, "board-dark.png")
}

// TestRenderEmpty checks the board with no cards: every column shows its
// placeholder instead of inventing content.
func TestRenderEmpty(t *testing.T) {
	fake := seedBoard()
	fake.tasks = nil
	b := newBoard(fake)
	tt := newBoardTester(b)

	waitFor(t, tt, "空看板", func() bool { return tt.HasText("还没有任务") })
	if n := strings.Count(strings.Join(tt.Texts(), "\n"), "还没有任务"); n != 4 {
		t.Fatalf("empty state appears %d times, want one per column (4)", n)
	}
	for _, want := range []string{"待办", "进行中", "评审", "完成"} {
		if !tt.HasText(want) {
			t.Errorf("empty frame lacks column %q", want)
		}
	}
	save(t, tt, "empty-light.png")
}

// TestRenderLoading captures the first frame of a slow board: the loading
// indicator and its Chinese label, before any column arrives.
func TestRenderLoading(t *testing.T) {
	fake := seedBoard()
	fake.delay = 2 * time.Second
	b := newBoard(fake)
	tt := newBoardTester(b)

	if !tt.HasText("正在载入看板…") {
		t.Fatalf("slow board shows no loading text; texts: %v", tt.Texts())
	}
	if !tt.HasText("正在载入看板") {
		t.Fatalf("loading indicator has no label")
	}
	save(t, tt, "loading-light.png")
}

// TestClickCardOpensAndSwitchesDetail checks a click on a card opens the
// detail with its comments, and a click on a card in another column
// switches the detail to that task.
func TestClickCardOpensAndSwitchesDetail(t *testing.T) {
	fake := seedBoard()
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })

	if err := tt.Click("把看板接上 SSE"); err != nil {
		t.Fatalf("click card: %v", err)
	}
	tt.Frame()
	if b.detail == nil || b.detail.id != "t-1" {
		t.Fatalf("detail = %+v, want task t-1", b.detail)
	}
	for _, want := range []string{"任务详情", "标题", "正文", "评论", "发送", "保存", "归档"} {
		if !tt.HasText(want) {
			t.Errorf("detail lacks %q; texts: %v", want, tt.Texts())
		}
	}
	waitFor(t, tt, "评论载入", func() bool { return tt.HasText("已加 atTop，拖到列顶也能落位。") })

	// Closing puts the board back, then another column's card switches it.
	if err := tt.Click("关闭"); err != nil {
		t.Fatalf("click close: %v", err)
	}
	tt.Frame()
	if b.detail != nil {
		t.Fatalf("detail still open after close: %+v", b.detail)
	}

	if err := tt.Click("原生窗口渲染验收"); err != nil {
		t.Fatalf("click second card: %v", err)
	}
	tt.Frame()
	if b.detail == nil || b.detail.id != "t-3" {
		t.Fatalf("detail = %+v, want task t-3", b.detail)
	}
	if b.detail.title != "原生窗口渲染验收" {
		t.Fatalf("detail title = %q", b.detail.title)
	}
}

// TestSSEEventUpdatesInPlace checks a pushed change lands on the board
// without refetching: the card appears in its column, then moves.
func TestSSEEventUpdatesInPlace(t *testing.T) {
	fake := seedBoard()
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })

	created := model.Task{
		ID: "t-push", ProjectID: "local", ColumnID: "c-review",
		Title: "由代理写入的卡片", Description: "经由 SSE 到达", Position: 2000,
		Version: 1, CreatedAt: fake.now, UpdatedAt: fake.now, UpdatedBy: "mcp",
	}
	fake.push(model.Event{Type: model.EventTaskCreated, ProjectID: "local", ID: created.ID, Version: 1, By: "mcp", Task: &created})
	waitFor(t, tt, "推送的卡片", func() bool { return tt.HasText("由代理写入的卡片") })

	inReview := false
	for _, task := range b.tasksIn("c-review") {
		if task.ID == created.ID {
			inReview = true
		}
	}
	if !inReview {
		t.Fatal("pushed card did not land in its column")
	}
	if !tt.HasText("mcp") {
		t.Errorf("agent badge missing; texts: %v", tt.Texts())
	}

	moved := created
	moved.ColumnID = "c-done"
	moved.Version = 2
	fake.push(model.Event{Type: model.EventTaskMoved, ProjectID: "local", ID: moved.ID, Version: 2, By: "mcp", Task: &moved,
		Column: &struct {
			FromID string `json:"fromId"`
			ToID   string `json:"toId"`
		}{FromID: "c-review", ToID: "c-done"}})
	waitFor(t, tt, "卡片移动", func() bool {
		for _, task := range b.tasksIn("c-done") {
			if task.ID == moved.ID {
				return true
			}
		}
		return false
	})

	archived := moved
	archived.Version = 3
	archived.Archived = true
	fake.push(model.Event{Type: model.EventTaskArchived, ProjectID: "local", ID: archived.ID, Version: 3, By: "mcp", Task: &archived})
	waitFor(t, tt, "卡片归档", func() bool { return b.task(archived.ID) == nil })
	if tt.HasText("由代理写入的卡片") {
		t.Error("archived card still drawn")
	}
}

// TestOfflineBanner checks the banner appears when the stream drops and
// that the board stays readable underneath.
func TestOfflineBanner(t *testing.T) {
	fake := seedBoard()
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })

	b.post(func() { b.status = client.Offline })
	waitFor(t, tt, "离线横幅", func() bool { return tt.HasText("本地服务未连接，正在重试…") })
	if !tt.HasText("重试") {
		t.Error("offline banner has no retry button")
	}
	if !tt.HasText("把看板接上 SSE") {
		t.Error("offline banner hid the loaded board")
	}
}

// TestConflictAligns checks a stale write raises the conflict alert with
// the server's version, and that choosing the server copy is silent.
func TestConflictAligns(t *testing.T) {
	fake := seedBoard()
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })

	if err := tt.Click("把看板接上 SSE"); err != nil {
		t.Fatalf("click card: %v", err)
	}
	tt.Frame()

	// The version the dialog opened with, before another agent writes.
	staleVersion := b.task("t-1").Version
	newer := *b.task("t-1")
	newer.Version = 7
	newer.Title = "被代理改过的标题"
	newer.UpdatedBy = "mcp"
	fake.external(newer)
	waitFor(t, tt, "推送的更新", func() bool { return b.task("t-1").Version == 7 })
	if b.detail.title != "被代理改过的标题" {
		t.Fatalf("open detail did not follow the pushed update: %q", b.detail.title)
	}

	// A save carrying the old version must conflict.
	title := "本地改的标题"
	_, err := fake.Update(newer.ID, staleVersion, &title, nil, nil)
	if err == nil {
		t.Fatal("stale update did not conflict")
	}
	ce, ok := err.(*client.ConflictError)
	if !ok {
		t.Fatalf("conflict error = %T %v", err, err)
	}
	b.post(func() {
		b.recover("t-1", ce, func(int) {})
	})
	waitFor(t, tt, "冲突弹窗", func() bool { return tt.HasText("任务已被修改") })
	if !tt.HasText("保留我的修改") || !tt.HasText("使用最新版本") {
		t.Fatalf("conflict alert lacks choices; texts: %v", tt.Texts())
	}
	if b.task("t-1").Title != "被代理改过的标题" {
		t.Fatalf("local copy not aligned: %q", b.task("t-1").Title)
	}
}

// TestAgentUpdateFlashesCard checks a card changed by another agent is
// drawn with the accent wash for a moment.
func TestAgentUpdateFlashesCard(t *testing.T) {
	fake := seedBoard()
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })

	other := b.task("t-2")
	cp := *other
	cp.Version = 9
	cp.Title = "别人改过的卡片"
	cp.UpdatedBy = "mcp"
	fake.push(model.Event{Type: model.EventTaskUpdated, ProjectID: "local", ID: cp.ID, Version: 9, By: "mcp", Task: &cp})
	waitFor(t, tt, "外部更新", func() bool { return tt.HasText("别人改过的卡片") })
	if _, ok := b.recent["t-2"]; !ok {
		t.Fatal("external update did not mark the card as changed")
	}
	time.Sleep(700 * time.Millisecond)
	tt.Frame()
	if _, ok := b.recent["t-2"]; ok {
		t.Fatal("card still marked as changed after the wash faded")
	}
}
