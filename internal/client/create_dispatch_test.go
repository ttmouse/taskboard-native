package client_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"taskboard-native/internal/client"
	"taskboard-native/internal/executor"
	"taskboard-native/internal/service"
)

func installFakeHerdr(t *testing.T, folder, state string) {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate fake Herdr fixture")
	}
	binDir := filepath.Join(filepath.Dir(source), "..", "..", "testdata", "fake-herdr")
	logs := t.TempDir()
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TASKBOARD_TEST_HERDR_FOLDER", folder)
	t.Setenv("TASKBOARD_TEST_HERDR_STATE", state)
	t.Setenv("TASKBOARD_TEST_HERDR_ARGS", filepath.Join(logs, "herdr-args"))
	t.Setenv("TASKBOARD_TEST_HC_SENDS", filepath.Join(logs, "hc-sends"))
	t.Setenv("TASKBOARD_TEST_HERDR_COUNTER", filepath.Join(logs, "counter"))
}

// The two create modes travel in one field: an empty dispatchRuntime is
// "仅建卡", a named one is "建卡并派发". The client must put the channel
// on the wire, and the service must act on it.
func TestCreateTaskAndDispatchOneCall(t *testing.T) {
	// A fake channel binary stands in for a real CLI: it prints a line
	// and exits, which is what the dispatcher records as a finished run.
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "codex"), []byte("#!/bin/sh\nprintf 'CLIENT_DISPATCH_OK\\n'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	srv, err := service.Start("127.0.0.1:0", filepath.Join(t.TempDir(), "board.db"))
	if err != nil {
		t.Fatalf("start service: %v", err)
	}
	t.Cleanup(func() { srv.Close() })

	c := client.New(&client.Discover{Addr: srv.Addr}, "client-dispatch-test")
	// Dispatching needs a working directory, so the project carries one.
	p, err := c.CreateProjectWithPath("一步派发", t.TempDir())
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if len(p.Columns) == 0 {
		t.Fatal("project has no columns")
	}
	installFakeHerdr(t, p.WorkspacePath, "done")
	col := p.Columns[0].ID

	// 建卡并派发: one call, and the run lands on the card.
	dispatched, err := c.CreateTaskAndDispatch(p.ID, col, "一步建卡并派发", "", "", executor.RuntimeCodex)
	if err != nil {
		t.Fatalf("create and dispatch: %v", err)
	}
	if dispatched.Title != "一步建卡并派发" {
		t.Fatalf("title = %q", dispatched.Title)
	}
	deadline := time.Now().Add(5 * time.Second)
	var current = dispatched
	for time.Now().Before(deadline) {
		current, err = c.Task(dispatched.ID)
		if err == nil && current.Execution != nil && current.Execution.Status == "returned" && strings.HasPrefix(current.Execution.SessionID, "herdr:") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if current.Execution == nil || current.Execution.Status != "returned" || !strings.HasPrefix(current.Execution.SessionID, "herdr:") {
		t.Fatalf("Herdr run did not return on the card: %+v", current.Execution)
	}

	// 仅建卡: no channel, no run, no comment from a channel.
	plain, err := c.CreateTaskAndDispatch(p.ID, col, "仅建卡", "", "", "")
	if err != nil {
		t.Fatalf("create only: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	after, err := c.Task(plain.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Comments) != 0 {
		t.Fatalf("create-only card gained comments: %+v", after.Comments)
	}
	if after.Execution != nil {
		t.Fatalf("create-only card has an execution: %+v", after.Execution)
	}
}

// An unknown channel is refused before the card exists, so a rejected
// call leaves nothing behind to clean up.
func TestCreateTaskAndDispatchRejectsBadChannel(t *testing.T) {
	srv, err := service.Start("127.0.0.1:0", filepath.Join(t.TempDir(), "board.db"))
	if err != nil {
		t.Fatalf("start service: %v", err)
	}
	t.Cleanup(func() { srv.Close() })

	c := client.New(&client.Discover{Addr: srv.Addr}, "client-dispatch-test")
	p, err := c.CreateProjectWithPath("一步派发", t.TempDir())
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	col := p.Columns[0].ID

	before := len(projectTasks(t, c, p.ID))
	if _, err := c.CreateTaskAndDispatch(p.ID, col, "坏渠道", "", "", "unregistered"); err == nil {
		t.Fatal("unknown channel was accepted")
	} else if !strings.Contains(err.Error(), "unknown runtime") {
		t.Fatalf("error = %v, want unknown runtime", err)
	}
	if after := len(projectTasks(t, c, p.ID)); after != before {
		t.Fatalf("rejected create left a card behind: %d -> %d", before, after)
	}
}

func projectTasks(t *testing.T, c *client.Client, projectID string) []string {
	t.Helper()
	board, err := c.Board(projectID)
	if err != nil {
		t.Fatalf("board: %v", err)
	}
	ids := make([]string, 0, len(board.Tasks))
	for _, task := range board.Tasks {
		ids = append(ids, task.ID)
	}
	return ids
}
