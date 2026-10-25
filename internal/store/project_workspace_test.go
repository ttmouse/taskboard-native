package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/plugins/sqlite"
)

func TestProjectWorkspaceSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.db")
	workspace := t.TempDir()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateProjectWithPath("workspace project", workspace, "test")
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	if project.WorkspacePath != workspace || len(project.Columns) != 4 {
		s.Close()
		t.Fatalf("bad creation: %+v", project)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Project(project.ID)
	if err != nil || got.WorkspacePath != workspace {
		t.Fatalf("reopen: %+v %v", got, err)
	}
	projects, err := s.Projects()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range projects {
		if p.ID == project.ID {
			found = p.WorkspacePath == workspace
		}
	}
	if !found {
		t.Fatal("project listing lost workspace")
	}
	board, err := s.Board(project.ID)
	if err != nil || board.Project.WorkspacePath != workspace {
		t.Fatalf("board lost workspace: %+v %v", board, err)
	}
	legacy, err := s.CreateProject("name only", "test")
	if err != nil || legacy.WorkspacePath != "" {
		t.Fatalf("legacy creation broke: %+v %v", legacy, err)
	}
}

func TestWorkspaceMigrationPreservesLegacyProjectAndCard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	ctx := context.Background()
	db, err := sqlite.Open(ctx, path, sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	legacySchema := strings.Replace(schema, "  workspace_path TEXT NOT NULL DEFAULT '',\n", "", 1)
	for _, stmt := range strings.Split(legacySchema, ";") {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err = db.Execute(ctx, stmt); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	now := timestamp(nowUTC())
	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO projects(id,name,position,created_at,updated_at) VALUES(?,?,?,?,?)`, []any{"legacy", "Existing project", 1000, now, now}},
		{`INSERT INTO "columns"(id,project_id,name,position) VALUES(?,?,?,?)`, []any{"legacy-col", "legacy", "待办", 1000}},
		{`INSERT INTO tasks(id,project_id,column_id,title,body,assignee,position,version,archived,last_agent,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, []any{"legacy-task", "legacy", "legacy-col", "Keep this card", "Existing body", "", 1000, 7, 0, "test", now, now}},
	}
	for _, stmt := range statements {
		if _, err = db.Execute(ctx, stmt.sql, stmt.args...); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		s, e := Open(path)
		if e != nil {
			t.Fatal(e)
		}
		p, e := s.Project("legacy")
		if e != nil || p.WorkspacePath != "" || p.Name != "Existing project" {
			s.Close()
			t.Fatalf("legacy project changed: %+v %v", p, e)
		}
		card, e := s.Task("legacy-task")
		if e != nil || card.Title != "Keep this card" || card.Description != "Existing body" || card.Version != 7 {
			s.Close()
			t.Fatalf("legacy task changed: %+v %v", card, e)
		}
		if err = s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
