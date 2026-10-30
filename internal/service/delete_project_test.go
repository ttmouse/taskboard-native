package service

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"taskboard-native/internal/model"
)

func TestDeleteProjectRemovesProjectAndCards(t *testing.T) {
	srv := start(t)

	// The start() fixture creates the「local」project with three cards; find
	// its id through the list endpoint.
	status, raw := request(t, srv, http.MethodGet, "/api/projects", "test", nil)
	if status != http.StatusOK {
		t.Fatalf("list: %d", status)
	}
	var projects []map[string]any
	if err := json.Unmarshal(raw, &projects); err != nil {
		t.Fatal(err)
	}
	var projectID string
	for _, p := range projects {
		if p["name"] == "local" {
			projectID, _ = p["id"].(string)
		}
	}
	if projectID == "" {
		t.Fatal("fixture project not found")
	}

	status, raw = request(t, srv, http.MethodDelete, "/api/projects/"+projectID, "test", nil)
	if status != http.StatusOK {
		t.Fatalf("delete: %d %s", status, raw)
	}
	var ok map[string]bool
	if err := json.Unmarshal(raw, &ok); err != nil || !ok["ok"] {
		t.Fatalf("delete body: %s", raw)
	}

	status, raw = request(t, srv, http.MethodGet, "/api/projects", "test", nil)
	if status != http.StatusOK {
		t.Fatalf("list after delete: %d", status)
	}
	if err := json.Unmarshal(raw, &projects); err != nil {
		t.Fatal(err)
	}
	for _, p := range projects {
		if p["id"] == projectID {
			t.Fatal("deleted project still listed")
		}
	}

	// Deleting again reports the miss as an error status, not 2xx.
	status, _ = request(t, srv, http.MethodDelete, "/api/projects/"+projectID, "test", nil)
	if status == http.StatusOK {
		t.Fatal("second delete unexpectedly succeeded")
	}
}

func TestDeleteProjectRejectsActiveExecution(t *testing.T) {
	srv := start(t)
	projects, err := srv.store.Projects()
	if err != nil || len(projects) == 0 {
		t.Fatalf("projects: %v (%d)", err, len(projects))
	}
	tasks, err := srv.store.TasksByProject(projects[0].ID, false)
	if err != nil || len(tasks) == 0 {
		t.Fatalf("tasks: %v (%d)", err, len(tasks))
	}
	if err := srv.store.SaveExecution(model.Execution{
		TaskID: tasks[0].ID, SessionID: "herdr:t-running", Runtime: "codex", Status: "running",
	}); err != nil {
		t.Fatal(err)
	}

	status, raw := request(t, srv, http.MethodDelete, "/api/projects/"+projects[0].ID, "test", nil)
	if status != http.StatusConflict || !strings.Contains(string(raw), `"error":"project_busy"`) {
		t.Fatalf("delete with running execution = %d %s, want 409 project_busy", status, raw)
	}
	if _, err := srv.store.Project(projects[0].ID); err != nil {
		t.Fatalf("project removed despite active execution: %v", err)
	}
}
