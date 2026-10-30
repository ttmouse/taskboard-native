package main

import (
	"testing"

	"taskboard-native/internal/model"

	"github.com/egoist/mygo/ui"
)

// TestProjectFormColorChipsRender asserts the project form shows the palette
// swatches (so the 自动 slot and every hex chip are interactive) and that a
// chosen chip is carried into the form state.
func TestProjectFormColorChipsRender(t *testing.T) {
	isolatePrefs(t)
	fake := seedBoard()
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)
	b.form = &form{open: true}
	tt.Frame()
	if !tt.HasText("卡片颜色") {
		t.Fatal("project form lacks 卡片颜色 label")
	}
	if !tt.HasText("自动") {
		t.Fatal("project form lacks 自动 swatch label")
	}
	for _, color := range projectColorPalette {
		label := "项目颜色 " + color
		if !tt.HasText(label) {
			t.Fatalf("palette chip %s missing", label)
		}
	}
}

// TestProjectFormColorClickAdoptsAndPersists: clicking a swatch fills
// form.color; saving a new project sends that color to the api (the fake
// keeps it); the edited project form pre-fills its saved color.
func TestProjectFormColorClickAdoptsAndPersists(t *testing.T) {
	isolatePrefs(t)
	fake := seedBoard()
	fake.delay = 0
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	b.form = &form{open: true}
	tt.Frame()
	tt.Type("彩色项目")
	// 选一个非自动的色板颜色（#10b981 = emerald）
	if err := tt.Click("项目颜色 #10b981"); err != nil {
		t.Fatal(err)
	}
	if b.form == nil || b.form.color != "#10b981" {
		t.Fatalf("chip did not adopt color: form=%+v", b.form)
	}
	// 用创建按钮提交，避免点击色板后焦点不在输入框导致 Enter 落空
	if err := tt.Click("创建"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, "项目创建完成", func() bool {
		return len(fake.projects) == 2 && fake.projects[1].Color == "#10b981"
	})
	created := fake.projects[1]
	if created.Name != "彩色项目" || created.Color != "#10b981" {
		t.Fatalf("created project = %+v", created)
	}

	// 编辑回填：打开编辑表单，颜色应预填为已保存值。
	b.openProjectEdit(created)
	tt.Frame()
	if b.form == nil || b.form.color != "#10b981" {
		t.Fatalf("edit form did not prefill color: form=%+v", b.form)
	}
}

// TestProjectFormAutoSlotClearsColor: the 自动 swatch resets form.color to
// empty so the window's stable palette slot applies.
func TestProjectFormAutoSlotClearsColor(t *testing.T) {
	isolatePrefs(t)
	fake := seedBoard()
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)
	b.form = &form{open: true, color: "#2563eb"}
	tt.Frame()
	if err := tt.Click("项目颜色 自动"); err != nil {
		t.Fatal(err)
	}
	if b.form == nil || b.form.color != "" {
		t.Fatalf("自动 did not clear color: form=%+v", b.form)
	}
}

// TestGlobalViewCardWearsProjectColor: in 全部项目 a card shows its
// project's explicit color via the left-edge bar and the project dot. The
// bar is painted by card.Draw; here we verify the dot elements exist (the
// sidebar dot per project and the card dot per task).
func TestGlobalViewCardWearsProjectColor(t *testing.T) {
	isolatePrefs(t)
	fake := seedBoard()
	// Seed a task in project[0] so the global board has a card.
	if len(fake.projects) == 0 {
		t.Fatal("seed board has no project")
	}
	task, err := fake.CreateTask(fake.projects[0].ID, fake.projects[0].Columns[0].ID, "带颜色的卡", "", "")
	if err != nil {
		t.Fatal(err)
	}
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)
	if b.cur != curGlobal {
		t.Fatalf("expected global view, got %q", b.cur)
	}
	// 侧栏项目行有色点（文本匹配，Find 按 label 文本寻址）
	if !tt.HasText("项目颜色") {
		t.Fatal("sidebar lacks project color dot")
	}
	// 卡片项目名旁的色点存在（label = 所属项目 + 项目名）
	if !tt.HasText("所属项目 " + fake.projects[0].Name) {
		t.Fatal("card lacks project color dot")
	}
	// 卡片标题仍可见
	if !tt.HasText(task.Title) {
		t.Fatal("card title lost in global view")
	}
}

// TestProjectColorStableSlot: explicit colors win; empty colors resolve to a
// stable palette slot that stays the same across calls and differs between
// two projects.
func TestProjectColorStableSlot(t *testing.T) {
	isolatePrefs(t)
	fake := seedBoard()
	b := newBoard(fake)
	p0 := model.Project{ID: "p0", Name: "A"}
	p1 := model.Project{ID: "p1", Name: "B"}
	c0 := b.projectColor(p0)
	c0b := b.projectColor(p0)
	if c0 != c0b {
		t.Fatalf("slot not stable: %v vs %v", c0, c0b)
	}
	if c0 == b.projectColor(p1) {
		t.Fatalf("adjacent projects share a slot: %v", c0)
	}
	colored := model.Project{ID: "p2", Name: "C", Color: "#ef4444"}
	if b.projectColor(colored) != ui.Hex("#ef4444") {
		t.Fatalf("explicit color lost: %v", b.projectColor(colored))
	}
}
