package executor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"taskboard-native/internal/model"
	"taskboard-native/internal/store"
)

// newTestStore opens a store on a temp database.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "taskboard.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// installFakeCLI puts a stub `codex` on PATH whose behaviour the test scripts.
func installFakeCLI(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "codex")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write fake CLI: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// projectWithWorkspace seeds a project bound to a real directory and one
// task in its first column.
func projectWithWorkspace(t *testing.T, st *store.Store) (model.Project, model.Task) {
	t.Helper()
	p, err := st.CreateProjectWithPath("probe", t.TempDir(), "test")
	if err != nil {
		t.Fatalf("CreateProjectWithPath: %v", err)
	}
	task, err := st.CreateTask(p.ID, p.Columns[0].ID, "跑一次构建", "go build ./...", "", "test")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return p, task
}

// waitFor polls until cond passes or the deadline hits; cond's second
// result is a state description for the timeout message.
func waitFor(t *testing.T, what string, cond func() (bool, string)) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var state string
	for time.Now().Before(deadline) {
		ok, s := cond()
		if ok {
			return
		}
		state = s
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s; state: %s", what, state)
}

func TestBuildTaskText(t *testing.T) {
	task := model.Task{ID: "task-123", Title: "修复崩溃", Description: "点卡片会崩。\n"}
	text := BuildTaskText(task)
	if want := "使用 manage-taskboard-native 执行任务 task-123。"; text != want {
		t.Fatalf("task text = %q, want %q", text, want)
	}
}

func TestTail(t *testing.T) {
	if got := tail("short", 10); got != "short" {
		t.Errorf("tail(short) = %q", got)
	}
	long := strings.Repeat("x", 30)
	got := tail(long, 10)
	if !strings.HasPrefix(got, "…") || len([]rune(got)) != 12 {
		t.Errorf("tail(long) = %q (len %d)", got, len([]rune(got)))
	}
}

func TestDispatchValidation(t *testing.T) {
	st := newTestStore(t)
	d := NewDispatcher(st)
	defer d.Close()
	_, task := projectWithWorkspace(t, st)

	if err := d.Dispatch(task.ID, "unknown-runtime", "test"); err == nil || !errors.As(err, new(*store.InvalidError)) {
		t.Errorf("unknown runtime: want InvalidError, got %v", err)
	}
	if err := d.Dispatch("no-such-task", "dsh", "test"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("missing task: want ErrNotFound, got %v", err)
	}
	archived, err := st.ArchiveTask(task.ID, task.Version, "test")
	if err != nil {
		t.Fatalf("ArchiveTask: %v", err)
	}
	if err := d.Dispatch(archived.ID, "dsh", "test"); err == nil || !errors.As(err, new(*store.InvalidError)) {
		t.Errorf("archived task: want InvalidError, got %v", err)
	}

	// A project without a workspace path, and one pointing at a file
	// (store-level creation skips path validation; the HTTP boundary owns it).
	bare, err := st.CreateProject("bare", "test")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	bareTask, err := st.CreateTask(bare.ID, bare.Columns[0].ID, "无路径任务", "", "", "test")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := d.Dispatch(bareTask.ID, "dsh", "test"); err == nil || !errors.As(err, new(*store.InvalidError)) {
		t.Errorf("no workspacePath: want InvalidError, got %v", err)
	}
	notDir := filepath.Join(t.TempDir(), "plain.txt")
	if err := os.WriteFile(notDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fileProj, err := st.CreateProjectWithPath("fileproj", notDir, "test")
	if err != nil {
		t.Fatalf("CreateProjectWithPath: %v", err)
	}
	fileTask, err := st.CreateTask(fileProj.ID, fileProj.Columns[0].ID, "文件路径任务", "", "", "test")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := d.Dispatch(fileTask.ID, "dsh", "test"); err == nil || !errors.As(err, new(*store.InvalidError)) {
		t.Errorf("file workspacePath: want InvalidError, got %v", err)
	}
}

func TestDispatchRunsFakeCLIWithoutClaimingOrWritingOutcome(t *testing.T) {
	st := newTestStore(t)
	p, task := projectWithWorkspace(t, st)
	installFakeHerdr(t, p.WorkspacePath, "done")
	d := NewDispatcher(st)
	defer d.Close()
	if err := d.Dispatch(task.ID, "codex", "tester"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Herdr session returned", func() (bool, string) {
		run, err := st.Execution(task.ID)
		return err == nil && run != nil && run.Status == "returned", fmt.Sprint(run, err)
	})
	cur, _ := st.Task(task.ID)
	if cur.ColumnID != task.ColumnID || cur.Version != task.Version {
		t.Fatalf("dispatcher changed task: %+v", cur)
	}
	if run, _ := st.Execution(task.ID); run.SessionID != "herdr:test-terminal-1" {
		t.Fatalf("execution did not retain Herdr terminal: %+v", run)
	}
	for _, c := range cur.Comments {
		if strings.Contains(c.Text, "fake-ok out") || strings.Contains(c.Text, "执行完成（") {
			t.Fatalf("dispatcher wrote agent outcome: %s", c.Text)
		}
	}
}

