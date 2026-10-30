package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"taskboard-native/internal/client"
	"taskboard-native/internal/executor"
	"taskboard-native/internal/model"
)

func TestProjectRunDispatchesTheNextCardAfterTheCurrentOneLeavesDoing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte("#!/bin/sh\nprintf 'RUN_OK\\n'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	workspace := t.TempDir()
	installFakeHerdr(t, workspace, "done")
	srv := start(t)
	c := client.New(&client.Discover{Addr: srv.Addr}, "project-run")
	p, err := c.CreateProjectWithPath("承接项目", workspace)
	if err != nil {
		t.Fatal(err)
	}
	intake, _ := p.ColumnByRole(model.ColumnRoleIntake)
	doing, _ := p.ColumnByRole(model.ColumnRoleDoing)
	review, _ := p.ColumnByRole(model.ColumnRoleReview)
	first, err := c.CreateTask(p.ID, doing.ID, "停住的卡", "", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.CreateTask(p.ID, intake.ID, "下一张", "", "")
	if err != nil {
		t.Fatal(err)
	}
	run, err := c.StartProjectRun(p.ID, executor.RuntimeCodex)
	if err != nil {
		t.Fatal(err)
	}
	if len(run.TaskIDs) != 2 || run.TaskIDs[0] != first.ID || run.TaskIDs[1] != second.ID {
		t.Fatalf("list=%v", run.TaskIDs)
	}
	waitFor(t, func() bool {
		current, err := c.Task(first.ID)
		return err == nil && len(current.Comments) > 0
	})
	again, err := c.Task(second.ID)
	if err != nil || len(again.Comments) != 0 {
		t.Fatal("second card started before the first one finished")
	}
	current, err := c.Task(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Move(current.ID, current.Version, review.ID, "", false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		current, err := c.Task(second.ID)
		return err == nil && len(current.Comments) > 0
	})
	left, err := c.Task(second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Move(left.ID, left.Version, review.ID, "", false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		run, err := c.ProjectRun(p.ID)
		return err == nil && run.Running() && run.CurrentTaskID == ""
	})
	third, err := c.CreateTask(p.ID, intake.ID, "开关开启后新建", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		current, err := c.Task(third.ID)
		return err == nil && len(current.Comments) > 0
	})
}

func TestProjectRunCanBeEnabledWhileACardIsStillRunning(t *testing.T) {
	srv := start(t)
	c := client.New(&client.Discover{Addr: srv.Addr}, "project-run")
	p, err := c.CreateProjectWithPath("忙项目", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	intake, _ := p.ColumnByRole(model.ColumnRoleIntake)
	doing, _ := p.ColumnByRole(model.ColumnRoleDoing)
	task, err := c.CreateTask(p.ID, doing.ID, "还在跑", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.store.SaveExecution(model.Execution{TaskID: task.ID, SessionID: "live", Status: "running", Runtime: executor.RuntimeCodex}); err != nil {
		t.Fatal(err)
	}
	run, err := c.StartProjectRun(p.ID, executor.RuntimeDSH)
	if err != nil {
		t.Fatal(err)
	}
	if !run.Running() || run.CurrentTaskID != task.ID || run.Detail != "等待卡片 "+task.ID+" 的现有执行结束" {
		t.Fatalf("run = %#v; want an enabled run waiting on the active card", run)
	}
	queued, err := c.CreateTask(p.ID, intake.ID, "等待中的新卡", "", "")
	if err != nil {
		t.Fatal(err)
	}
	srv.dispatch.NoteProjectRun(queued)
	if execution, err := srv.store.Execution(queued.ID); err != nil || execution != nil {
		t.Fatalf("queued card started while another card is active: execution=%#v err=%v", execution, err)
	}
}

func waitFor(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out")
}
