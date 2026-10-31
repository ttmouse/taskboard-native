package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"taskboard-native/internal/client"
	"taskboard-native/internal/executor"
	"taskboard-native/internal/model"
)

func TestProjectRunClearsDoingAndIntakeQueuesAutomatically(t *testing.T) {
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
	if _, err := c.StartProjectRun(p.ID, executor.RuntimeCodex); err != nil {
		t.Fatal(err)
	}
	// The stalled doing card is dispatched first; when its session returns the
	// service moves it to 等你 by itself and writes the move note.
	waitFor(t, func() bool {
		current, err := c.Task(first.ID)
		return err == nil && current.ColumnID == review.ID
	})
	noted, err := c.Task(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	autoMove := false
	for _, cm := range noted.Comments {
		if cm.Author == "自动承接" && strings.Contains(cm.Text, "自动移入「等你」") {
			autoMove = true
		}
	}
	if !autoMove {
		t.Fatalf("comments = %#v; want a 自动承接 move note on the finished card", noted.Comments)
	}
	// The next card starts on its own and lands in 等你 too.
	waitFor(t, func() bool {
		current, err := c.Task(second.ID)
		return err == nil && current.ColumnID == review.ID
	})
	waitRunEmpty := func() bool {
		run, err := c.ProjectRun(p.ID)
		return err == nil && run.Running() && run.CurrentTaskID == ""
	}
	waitFor(t, waitRunEmpty)

	logBytes, err := os.ReadFile(srv.DBPath + ".auto-handoff.jsonl")
	if err != nil {
		t.Fatalf("read automatic handoff log: %v", err)
	}
	logText := string(logBytes)
	for _, event := range []string{"run.start_requested", "run.queue_scanned", "task.dispatch_started", "task.dispatch_accepted", "task.execution_converged", "task.moved_to_review"} {
		if !strings.Contains(logText, `"event":"`+event+`"`) {
			t.Errorf("automatic handoff log missing %q event: %s", event, logText)
		}
	}
	if strings.Contains(logText, "停住的卡") || strings.Contains(logText, "RUN_OK") {
		t.Fatalf("automatic handoff log contains task content or agent output: %s", logText)
	}
	// A card created while the switch is on is picked up and cleared as well.
	third, err := c.CreateTask(p.ID, intake.ID, "开关开启后新建", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		current, err := c.Task(third.ID)
		return err == nil && current.ColumnID == review.ID
	})
	waitFor(t, waitRunEmpty)
}

func TestProjectRunClaimsActiveIntakeExecutionWithoutRedispatch(t *testing.T) {
	workspace := t.TempDir()
	srv := start(t)
	c := client.New(&client.Discover{Addr: srv.Addr}, "project-run")
	p, err := c.CreateProjectWithPath("已有会话项目", workspace)
	if err != nil {
		t.Fatal(err)
	}
	intake, _ := p.ColumnByRole(model.ColumnRoleIntake)
	doing, _ := p.ColumnByRole(model.ColumnRoleDoing)
	task, err := c.CreateTask(p.ID, intake.ID, "待办中的活跃会话", "", "")
	if err != nil {
		t.Fatal(err)
	}
	existing := model.Execution{
		TaskID: task.ID, Runtime: executor.RuntimeCodex,
		SessionID: "herdr:existing", Status: "running",
	}
	if err := srv.store.SaveExecution(existing); err != nil {
		t.Fatal(err)
	}

	projectRun, err := c.StartProjectRun(p.ID, executor.RuntimeCodex)
	if err != nil {
		t.Fatal(err)
	}
	if projectRun.CurrentTaskID != task.ID || projectRun.Detail == "" {
		t.Fatalf("project run = %#v; want to track the existing execution", projectRun)
	}
	claimed, err := c.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ColumnID != doing.ID {
		t.Fatalf("active intake card stayed in %q, want doing %q", claimed.ColumnID, doing.ID)
	}
	current, err := srv.store.Execution(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.SessionID != existing.SessionID {
		t.Fatalf("project run replaced active session %q with %q", existing.SessionID, current.SessionID)
	}
	logBytes, err := os.ReadFile(srv.DBPath + ".auto-handoff.jsonl")
	if err != nil {
		t.Fatalf("read automatic handoff log: %v", err)
	}
	if !strings.Contains(string(logBytes), `"sessionId":"herdr:existing"`) || !strings.Contains(string(logBytes), `"executionStatus":"running"`) {
		t.Fatalf("automatic handoff log missing active execution details: %s", logBytes)
	}
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
