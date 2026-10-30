package store

import (
	"errors"
	"testing"

	"taskboard-native/internal/model"
)

func TestDeleteProjectRemovesProjectAndCards(t *testing.T) {
	s := newEditTestStore(t)
	workspace := t.TempDir()
	p, err := s.CreateProjectWithPath("gone", workspace, "test")
	if err != nil {
		t.Fatal(err)
	}
	active, err := s.CreateTask(p.ID, p.Columns[0].ID, "active card", "", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddComment(active.ID, "progress note", "test"); err != nil {
		t.Fatal(err)
	}
	archived, err := s.CreateTask(p.ID, p.Columns[1].ID, "archived card", "", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ArchiveTask(archived.ID, archived.Version, "test"); err != nil {
		t.Fatal(err)
	}

	var events []model.Event
	s.OnEvent = func(ev model.Event) { events = append(events, ev) }

	if err := s.DeleteProject(p.ID, "Cindy"); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}

	projects, err := s.Projects()
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range projects {
		if got.ID == p.ID {
			t.Fatal("project still listed after delete")
		}
	}
	if _, err := s.Project(p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Project after delete: want ErrNotFound, got %v", err)
	}
	if _, err := s.Task(active.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("active task after delete: want ErrNotFound, got %v", err)
	}
	if _, err := s.Task(archived.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("archived task after delete: want ErrNotFound, got %v", err)
	}
	remaining, err := s.TasksByProject(p.ID, true)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("TasksByProject after delete: want ErrNotFound, got %v (n=%d)", err, len(remaining))
	}
	found := false
	for _, ev := range events {
		if ev.Type == model.EventProjectChange && ev.ProjectID == p.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("no project.changed event emitted")
	}
}

func TestDeleteProjectUnknownID(t *testing.T) {
	s := newEditTestStore(t)
	if err := s.DeleteProject("no-such-project", "test"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteProject unknown id: want ErrNotFound, got %v", err)
	}
}

func TestDeleteProjectRejectsActiveExecution(t *testing.T) {
	s := newEditTestStore(t)
	p, err := s.CreateProjectWithPath("busy", t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.CreateTask(p.ID, p.Columns[0].ID, "running", "", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveExecution(model.Execution{
		TaskID: task.ID, SessionID: "herdr:t1", Runtime: "codex", Status: "running",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProject(p.ID, "test"); !errors.Is(err, ErrActiveExecution) {
		t.Fatalf("DeleteProject with running execution: want ErrActiveExecution, got %v", err)
	}
	if _, err := s.Project(p.ID); err != nil {
		t.Fatalf("project removed despite active execution: %v", err)
	}
}

func TestDeleteProjectKeepsOtherProjects(t *testing.T) {
	s := newEditTestStore(t)
	workspace := t.TempDir()
	keep, err := s.CreateProjectWithPath("keep", workspace, "test")
	if err != nil {
		t.Fatal(err)
	}
	gone, err := s.CreateProjectWithPath("gone", workspace, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask(gone.ID, gone.Columns[0].ID, "doomed", "", "", "test"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProject(gone.ID, "test"); err != nil {
		t.Fatal(err)
	}
	tasks, err := s.TasksByProject(keep.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("keeper project touched: %d tasks", len(tasks))
	}
	projects, err := s.Projects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].ID != keep.ID {
		t.Fatalf("want only keeper project, got %d", len(projects))
	}
}
