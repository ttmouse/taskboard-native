package main

import (
	"testing"
	"time"

	"taskboard-native/internal/model"
)

// TestProjectSortControl: the sidebar header has a sort dropdown left of 添加项目;
// choosing 按更新时间 / 按创建时间 reorders the project list, and the state
// persists across renders.
func TestProjectSortControl(t *testing.T) {
	isolatePrefs(t)
	fake := seedBoard()
	// Three extra projects with distinct activity / creation times.
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	fake.projects = append(fake.projects,
		model.Project{ID: "p-old", Name: "旧项目", Columns: fake.projects[0].Columns, CreatedAt: now.AddDate(0, 0, -20), ActiveAt: now.AddDate(0, 0, -20)},
		model.Project{ID: "p-hot", Name: "活跃项目", Columns: fake.projects[0].Columns, CreatedAt: now.AddDate(0, 0, -5), ActiveAt: now.Add(-1 * time.Hour)},
		model.Project{ID: "p-new", Name: "新项目", Columns: fake.projects[0].Columns, CreatedAt: now.AddDate(0, 0, -1), ActiveAt: now.Add(-2 * time.Hour)},
	)
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return !b.loading })
	settle(t, tt)

	yOf := func(label string) float32 {
		e, ok := tt.Find(label)
		if !ok {
			t.Fatalf("project row %q not found", label)
		}
		return e.Y
	}

	// Default order keeps the server order.
	if got, want := yOf("旧项目") < yOf("活跃项目"), true; got != want {
		t.Fatalf("default order: 旧项目(%v) should be above 活跃项目(%v)", yOf("旧项目"), yOf("活跃项目"))
	}

	// Open the sort menu from the「排序」button.
	btn, ok := tt.Find("项目排序")
	if !ok {
		t.Fatal("missing project sort control")
	}
	tt.Press(btn.X+btn.W/2, btn.Y+btn.H/2)
	menu := tt.Menu()
	want := []string{"默认排序", "按更新时间", "按创建时间"}
	if len(menu) != len(want) {
		t.Fatalf("sort menu is %v, want %v", menu, want)
	}
	for i := range want {
		if menu[i] != want[i] {
			t.Fatalf("sort menu is %v, want %v", menu, want)
		}
	}

	// 按更新时间 → 最近活跃的项目浮到最上。
	if err := tt.ChooseMenuItem("按更新时间"); err != nil {
		t.Fatal(err)
	}
	settle(t, tt)
	if b.projectSort != "updated" {
		t.Fatalf("projectSort = %q, want updated", b.projectSort)
	}
	if got := loadUIPrefs().ProjectSort; got != "updated" {
		t.Fatalf("persisted ProjectSort = %q, want updated", got)
	}
	if got, want := yOf("活跃项目") < yOf("新项目"), true; got != want {
		t.Fatalf("updated order: 活跃项目(%v) should be above 新项目(%v)", yOf("活跃项目"), yOf("新项目"))
	}
	if got, want := yOf("新项目") < yOf("旧项目"), true; got != want {
		t.Fatalf("updated order: 新项目(%v) should be above 旧项目(%v)", yOf("新项目"), yOf("旧项目"))
	}

	// 按创建时间 → 最新创建的项目浮到最上。
	if err := tt.Click("项目排序"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("按创建时间"); err != nil {
		t.Fatal(err)
	}
	settle(t, tt)
	if b.projectSort != "created" {
		t.Fatalf("projectSort = %q, want created", b.projectSort)
	}
	if got := loadUIPrefs().ProjectSort; got != "created" {
		t.Fatalf("persisted ProjectSort = %q, want created", got)
	}
	restarted := newBoard(fake)
	if restarted.projectSort != "created" {
		t.Fatalf("projectSort after new board startup = %q, want created", restarted.projectSort)
	}
	if got, want := yOf("新项目") < yOf("活跃项目"), true; got != want {
		t.Fatalf("created order: 新项目(%v) should be above 活跃项目(%v)", yOf("新项目"), yOf("活跃项目"))
	}
	if got, want := yOf("活跃项目") < yOf("旧项目"), true; got != want {
		t.Fatalf("created order: 活跃项目(%v) should be above 旧项目(%v)", yOf("活跃项目"), yOf("旧项目"))
	}
}
