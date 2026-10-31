package service

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"taskboard-native/internal/executor"
)

func TestHandoffLogEndpointReturnsTaskScopedTimeline(t *testing.T) {
	srv := start(t)
	task := firstTask(t, srv)
	started := time.Date(2026, 10, 31, 1, 0, 0, 0, time.UTC)
	other := map[string]any{"timestamp": started, "event": "task.dispatch_started", "taskId": "another-task", "runtime": "secret-runtime", "sessionId": "secret-session"}
	lines := []map[string]any{
		{"timestamp": started, "event": "task.dispatch_started", "taskId": task.ID, "runtime": "codex", "actor": "secret-actor", "sessionId": "secret-session"},
		{"timestamp": started.Add(1500 * time.Millisecond), "event": "task.dispatch_accepted", "taskId": task.ID, "runtime": "codex", "executionStatus": "running", "sessionId": "secret-session"},
		{"timestamp": started.Add(3 * time.Second), "event": "task.execution_converged", "taskId": task.ID, "runtime": "codex", "executionStatus": "returned", "reason": "execution_ended_human_acceptance_required", "sessionId": "secret-session"},
		other,
	}
	path := srv.DBPath + ".auto-handoff.jsonl"
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range lines {
		if err := json.NewEncoder(f).Encode(line); err != nil {
			f.Close()
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	status, raw := request(t, srv, http.MethodGet, "/api/tasks/"+task.ID+"/handoff-log", "", nil)
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%s", status, raw)
	}
	var view executor.HandoffLogView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Events) != 3 || view.Events[0].Event != "task.dispatch_started" || view.Events[1].Runtime != "codex" || view.Summary.DurationMs != 3000 || view.Summary.Outcome == "" {
		t.Fatalf("unexpected timeline: %+v", view)
	}
	for _, forbidden := range []string{"another-task", "secret-runtime"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, raw)
		}
	}
	if _, err := os.Stat(filepath.Clean(path)); err != nil {
		t.Fatalf("log file unexpectedly unavailable: %v", err)
	}
}
