package service

import (
	"net/http"
	"strings"
	"testing"

	"taskboard-native/internal/model"
)

// Releasing is the manual exit for a session that is gone for good: without it
// the card stays locked forever and neither dispatch nor follow-up can proceed.
func TestReleaseExecutionConvergesToDetached(t *testing.T) {
	srv := start(t)
	task := firstTask(t, srv)
	if err := srv.store.SaveExecution(model.Execution{
		TaskID:    task.ID,
		SessionID: "herdr:gone-terminal",
		Status:    "disconnected",
		Runtime:   "pi",
	}); err != nil {
		t.Fatalf("seed execution: %v", err)
	}
	status, raw := request(t, srv, http.MethodPost, "/api/tasks/"+task.ID+"/execution/release", "豆皮", nil)
	if status != http.StatusOK {
		t.Fatalf("release: %d %s", status, raw)
	}
	released := decode[model.Task](t, raw)
	if released.Execution == nil || released.Execution.Status != "released" {
		t.Fatalf("execution after release = %+v", released.Execution)
	}
	if released.Execution.Active() {
		t.Fatal("released execution still holds the write lock")
	}
	if !strings.Contains(released.Execution.Detail, "豆皮") {
		t.Fatalf("release detail lost the acting agent: %q", released.Execution.Detail)
	}
	// The lock is gone: dispatch accepts the card again instead of 409.
	status, raw = request(t, srv, http.MethodPost, "/api/tasks/"+task.ID+"/dispatch", "豆皮", map[string]any{"runtime": "pi"})
	if status == http.StatusConflict {
		t.Fatalf("dispatch after release still blocked: %d %s", status, raw)
	}
}

// A session that may still be writing must be stopped in its own channel first;
// releasing it here would allow a second writer on the same card.
func TestReleaseExecutionRefusesLiveRun(t *testing.T) {
	srv := start(t)
	task := firstTask(t, srv)
	if err := srv.store.SaveExecution(model.Execution{
		TaskID:    task.ID,
		SessionID: "herdr:live-terminal",
		Status:    "running",
		Runtime:   "pi",
	}); err != nil {
		t.Fatalf("seed execution: %v", err)
	}
	status, raw := request(t, srv, http.MethodPost, "/api/tasks/"+task.ID+"/execution/release", "豆皮", nil)
	if status != http.StatusConflict {
		t.Fatalf("release of a running session = %d %s", status, raw)
	}
	run, err := srv.store.Execution(task.ID)
	if err != nil || run == nil || run.Status != "running" {
		t.Fatalf("refused release changed the record: %+v, err=%v", run, err)
	}
}

// No record means nothing to release.
func TestReleaseExecutionWithoutRecord(t *testing.T) {
	srv := start(t)
	task := firstTask(t, srv)
	status, raw := request(t, srv, http.MethodPost, "/api/tasks/"+task.ID+"/execution/release", "豆皮", nil)
	if status != http.StatusNotFound {
		t.Fatalf("release without execution = %d %s", status, raw)
	}
}

func firstTask(t *testing.T, srv *Server) model.Task {
	t.Helper()
	status, raw := request(t, srv, http.MethodGet, "/api/projects", "", nil)
	if status != http.StatusOK {
		t.Fatalf("list projects: %d %s", status, raw)
	}
	projects := decode[[]model.Project](t, raw)
	if len(projects) == 0 {
		t.Fatal("fixture board has no projects")
	}
	status, raw = request(t, srv, http.MethodGet, "/api/tasks?projectId="+projects[0].ID, "", nil)
	if status != http.StatusOK {
		t.Fatalf("list tasks: %d %s", status, raw)
	}
	tasks := decode[[]model.Task](t, raw)
	if len(tasks) == 0 {
		t.Fatal("fixture board has no tasks")
	}
	return tasks[0]
}
