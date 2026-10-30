package main

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// TestNewTaskShortcut checks ⌘N opens the same create-task sheet as the
// sidebar 新建任务 row — targeting the current project's 待办 — that in
// 「全部项目」it asks for a project instead, and that it waits while the
// task detail dialog is on top rather than stacking a form under it.
func TestNewTaskShortcut(t *testing.T) {
	dir := t.TempDir()
	// Prefs live beside TASKBOARD_DB; point both inside this test's dir so a
	// frame that saves ui-prefs.json cannot write the developer's real file.
	t.Setenv("TASKBOARD_DB", filepath.Join(dir, "tb.db"))
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })

	b := newBoard(seedBoard())
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	// Launch is 「全部项目」: no column to land in, so ⌘N asks for one.
	if b.cur != curGlobal {
		t.Fatalf("launch cur = %q, want curGlobal", b.cur)
	}
	tt.Key(ui.Cmd, ui.KeyN)
	waitFor(t, tt, "新建提示", func() bool { return tt.HasText("请先在侧栏选一个项目") })
	if b.form != nil {
		t.Fatal("⌘N in 全部项目 opened a form without a project")
	}

	// On a project board ⌘N opens the task form for that project's 待办.
	if err := tt.Click("切换到项目 local"); err != nil {
		t.Fatalf("choose project: %v", err)
	}
	waitFor(t, tt, "切换项目", func() bool { return b.cur == "local" })
	waitFor(t, tt, "看板列到达", func() bool { return len(b.columns) > 0 })
	tt.Key(ui.Cmd, ui.KeyN)
	waitFor(t, tt, "新建任务浮层", func() bool {
		return b.form != nil && b.form.open && b.form.isTask
	})
	if b.form.projectID != "local" || b.form.columnID != "c-todo" {
		t.Fatalf("form targets %q / %q, want local / c-todo", b.form.projectID, b.form.columnID)
	}

	// From 全部项目, the next shortcut reuses the last project.
	tt.Key(0, ui.KeyEscape)
	waitFor(t, tt, "浮层关闭", func() bool { return b.form == nil })
	b.openGlobal()
	tt.Key(ui.Cmd, ui.KeyN)
	waitFor(t, tt, "全局新建任务浮层", func() bool {
		return b.form != nil && b.form.open && b.form.isTask
	})
	if b.form.projectID != "local" {
		t.Fatalf("global shortcut targets %q, want remembered project local", b.form.projectID)
	}

	// While the detail dialog is up, the window shortcut waits.
	tt.Key(0, ui.KeyEscape)
	waitFor(t, tt, "浮层关闭", func() bool { return b.form == nil })
	if err := tt.Click("打开任务：把看板接上 SSE"); err != nil {
		t.Fatalf("open detail: %v", err)
	}
	waitFor(t, tt, "详情打开", func() bool { return b.detail != nil && b.detail.open })
	tt.Key(ui.Cmd, ui.KeyN)
	tt.Frame()
	if b.form != nil {
		t.Fatal("⌘N stacked a create form under the detail dialog")
	}
}