func TestDispatchFailureKeepsColumn(t *testing.T) {
	st := newTestStore(t)
	p, task := projectWithWorkspace(t, st)
	installFakeHerdr(t, filepath.Join(p.WorkspacePath, "missing"), "done")
	d := NewDispatcher(st)
	defer d.Close()

	if err := d.Dispatch(task.ID, "codex", "tester"); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	doingID := p.Columns[0].ID
	waitFor(t, "failure comment", func() (bool, string) {
		cs, err := st.Comments(task.ID)
		if err != nil {
			return false, "comments read: " + err.Error()
		}
		if len(cs) == 0 {
			return false, "no comments yet"
		}
		last := cs[len(cs)-1].Text
		var all strings.Builder
		for i, c := range cs {
			fmt.Fprintf(&all, "#%d [%s] %q; ", i, c.Author, c.Text)
		}
		return len(cs) >= 2 && strings.Contains(last, "Herdr 派发 codex 失败"),
			fmt.Sprintf("comments=%d all=%s", len(cs), all.String())
	})
	cur, _ := st.Task(task.ID)
	if cur.ColumnID != doingID {
		t.Fatalf("column after failure = %s, want still 进行中", cur.ColumnID)
	}
}

func TestDispatchInFlightMutex(t *testing.T) {
	st := newTestStore(t)
	p, task := projectWithWorkspace(t, st)
	installFakeHerdr(t, p.WorkspacePath, "working")
	d := NewDispatcher(st)

	if err := d.Dispatch(task.ID, "codex", "tester"); err != nil {
		t.Fatalf("first Dispatch: %v", err)
	}
	if err := d.Dispatch(task.ID, "codex", "tester"); !errors.Is(err, ErrInProgress) {
		t.Fatalf("second Dispatch: want ErrInProgress, got %v", err)
	}
	d.Close()
}

func TestCloseCancelsRunning(t *testing.T) {
	st := newTestStore(t)
	p, task := projectWithWorkspace(t, st)
	_, sendsLog := installFakeHerdr(t, p.WorkspacePath, "working")
	d := NewDispatcher(st)

	if err := d.Dispatch(task.ID, "codex", "tester"); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	waitFor(t, "Herdr session tracking", func() (bool, string) {
		run, err := st.Execution(task.ID)
		return err == nil && run != nil && run.Status == "running", fmt.Sprint(run, err)
	})
	d.Close()
	run, err := st.Execution(task.ID)
	if err != nil || (run.Status != "running" && run.Status != "disconnected") || run.SessionID != "herdr:test-terminal-1" {
		t.Fatalf("closing the service must leave the Herdr terminal recorded: %+v %v", run, err)
	}
	if _, err := os.Stat(sendsLog); !os.IsNotExist(err) {
		t.Fatalf("closing the service unexpectedly talked to or killed the terminal: err=%v", err)
	}
}
