package store

import (
	"errors"
	"path/filepath"
	"testing"

	"taskboard-native/internal/model"
)

func newEditTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "edit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestUpdateProjectEditsNameAndPath(t *testing.T) {
	s := newEditTestStore(t)
	workspace := t.TempDir()
	newWorkspace := t.TempDir()
	p, err := s.CreateProjectWithPath("old name", workspace, "test")
	if err != nil {
		t.Fatal(err)
	}
	var events []model.Event
	s.OnEvent = func(ev model.Event) { events = append(events, ev) }

	newName := "renamed"
	updated, err := s.UpdateProject(p.ID, ProjectPatch{Name: &newName, WorkspacePath: &newWorkspace}, "tester")
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if updated.Name != newName || updated.WorkspacePath != newWorkspace {
		t.Fatalf("updated = %+v", updated)
	}
	if len(updated.Columns) != 4 {
		t.Fatalf("columns lost: %+v", updated.Columns)
	}
	got, err := s.Project(p.ID)
	if err != nil || got.Name != newName || got.WorkspacePath != newWorkspace {
		t.Fatalf("reread = %+v %v", got, err)
	}
	if len(events) != 1 || events[0].Type != model.EventProjectChange {
		t.Fatalf("events = %+v", events)
	}
}

func TestUpdateProjectClearsPathAndKeepsFieldOnNil(t *testing.T) {
	s := newEditTestStore(t)
	workspace := t.TempDir()
	p, err := s.CreateProjectWithPath("has path", workspace, "test")
	if err != nil {
		t.Fatal(err)
	}

	// nil WorkspacePath leaves the path alone.
	newName := "only renamed"
	if _, err := s.UpdateProject(p.ID, ProjectPatch{Name: &newName}, "tester"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Project(p.ID)
	if got.WorkspacePath != workspace {
		t.Fatalf("nil patch changed path: %+v", got)
	}

	// Empty (non-nil) string clears it.
	empty := ""
	if _, err := s.UpdateProject(p.ID, ProjectPatch{WorkspacePath: &empty}, "tester"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Project(p.ID)
	if got.WorkspacePath != "" || got.Name != newName {
		t.Fatalf("clear: %+v", got)
	}
}

func TestUpdateProjectValidation(t *testing.T) {
	s := newEditTestStore(t)
	p, err := s.CreateProject("plain", "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateProject("missing", ProjectPatch{}, "tester"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing project: want ErrNotFound, got %v", err)
	}
	empty := "   "
	if _, err := s.UpdateProject(p.ID, ProjectPatch{Name: &empty}, "tester"); err == nil {
		t.Fatal("empty name: want error, got nil")
	}
	// Patches with no fields still succeed (touch nothing but updated_at).
	if _, err := s.UpdateProject(p.ID, ProjectPatch{}, "tester"); err != nil {
		t.Fatalf("empty patch: %v", err)
	}
}

// TestProjectColorPersists covers the accent color end to end: set on
// create, kept on reread, changed by patch, cleared back to empty, and
// surviving the projects list (the data the global view colors from).
func TestProjectColorPersists(t *testing.T) {
	s := newEditTestStore(t)
	p, err := s.CreateProjectWithColor("colored", t.TempDir(), "#2563eb", "test")
	if err != nil {
		t.Fatal(err)
	}
	if p.Color != "#2563eb" {
		t.Fatalf("created color = %q", p.Color)
	}
	got, err := s.Project(p.ID)
	if err != nil || got.Color != "#2563eb" {
		t.Fatalf("reread = %+v %v", got, err)
	}
	// A patch changes the color; the projects list reads it back.
	newColor := "#10b981"
	if _, err := s.UpdateProject(p.ID, ProjectPatch{Color: &newColor}, "tester"); err != nil {
		t.Fatal(err)
	}
	ps, err := s.Projects()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range ps {
		if item.ID == p.ID {
			found = true
			if item.Color != "#10b981" {
				t.Fatalf("list color = %q", item.Color)
			}
		}
	}
	if !found {
		t.Fatal("project missing from list")
	}
	// Clearing (empty string) is a legitimate patch — back to automatic slot.
	clear := ""
	if _, err := s.UpdateProject(p.ID, ProjectPatch{Color: &clear}, "tester"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Project(p.ID)
	if got.Color != "" {
		t.Fatalf("cleared color = %q", got.Color)
	}
}

// TestCreateProjectColorDefaultEmpty makes sure plain creates carry no color
// so the window's stable palette slot stays the default.
func TestCreateProjectColorDefaultEmpty(t *testing.T) {
	s := newEditTestStore(t)
	p, err := s.CreateProject("plain", "test")
	if err != nil {
		t.Fatal(err)
	}
	if p.Color != "" {
		t.Fatalf("plain create color = %q", p.Color)
	}
}
