package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/plugins/sqlite"
)

func TestFormatTaskNumber(t *testing.T) {
	if got := FormatTaskNumber(1083); got != "1083" {
		t.Fatalf("FormatTaskNumber(1083) = %q", got)
	}
	if FormatTaskNumber(0) != "" || FormatTaskNumber(-1) != "" {
		t.Fatal("non-positive numbers should format empty")
	}
}

func TestCreateTaskAllocatesSequentialNumbers(t *testing.T) {
	s := openTemp(t)
	board, err := s.Board("local")
	if err != nil {
		t.Fatal(err)
	}
	// openTemp already created three fixture tasks at 1001..1003.
	a, err := s.CreateTask("local", board.Columns[0].ID, "next-a", "", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateTask("local", board.Columns[0].ID, "next-b", "", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	if a.Number != 1004 || b.Number != 1005 {
		t.Fatalf("got numbers %d, %d; want 1004, 1005", a.Number, b.Number)
	}
	if FormatTaskNumber(a.Number) != "1004" {
		t.Fatalf("format = %q", FormatTaskNumber(a.Number))
	}
}

func TestTaskResolvesByNumber(t *testing.T) {
	s := openTemp(t)
	board, err := s.Board("local")
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.CreateTask("local", board.Columns[0].ID, "by-number", "", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	// Keep the legacy hex-prefix compatibility deterministic: a numeric-only
	// prefix must fall through to prefix lookup when it is not a task number.
	oldID := task.ID
	task.ID = "1234567890abcdef1234567890abcdef"
	s.mu.Lock()
	_, err = s.db.Execute(context.Background(), `UPDATE tasks SET id = ? WHERE id = ?`, task.ID, oldID)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Task(FormatTaskNumber(task.Number))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != task.ID || got.Number != task.Number {
		t.Fatalf("resolved number → %+v", got)
	}
	if _, err := s.AddComment(FormatTaskNumber(task.Number), "via number", "test"); err != nil {
		t.Fatalf("comment via number: %v", err)
	}
	got, err = s.Task(task.ID)
	if err != nil || got.ID != task.ID {
		t.Fatalf("full id resolve: %+v %v", got, err)
	}
	prefix := task.ID[:HexPrefixMinLen]
	got, err = s.Task(prefix)
	if err != nil || got.ID != task.ID {
		t.Fatalf("hex prefix resolve: %+v %v", got, err)
	}
}

func TestMigrateAssignsTaskNumbers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.db")
	ctx := context.Background()
	db, err := sqlite.Open(ctx, path, sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	legacy := `
CREATE TABLE projects (
  id TEXT PRIMARY KEY, name TEXT NOT NULL, position INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE "columns" (
  id TEXT PRIMARY KEY, project_id TEXT NOT NULL, name TEXT NOT NULL,
  position INTEGER NOT NULL DEFAULT 0, role TEXT NOT NULL DEFAULT ''
);
CREATE TABLE tasks (
  id TEXT PRIMARY KEY, project_id TEXT NOT NULL, column_id TEXT NOT NULL,
  title TEXT NOT NULL, body TEXT NOT NULL DEFAULT '', assignee TEXT NOT NULL DEFAULT '',
  position INTEGER NOT NULL DEFAULT 0, version INTEGER NOT NULL DEFAULT 1,
  archived INTEGER NOT NULL DEFAULT 0, last_agent TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE comments (
  id TEXT PRIMARY KEY, task_id TEXT NOT NULL, author TEXT NOT NULL, body TEXT NOT NULL,
  mention TEXT NOT NULL DEFAULT '', parent_comment_id TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
);
CREATE TABLE executions (
  task_id TEXT PRIMARY KEY, session_id TEXT NOT NULL, status TEXT NOT NULL, detail TEXT NOT NULL DEFAULT ''
);
CREATE TABLE followups (
  comment_id TEXT PRIMARY KEY, task_id TEXT NOT NULL, status TEXT NOT NULL
);
CREATE TABLE project_runs (
  project_id TEXT PRIMARY KEY, runtime TEXT NOT NULL, status TEXT NOT NULL, task_ids TEXT NOT NULL,
  cursor INTEGER NOT NULL DEFAULT 0, current_task_id TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL
);
`
	for _, stmt := range strings.Split(legacy, ";") {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := db.Execute(ctx, stmt); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	now := timestamp(nowUTC())
	stmts := []sqlite.Statement{
		{SQL: `INSERT INTO projects(id,name,position,created_at,updated_at) VALUES('p','P',0,?,?)`, Args: []any{now, now}},
		{SQL: `INSERT INTO "columns"(id,project_id,name,position,role) VALUES('c','p','待办',1000,'intake')`},
		{SQL: `INSERT INTO tasks(id,project_id,column_id,title,body,assignee,position,version,archived,last_agent,created_at,updated_at)
		       VALUES('ta','p','c','A','', '',1000,1,0,'t',?,?)`, Args: []any{now, now}},
		{SQL: `INSERT INTO tasks(id,project_id,column_id,title,body,assignee,position,version,archived,last_agent,created_at,updated_at)
		       VALUES('tb','p','c','B','', '',2000,1,0,'t',?,?)`, Args: []any{now, now}},
	}
	if _, err := db.Transaction(ctx, stmts); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	a, err := s.Task("ta")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Task("tb")
	if err != nil {
		t.Fatal(err)
	}
	if a.Number != TaskNumberStart || b.Number != TaskNumberStart+1 {
		t.Fatalf("migrated numbers = %d, %d; want %d, %d", a.Number, b.Number, TaskNumberStart, TaskNumberStart+1)
	}
	got, err := s.Task(FormatTaskNumber(a.Number))
	if err != nil || got.ID != "ta" {
		t.Fatalf("resolve migrated number: %+v %v", got, err)
	}
	if a.ID != "ta" || b.ID != "tb" {
		t.Fatalf("uuid changed: %q %q", a.ID, b.ID)
	}
}
