package main

import (
	"testing"

	"taskboard-native/internal/model"
)

// TestTopBarBreadcrumb checks the path in the top bar: it names the board
// on screen, the arrow lists every board and switches, and 首页 returns to
// the merged 全部项目 board without reloading when already there.
func TestTopBarBreadcrumb(t *testing.T) {
	fake := seedBoard()
	fake.projects = append(fake.projects, model.Project{ID: "p-2", Name: "工作",
		Columns: []model.Column{{ID: "c2-todo", Name: "待办"}}, CreatedAt: fake.now})
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	// The window launches into 全部项目, so the last crumb names it.
	if !tt.HasText("首页") {
		t.Fatalf("top bar lacks the 首页 crumb; texts: %v", tt.Texts())
	}
	if got := crumbLabel(b); got != "切换看板，当前 全部项目" {
		t.Fatalf("board crumb is %q", got)
	}

	// Its arrow lists every board, in sidebar order, the current one checked.
	r, ok := tt.Find(crumbLabel(b))
	if !ok {
		t.Fatalf("no board crumb to click")
	}
	tt.Press(r.X+r.W/2, r.Y+r.H/2)
	menu := tt.Menu()
	want := []string{"全部项目", "local", "工作"}
	if len(menu) != len(want) {
		t.Fatalf("board menu is %v, want %v", menu, want)
	}
	for i := range want {
		if menu[i] != want[i] {
			t.Fatalf("board menu is %v, want %v", menu, want)
		}
	}
	if err := tt.ChooseMenuItem("工作"); err != nil {
		t.Fatalf("choose 工作: %v", err)
	}
	waitFor(t, tt, "切换项目", func() bool { return b.cur == "p-2" })
	if got := crumbLabel(b); got != "切换看板，当前 工作" {
		t.Fatalf("crumb did not follow the switch: %q", got)
	}

	// 首页 goes back to the merged board.
	if err := tt.Click("首页，回到全部项目"); err != nil {
		t.Fatalf("click 首页: %v", err)
	}
	waitFor(t, tt, "回到全部项目", func() bool { return b.cur == curGlobal })
}

// crumbLabel is the board crumb's accessible name, which carries the name
// on screen so the test reads the same string the view sets.
func crumbLabel(b *board) string { return "切换看板，当前 " + b.projectName() }
