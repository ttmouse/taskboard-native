package main

import (
	"errors"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"taskboard-native/internal/client"
	"taskboard-native/internal/model"

	"github.com/egoist/mygo"
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
	return saveFrame(t, tt, name, renderW, renderH)
}

func saveFrame(t *testing.T, tt *ui.Tester, name string, width, height int) string {
	t.Helper()
	path := filepath.Join(renderDir(t), name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	img := tt.Image()
	b := img.Bounds()
	if b.Dx() != width || b.Dy() != height {
		t.Fatalf("%s is %dx%d, want %dx%d", name, b.Dx(), b.Dy(), width, height)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
	return path
}

// meanLuma is the average brightness of an image, 0 (black) to 1 (white).
func meanLuma(img *image.RGBA) float64 {
	b := img.Bounds()
	var sum float64
	n := 0
	for y := b.Min.Y; y < b.Max.Y; y += 2 {
		for x := b.Min.X; x < b.Max.X; x += 2 {
			r, g, bl, _ := img.At(x, y).RGBA()
			sum += (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(bl)) / 65535
			n++
		}
	}
	return sum / float64(n)
}

// contrastRatio is WCAG's contrast between two colors, 1 to 21.
func contrastRatio(a, b ui.Color) float64 {
	la, lb := relLuma(a), relLuma(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func relLuma(c ui.Color) float64 {
	ch := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
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

// settle frames the view past the board's entrance animation, so a
// snapshot shows the resting board rather than columns mid-rise.
func settle(t *testing.T, tt *ui.Tester) {
	t.Helper()
	tt.Advance(700 * time.Millisecond)
	tt.Frame()
}

// isolatePrefs points the app's user-data path and TASKBOARD_DB at a temp
// dir for one test, so a frame that saves ui-prefs.json (a sidebar resize,
// a toggle) cannot leak into the developer's real prefs and change what the
// next test renders.
func isolatePrefs(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TASKBOARD_DB", filepath.Join(dir, "tb.db"))
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })
}

// TestRenderBoardLightAndDark renders the full board in both appearances
// and checks the Chinese chrome is on screen.
func TestRenderBoardLightAndDark(t *testing.T) {
	isolatePrefs(t)
	fake := seedBoard()
	b := newBoard(fake)
	tt := newBoardTester(b)

	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	waitFor(t, tt, "看板列", func() bool { return tt.HasText("待办") })
	settle(t, tt)

	for _, want := range []string{
		"local", "待办", "进行中", "评审", "完成",
		"打开设置", "新建项目", // settings icon a11y + sidebar plus
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
	for _, want := range []string{"待办", "把看板接上 SSE", "新建项目"} {
		if !tt.HasText(want) {
			t.Errorf("dark frame lacks %q", want)
		}
	}
	// The dark appearance must really be dark: a light card face under
	// light text would be unreadable, so check both the frame's overall
	// brightness and the card's contrast against its text.
	if got := meanLuma(tt.Image()); got > 0.25 {
		t.Errorf("dark frame has mean brightness %.2f, want under 0.25", got)
	}
	dark := ui.DarkTheme()
	if got := contrastRatio(dark.Text, cardFace(dark)); got < 7 {
		t.Errorf("dark card contrast is %.1f:1, want at least 7:1", got)
	}
	if got := contrastRatio(ui.LightTheme().Text, cardFace(ui.LightTheme())); got < 7 {
		t.Errorf("light card contrast is %.1f:1, want at least 7:1", got)
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

	waitFor(t, tt, "空看板", func() bool {
		return tt.HasText("待办") && tt.HasText("完成")
	})
	settle(t, tt)
	if tt.HasText("还没有任务") || tt.HasText("添加任务，或拖入") {
		t.Fatalf("empty columns should show no reminder; texts: %v", tt.Texts())
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
	isolatePrefs(t)
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
	// The dialog was opened, not dirty: it follows the pushed copy.
	if b.detail.title != "被代理改过的标题" {
		t.Fatalf("clean detail did not follow the pushed update: %q", b.detail.title)
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

// TestEditMoveArchiveAndComment exercises the detail dialog's writes: a
// title edit is saved with the version the dialog opened with, a column
// move relocates the card, a comment is posted, and archive removes it.
func TestEditMoveArchiveAndComment(t *testing.T) {
	fake := seedBoard()
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })

	if err := tt.Click("把看板接上 SSE"); err != nil {
		t.Fatalf("click card: %v", err)
	}
	tt.Frame()

	// Editing the title and saving patches the service.
	b.detail.title = "把看板接上 SSE（已改）"
	if err := tt.Click("保存"); err != nil {
		t.Fatalf("click save: %v", err)
	}
	waitFor(t, tt, "标题保存", func() bool {
		task, err := fake.Task("t-1")
		return err == nil && task.Title == "把看板接上 SSE（已改）"
	})
	waitFor(t, tt, "弹层关闭", func() bool { return b.detail == nil })
	if !tt.HasText("把看板接上 SSE（已改）") {
		t.Errorf("board does not show the new title; texts: %v", tt.Texts())
	}

	// Reopening and choosing another column moves the card.
	if err := tt.Click("把看板接上 SSE（已改）"); err != nil {
		t.Fatalf("reopen card: %v", err)
	}
	tt.Frame()
	b.detail.column = "完成"
	b.post(func() {
		if colID := b.columnID("完成"); colID != "" {
			b.moveTask("t-1", colID, "", false)
		}
	})
	waitFor(t, tt, "移动列", func() bool {
		task, err := fake.Task("t-1")
		return err == nil && task.ColumnID == "c-done"
	})

	// A comment posts, and shows up in the detail.
	b.detail.comment = "已从窗口发送"
	if err := tt.Click("发送"); err != nil {
		t.Fatalf("click send: %v", err)
	}
	waitFor(t, tt, "评论发表", func() bool {
		comments, err := fake.Comments("t-1")
		if err != nil {
			return false
		}
		for _, cm := range comments {
			if cm.Text == "已从窗口发送" {
				return true
			}
		}
		return false
	})

	// Archive removes the card and offers an undo.
	if err := tt.Click("归档"); err != nil {
		t.Fatalf("click archive: %v", err)
	}
	waitFor(t, tt, "归档", func() bool {
		task, _ := fake.Task("t-1")
		return task.Archived
	})
	waitFor(t, tt, "归档提示", func() bool { return tt.HasText("撤销") })
	if b.task("t-1") != nil {
		t.Fatal("archived card still on the board")
	}

	// Undo clears the flag on the service and the pushed event redraws the
	// card, in the column it was archived from.
	archivedTask, err := fake.Task("t-1")
	if err != nil || !archivedTask.Archived {
		t.Fatalf("archived task = %+v, %v", archivedTask, err)
	}
	b.post(func() { b.restore(archivedTask) })
	waitFor(t, tt, "撤销归档", func() bool { return b.task("t-1") != nil })
	if tt.HasText("还没有任务") && len(b.tasksIn("c-done")) == 0 {
		t.Error("restored card did not come back to its column")
	}
	restored, err := fake.Task("t-1")
	if err != nil || restored.Archived {
		t.Fatalf("restored task = %+v, %v", restored, err)
	}
	if restored.ColumnID != "c-done" {
		t.Fatalf("restored into %q, want its original column c-done", restored.ColumnID)
	}
}

// TestProjectSwitchAndCreateTask checks the project sidebar switches boards and
// that the column input creates a card.
func TestProjectSwitchAndCreateTask(t *testing.T) {
	isolatePrefs(t)
	fake := seedBoard()
	fake.projects = append(fake.projects, model.Project{ID: "p-2", Name: "工作",
		Columns: []model.Column{{ID: "c2-todo", Name: "待办"}}, CreatedAt: fake.now})
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })

	// The project sidebar lists both projects; click switches boards.
	if !tt.HasText("工作") {
		t.Fatalf("project sidebar missing 工作; texts: %v", tt.Texts())
	}
	if err := tt.Click("切换到项目 工作"); err != nil {
		// Fall back to visible label if accessibility label differs.
		if err2 := tt.Click("工作"); err2 != nil {
			t.Fatalf("choose project: %v / %v", err, err2)
		}
	}
	waitFor(t, tt, "切换项目", func() bool { return b.cur == "p-2" })
	if tt.HasText("把看板接上 SSE") {
		t.Error("old project's cards survived the switch")
	}

	// Creating a card (sidebar/API path) still lands it in the column.
	b.post(func() { b.createTop(b.cur, "c2-todo", "从侧栏路径新建的卡片") })
	waitFor(t, tt, "新建卡片", func() bool { return tt.HasText("从侧栏路径新建的卡片") })
}

// TestDirtyDetailSurvivesPush checks an unsaved edit is not overwritten by
// an agent's pushed update, and that saving it goes through the conflict
// dialog instead of silently closing as "no changes".
func TestDirtyDetailSurvivesPush(t *testing.T) {
	fake := seedBoard()
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })

	if err := tt.Click("把看板接上 SSE"); err != nil {
		t.Fatalf("click card: %v", err)
	}
	tt.Frame()
	b.detail.title = "我正在写的新标题"
	tt.Frame()

	cp := *b.task("t-1")
	cp.Version = 8
	cp.Title = "被代理抢先改的标题"
	cp.UpdatedBy = "mcp"
	// external updates the fake's authoritative copy as well as emitting
	// the event, so a save built on the pre-push version would succeed
	// against the live service — exactly the real-world hazard.
	fake.external(cp)
	waitFor(t, tt, "推送到达看板", func() bool { return b.task("t-1").Version == 8 })

	if b.detail.title != "我正在写的新标题" {
		t.Fatalf("pushed update overwrote the unsaved title: %q", b.detail.title)
	}
	if !tt.HasText("你的编辑尚未保存") {
		t.Errorf("detail did not warn about the incoming update; texts: %v", tt.Texts())
	}

	tt.Frame()
	if err := tt.Click("保存"); err != nil {
		t.Fatalf("click save: %v", err)
	}
	waitFor(t, tt, "冲突弹窗", func() bool { return b.conflict != nil && tt.HasText("任务已被修改") })
	if b.detail == nil {
		t.Fatal("saving a dirty stale dialog closed it instead of asking")
	}
}

// TestDarkAccentMeetsContrast checks the dark frame hands mygo a theme
// whose accent keeps white button text above AA (4.5:1), instead of the
// stock #3b82f6 (3.68:1).
func TestDarkAccentMeetsContrast(t *testing.T) {
	fake := seedBoard()
	b := newBoard(fake)
	tt := newBoardTester(b)
	tt.SetDark(true)
	tt.Frame()
	waitFor(t, tt, "暗色渲染", func() bool { return tt.HasText("待办") })

	accent := b.curTheme.Accent
	want := [3]uint8{0x25, 0x63, 0xeb}
	if accent.R != want[0] || accent.G != want[1] || accent.B != want[2] {
		t.Fatalf("dark accent = #%02x%02x%02x, want the AA-safe #2563eb", accent.R, accent.G, accent.B)
	}
}

// TestHoverFaceDiffersFromColumn checks a hovered card does not vanish
// into its column in the light appearance (the reported 1.00:1 bug).
func TestHoverFaceDiffersFromColumn(t *testing.T) {
	light := ui.LightTheme()
	if cardHover(light) == colFace(light) {
		t.Fatal("light hover face equals the column face: the card disappears")
	}
	dark := ui.DarkTheme()
	if cardHover(dark) == colFace(dark) {
		t.Fatal("dark hover face equals the column face")
	}
	if cardFace(light) == colFace(light) {
		t.Fatal("light resting face equals the column face")
	}
}

// Full-height drop projection is covered by TestDragProjectionReflowsAndCancels
// and TestProjectedDropCommitsDisplayedSlot in motion_test.go.

// The smallest supported window must leave the dialog actions reachable.
func TestDetailFitsMinimumWindow(t *testing.T) {
	b := newBoard(seedBoard())
	tt := ui.NewTester(b.view, 960, 560)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	b.openDetail(*b.task("t-1"))
	waitFor(t, tt, "评论载入", func() bool { return b.detail != nil && !b.detail.loading })
	for _, label := range []string{"关闭详情", "发送评论", "保存任务", "归档任务"} {
		r, ok := tt.Find(label)
		if !ok || r.Y < 0 || r.Y+r.H > 560 || r.X < 0 || r.X+r.W > 960 {
			t.Errorf("%s is outside minimum window: %+v (found %v)", label, r, ok)
		}
	}
}

func TestCommentEnterSends(t *testing.T) {
	fake := seedBoard()
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	b.openDetail(*b.task("t-1"))
	waitFor(t, tt, "评论载入", func() bool { return b.detail != nil && !b.detail.loading })
	if err := tt.Click("写评论"); err != nil {
		t.Fatal(err)
	}
	tt.Type("回车发送的评论")
	tt.Key(0, ui.KeyEnter)
	waitFor(t, tt, "回车发送", func() bool {
		comments, _ := fake.Comments("t-1")
		for _, cm := range comments {
			if cm.Text == "回车发送的评论" {
				return true
			}
		}
		return false
	})
}

func TestRenderDetailSizes(t *testing.T) {
	for _, size := range []struct {
		name string
		w, h int
		dark bool
	}{
		{"detail-light.png", 1280, 800, false},
		{"detail-dark.png", 1280, 800, true},
		{"detail-minimum.png", 960, 560, false},
	} {
		t.Run(size.name, func(t *testing.T) {
			b := newBoard(seedBoard())
			tt := ui.NewTester(b.view, size.w, size.h)
			tt.SetDark(size.dark)
			waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
			b.openDetail(*b.task("t-1"))
			waitFor(t, tt, "评论载入", func() bool { return !b.detail.loading })
			settle(t, tt)
			saveFrame(t, tt, size.name, size.w, size.h)
			// Force the content taller than any viewport. Fixed controls must
			// remain inside the dialog while only the content scrolls.
			b.detail.comments = append(b.detail.comments, model.Comment{
				ID: "long-comment", Author: strings.Repeat("agent-", 30),
				Text: strings.Repeat("这是一条用于检查滚动边界的长评论。", 100),
			})
			tt.Frame()
			for _, label := range []string{"关闭详情", "写评论", "发送评论", "保存任务", "归档任务"} {
				r, ok := tt.Find(label)
				if !ok || r.Y < 0 || r.Y+r.H > float32(size.h) {
					t.Errorf("long content hides %s: %+v (found %v)", label, r, ok)
				}
			}
			if size.h == 560 {
				saveFrame(t, tt, "detail-long-minimum.png", size.w, size.h)
			}
		})
	}
}

type failedCommentAPI struct{ api }

func (f failedCommentAPI) AddComment(string, string) (model.Comment, error) {
	return model.Comment{}, errors.New("测试服务不可用")
}

func TestCommentFailureRestoresDraft(t *testing.T) {
	b := newBoard(failedCommentAPI{seedBoard()})
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
	b.openDetail(*b.task("t-1"))
	waitFor(t, tt, "评论载入", func() bool { return !b.detail.loading })
	b.detail.comment = "不能丢失的评论草稿"
	b.sendComment()
	if !b.commenting {
		t.Fatal("send did not enter pending state")
	}
	b.detail.comment = "另一次提交"
	b.sendComment()
	if b.detail.comment != "另一次提交" {
		t.Fatal("duplicate submission was not blocked")
	}
	b.detail.comment = ""
	waitFor(t, tt, "失败后恢复草稿", func() bool { return !b.commenting })
	if b.detail.comment != "不能丢失的评论草稿" {
		t.Fatalf("failed comment lost draft: %q", b.detail.comment)
	}
}

func TestDarkHighContrastPreservesFocus(t *testing.T) {
	b := newBoard(seedBoard())
	tt := newBoardTester(b)
	tt.SetDark(true)
	tt.SetPreferences(ui.Preferences{HighContrast: true})
	tt.Frame()
	if b.curTheme.Focus.A != 255 {
		t.Fatalf("high contrast focus was weakened: alpha %d", b.curTheme.Focus.A)
	}
}

func TestRenderBoardWindowSizes(t *testing.T) {
	for _, width := range []int{960, 1800} {
		fake := seedBoard()
		if width == 960 {
			fake.tasks[0].Title = strings.Repeat("检查超长任务标题能否省略", 8)
			fake.tasks[0].UpdatedBy = strings.Repeat("agent-", 30)
		}
		b := newBoard(fake)
		tt := ui.NewTester(b.view, width, 800)
		waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
		settle(t, tt)
		saveFrame(t, tt, "board-"+strconv.Itoa(width)+".png", width, 800)
	}
}

// TestTitleOnlyCards checks the denser card layout hides summary + footer
// while titles, click-to-open, and the settings sheet stay available.
func TestTitleOnlyCards(t *testing.T) {
	dir := t.TempDir()
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })
	t.Setenv("TASKBOARD_DB", "")

	fake := seedBoard()
	b := newBoard(fake)
	tt := newBoardTester(b)

	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	summary := "事件到达后就地更新卡片，不轮询、不整表重建。"
	if !tt.HasText(summary) {
		t.Fatalf("full card missing description summary %q", summary)
	}
	if !tt.HasText("1 条评论") {
		t.Fatalf("full card missing comment badge")
	}

	b.prefs.TitleOnlyCards = true
	tt.Frame()
	settle(t, tt)

	for _, want := range []string{"把看板接上 SSE", "原生窗口渲染验收", "版本冲突时拉一次详情对齐"} {
		if !tt.HasText(want) {
			t.Errorf("title-only frame lacks title %q", want)
		}
	}
	if tt.HasText(summary) {
		t.Errorf("title-only frame still shows description summary")
	}
	if tt.HasText("1 条评论") {
		t.Errorf("title-only frame still shows comment badge")
	}

	if err := tt.Click("打开设置"); err != nil {
		t.Fatalf("click 打开设置: %v", err)
	}
	tt.Frame()
	if !tt.HasText("卡片仅显示标题") {
		t.Fatalf("settings sheet missing toggle; texts: %v", tt.Texts())
	}
	if err := tt.Click("卡片仅显示标题"); err != nil {
		t.Fatalf("click toggle: %v", err)
	}
	tt.Frame()
	settle(t, tt)
	if b.prefs.TitleOnlyCards {
		t.Fatalf("toggle did not turn title-only off")
	}
	got := loadUIPrefs()
	if got.TitleOnlyCards {
		t.Fatalf("prefs file still has titleOnlyCards=true")
	}
	if !tt.HasText(summary) {
		t.Errorf("full cards should show summary after toggle off")
	}
}
