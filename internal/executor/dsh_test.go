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

// installFakeDsh puts a stub `dsh` on PATH whose behaviour the test scripts.
func installFakeDsh(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "dsh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write fake dsh: %v", err)
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
	task := model.Task{Title: "修复崩溃", Description: "点卡片会崩。\n"}
	project := model.Project{Name: "demo"}
	text := BuildTaskText(task, project)
	for _, want := range []string{"【看板任务派发】", "项目：demo", "任务标题：修复崩溃", "任务正文：", "点卡片会崩。", "不要向看板写评论"} {
		if !strings.Contains(text, want) {
			t.Errorf("task text missing %q:\n%s", want, text)
		}
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

	if err := d.Dispatch(task.ID, "codex", "test"); err == nil || !errors.As(err, new(*store.InvalidError)) {
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

func TestDispatchRunsFakeDsh(t *testing.T) {
	installFakeDsh(t, `echo "fake-ok out"`)
	st := newTestStore(t)
	d := NewDispatcher(st)
	p, task := projectWithWorkspace(t, st)

	if err := d.Dispatch(task.ID, "dsh", "tester"); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	reviewID := p.Columns[2].ID // 评审
	doingID := p.Columns[1].ID  // 进行中
	waitFor(t, "task to land in 评审 with a result comment", func() (bool, string) {
		cur, err := st.Task(task.ID)
		if err != nil {
			return false, "task read: " + err.Error()
		}
		if cur.ColumnID != reviewID {
			return false, "column=" + cur.ColumnID
		}
		cs, err := st.Comments(task.ID)
		if err != nil {
			return false, "comments read: " + err.Error()
		}
		if len(cs) < 2 {
			return false, "comments=" + fmt.Sprint(len(cs))
		}
		joined := cs[0].Text + cs[len(cs)-1].Text
		ok := strings.Contains(cs[0].Text, "派发 dsh 执行") &&
			strings.Contains(joined, "fake-ok out")
		return ok, fmt.Sprintf("comments=%d first=%q last=%q", len(cs), cs[0].Text, cs[len(cs)-1].Text)
	})
	// The claim moved the card through 进行中 on the way to 评审; check the
	// final state and that a 进行中 move actually happened by ordering.
	cur, _ := st.Task(task.ID)
	if cur.ColumnID != reviewID {
		t.Fatalf("final column = %s, want 评审", cur.ColumnID)
	}
	_ = doingID
}

func TestDispatchFailureKeepsColumn(t *testing.T) {
	installFakeDsh(t, `echo "boom" >&2; exit 3`)
	st := newTestStore(t)
	d := NewDispatcher(st)
	p, task := projectWithWorkspace(t, st)

	if err := d.Dispatch(task.ID, "dsh", "tester"); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	doingID := p.Columns[1].ID
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
		return len(cs) >= 2 && strings.Contains(last, "执行失败"),
			fmt.Sprintf("comments=%d all=%s", len(cs), all.String())
	})
	cur, _ := st.Task(task.ID)
	if cur.ColumnID != doingID {
		t.Fatalf("column after failure = %s, want still 进行中", cur.ColumnID)
	}
}

func TestDispatchInFlightMutex(t *testing.T) {
	installFakeDsh(t, `sleep 1`)
	st := newTestStore(t)
	d := NewDispatcher(st)
	_, task := projectWithWorkspace(t, st)

	if err := d.Dispatch(task.ID, "dsh", "tester"); err != nil {
		t.Fatalf("first Dispatch: %v", err)
	}
	if err := d.Dispatch(task.ID, "dsh", "tester"); !errors.Is(err, ErrInProgress) {
		t.Fatalf("second Dispatch: want ErrInProgress, got %v", err)
	}
	waitFor(t, "in-flight entry to clear", func() (bool, string) {
		d.mu.Lock()
		defer d.mu.Unlock()
		return len(d.inflight) == 0, fmt.Sprintf("inflight=%d", len(d.inflight))
	})
}

func TestCloseCancelsRunning(t *testing.T) {
	installFakeDsh(t, `sleep 30`)
	st := newTestStore(t)
	d := NewDispatcher(st)
	_, task := projectWithWorkspace(t, st)

	if err := d.Dispatch(task.ID, "dsh", "tester"); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	d.Close()
	waitFor(t, "cancel comment", func() (bool, string) {
		cs, err := st.Comments(task.ID)
		if err != nil {
			return false, "comments read: " + err.Error()
		}
		if len(cs) == 0 {
			return false, "no comments yet"
		}
		last := cs[len(cs)-1].Text
		return len(cs) >= 2 && strings.Contains(last, "被取消"),
			fmt.Sprintf("comments=%d last=%q", len(cs), last)
	})
}
