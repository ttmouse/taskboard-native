package store

import (
	"testing"
	"time"

	"taskboard-native/internal/model"
)

// TestProjectActiveAt: Projects() reports ActiveAt as the newest live-task
// updated_at (falling back to created_at), so the sidebar can order by recent
// activity.
func TestProjectActiveAt(t *testing.T) {
	s := openTemp(t)

	projects, err := s.Projects()
	if err != nil {
		t.Fatalf("projects: %v", err)
	}
	local := projects[0]
	empty, err := s.CreateProject("empty", "tester")
	if err != nil {
		t.Fatalf("create empty project: %v", err)
	}
	projects, err = s.Projects()
	if err != nil {
		t.Fatalf("projects with empty project: %v", err)
	}
	var emptyFromList *model.Project
	for i := range projects {
		if projects[i].ID == empty.ID {
			emptyFromList = &projects[i]
			break
		}
	}
	if emptyFromList == nil {
		t.Fatal("empty project missing from project list")
	}
	// openTemp seeds three cards into local, so an independent project is
	// required to exercise the no-task fallback.
	if !emptyFromList.ActiveAt.Equal(emptyFromList.CreatedAt) {
		t.Fatalf("ActiveAt = %v, want CreatedAt %v for empty project", emptyFromList.ActiveAt, emptyFromList.CreatedAt)
	}
	if local.UpdatedAt.Before(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("UpdatedAt looks unset: %v", local.UpdatedAt)
	}

	// Create a task → ActiveAt tracks its updated_at.
	task, err := s.CreateTask(local.ID, local.Columns[0].ID, "t", "", "", "tester")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	projects, err = s.Projects()
	if err != nil {
		t.Fatalf("projects: %v", err)
	}
	got := projects[0].ActiveAt
	if !got.Equal(task.UpdatedAt) {
		t.Fatalf("ActiveAt = %v, want task.UpdatedAt %v", got, task.UpdatedAt)
	}
}
