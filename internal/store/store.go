// Package store is the app-owned authoritative data layer, backed by SQLite
// (github.com/egoist/mygo/plugins/sqlite). All mutations are serialized
// through one mutex, bump the task version and emit exactly one event.
package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"

	"taskboard-native/internal/model"
)

// ErrNotFound reports a missing project, column, task or comment.
var ErrNotFound = errors.New("not found")
var ErrForbidden = errors.New("comment author mismatch")
var ErrActiveExecution = errors.New("project has an active execution")

// VersionError reports an optimistic-concurrency mismatch. Current is the
// task as stored, so callers can return it to the client.
type VersionError struct{ Current model.Task }

func (e *VersionError) Error() string {
	return fmt.Sprintf("version conflict: task is at v%d", e.Current.Version)
}

// InvalidError reports a caller mistake (missing title, unknown column).
type InvalidError struct{ Msg string }

func (e *InvalidError) Error() string { return e.Msg }

func invalid(format string, args ...any) error {
	return &InvalidError{Msg: fmt.Sprintf(format, args...)}
}

const schema = `
CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  workspace_path TEXT NOT NULL DEFAULT '',
  color TEXT NOT NULL DEFAULT '',
  position INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS "columns" (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  name TEXT NOT NULL,
  position INTEGER NOT NULL DEFAULT 0,
  role TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS columns_project ON "columns"(project_id, position);
CREATE TABLE IF NOT EXISTS tasks (
  id TEXT PRIMARY KEY,
  number INTEGER NOT NULL DEFAULT 0,
  project_id TEXT NOT NULL,
  column_id TEXT NOT NULL,
  title TEXT NOT NULL,
  body TEXT NOT NULL DEFAULT '',
  assignee TEXT NOT NULL DEFAULT '',
  position INTEGER NOT NULL DEFAULT 0,
  version INTEGER NOT NULL DEFAULT 1,
  archived INTEGER NOT NULL DEFAULT 0,
  last_agent TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS tasks_project ON tasks(project_id, archived);
CREATE INDEX IF NOT EXISTS tasks_column ON tasks(column_id, position);
CREATE TABLE IF NOT EXISTS comments (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  author TEXT NOT NULL,
  body TEXT NOT NULL,
  mention TEXT NOT NULL DEFAULT '',
  parent_comment_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS comments_task ON comments(task_id, created_at);
CREATE TABLE IF NOT EXISTS executions (
  task_id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  status TEXT NOT NULL,
  detail TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS followups (
  comment_id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  status TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS project_runs (
  project_id TEXT PRIMARY KEY,
  runtime TEXT NOT NULL,
  status TEXT NOT NULL,
  task_ids TEXT NOT NULL,
  cursor INTEGER NOT NULL DEFAULT 0,
  current_task_id TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
);
`

// DefaultColumns returns the seeded board layout, each column carrying the
// workflow role users and tools know it by.
func DefaultColumns() []model.Column {
	return []model.Column{
		{Name: "待办", Role: model.ColumnRoleIntake},
		{Name: "进行中", Role: model.ColumnRoleDoing},
		{Name: "等你", Role: model.ColumnRoleReview},
		{Name: "完成", Role: model.ColumnRoleDone},
	}
}

// Store is the single writer of the SQLite database.
type Store struct {
	mu  sync.Mutex
	db  *sqlite.DB
	seq int64

	// OnEvent receives every committed mutation. Set it before use; it is
	// called synchronously with the store lock held, so it must not block.
	OnEvent func(model.Event)
}

// Open opens (creating when absent) the database at path.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("store: empty database path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	ctx := context.Background()
	db, err := sqlite.Open(ctx, path, sqlite.OpenOptions{})
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database connection.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	for _, stmt := range strings.Split(schema, ";") {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := s.db.Execute(ctx, stmt); err != nil {
			return fmt.Errorf("store: migrate: %w", err)
		}
	}
	// CREATE TABLE does not add fields to an existing database. Preserve
	// existing projects and cards while upgrading older installations.
	executionColumns, err := s.db.Query(ctx, `PRAGMA table_info(executions)`)
	if err != nil {
		return err
	}
	for _, name := range []string{"runtime", "followup_id", "previous_turn_id"} {
		found := false
		for _, column := range executionColumns.Values {
			if str(column[1]) == name {
				found = true
			}
		}
		if !found {
			if _, err := s.db.Execute(ctx, `ALTER TABLE executions ADD COLUMN `+name+` TEXT NOT NULL DEFAULT ''`); err != nil {
				return err
			}
		}
	}
	// Comments gained an address field: the channel an author @mentioned.
	// Older databases keep every comment, they simply have no address.
	commentColumns, err := s.db.Query(ctx, `PRAGMA table_info(comments)`)
	if err != nil {
		return fmt.Errorf("store: comment schema: %w", err)
	}
	hasMention, hasParentCommentID := false, false
	for _, column := range commentColumns.Values {
		switch str(column[1]) {
		case "mention":
			hasMention = true
		case "parent_comment_id":
			hasParentCommentID = true
		}
	}
	if !hasMention {
		if _, err := s.db.Execute(ctx, `ALTER TABLE comments ADD COLUMN mention TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("store: comment mention migration: %w", err)
		}
	}
	if !hasParentCommentID {
		if _, err := s.db.Execute(ctx, `ALTER TABLE comments ADD COLUMN parent_comment_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("store: comment reply migration: %w", err)
		}
	}
	// Column roles: add the field to older databases, then backfill the
	// meaning each legacy fixed name carried. The backfill only fills an
	// empty role and never takes a role another column of the same project
	// already holds, so an explicit choice is not overwritten, roles stay
	// unique per project, and a second open changes nothing.
	columnColumns, err := s.db.Query(ctx, `PRAGMA table_info("columns")`)
	if err != nil {
		return fmt.Errorf("store: column schema: %w", err)
	}
	hasRole := false
	for _, column := range columnColumns.Values {
		if str(column[1]) == "role" {
			hasRole = true
		}
	}
	if !hasRole {
		if _, err := s.db.Execute(ctx, `ALTER TABLE "columns" ADD COLUMN role TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("store: column role migration: %w", err)
		}
		for _, role := range model.ColumnRoles {
			name := model.LegacyColumnName(role)
			if name == "" {
				continue
			}
			if _, err := s.db.Execute(ctx,
				`UPDATE "columns" SET role = ? WHERE name = ? AND role = ''
			 AND NOT EXISTS (SELECT 1 FROM "columns" taken WHERE taken.project_id = "columns".project_id AND taken.role = ?)`,
				role, name, role,
			); err != nil {
				return fmt.Errorf("store: column role backfill: %w", err)
			}
		}
	}
	// The review step is now presented as 等你. Rename only the old default
	// label on columns that already carry the review role; custom labels stay
	// intact, and the role backfill above still recognizes legacy databases.
	if _, err := s.db.Execute(ctx,
		`UPDATE "columns" SET name = '等你' WHERE role = ? AND name = '评审'`,
		model.ColumnRoleReview,
	); err != nil {
		return fmt.Errorf("store: review column label migration: %w", err)
	}
	columns, err := s.db.Query(ctx, `PRAGMA table_info(projects)`)
	if err != nil {
		return fmt.Errorf("store: project schema: %w", err)
	}
	hasWorkspace, hasColor := false, false
	for _, column := range columns.Values {
		switch str(column[1]) {
		case "workspace_path":
			hasWorkspace = true
		case "color":
			hasColor = true
		}
	}
	if !hasWorkspace {
		if _, err = s.db.Execute(ctx, `ALTER TABLE projects ADD COLUMN workspace_path TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("store: project workspace migration: %w", err)
		}
	}
	if !hasColor {
		if _, err = s.db.Execute(ctx, `ALTER TABLE projects ADD COLUMN color TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("store: project color migration: %w", err)
		}
	}
	if err := s.migrateTaskNumbers(ctx); err != nil {
		return err
	}
	return nil
}

// TaskNumberStart is the first human-facing task number assigned on a
// fresh or upgraded database. Numbers are global, sequential, and typically
// four digits so they are easy to say aloud.
const TaskNumberStart = 1001

// HexPrefixMinLen is the minimum length accepted for a unique hex-id prefix
// lookup (backward compatibility with older short-id copies).
const HexPrefixMinLen = 6

// FormatTaskNumber returns the human-facing display form of a task number
// (unpadded decimal, e.g. "1083"). Zero or negative values yield "".
func FormatTaskNumber(n int) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// migrateTaskNumbers adds the global sequential display number column,
// backfills existing rows ordered by created_at/rowid starting at
// TaskNumberStart, and enforces uniqueness.
func (s *Store) migrateTaskNumbers(ctx context.Context) error {
	cols, err := s.db.Query(ctx, `PRAGMA table_info(tasks)`)
	if err != nil {
		return fmt.Errorf("store: task schema: %w", err)
	}
	hasNumber := false
	for _, column := range cols.Values {
		if str(column[1]) == "number" {
			hasNumber = true
			break
		}
	}
	if !hasNumber {
		if _, err := s.db.Execute(ctx, `ALTER TABLE tasks ADD COLUMN number INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("store: task number migration: %w", err)
		}
	}
	pending, err := s.db.Query(ctx, `SELECT id FROM tasks WHERE number = 0 ORDER BY created_at, rowid`)
	if err != nil {
		return fmt.Errorf("store: task number backfill select: %w", err)
	}
	if len(pending.Values) > 0 {
		maxRows, err := s.db.Query(ctx, `SELECT COALESCE(MAX(number), 0) FROM tasks`)
		if err != nil {
			return fmt.Errorf("store: task number max: %w", err)
		}
		next := int(integer(maxRows.Values[0][0])) + 1
		if next < TaskNumberStart {
			next = TaskNumberStart
		}
		var stmts []sqlite.Statement
		for _, row := range pending.Values {
			stmts = append(stmts, sqlite.Statement{
				SQL:  `UPDATE tasks SET number = ? WHERE id = ?`,
				Args: []any{next, str(row[0])},
			})
			next++
		}
		if _, err := s.db.Transaction(ctx, stmts); err != nil {
			return fmt.Errorf("store: task number backfill: %w", err)
		}
	}
	if _, err := s.db.Execute(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS tasks_number ON tasks(number)`); err != nil {
		return fmt.Errorf("store: task number index: %w", err)
	}
	return nil
}

// nextTaskNumberLocked returns max(number)+1, never below TaskNumberStart.
func (s *Store) nextTaskNumberLocked(ctx context.Context) (int, error) {
	rows, err := s.db.Query(ctx, `SELECT COALESCE(MAX(number), 0) FROM tasks`)
	if err != nil {
		return 0, err
	}
	next := int(integer(rows.Values[0][0])) + 1
	if next < TaskNumberStart {
		next = TaskNumberStart
	}
	return next, nil
}

// newID returns a 16-byte random hex id.
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand unavailable is unrecoverable
	}
	return hex.EncodeToString(b)
}

// nowUTC is the single clock: second precision, UTC, matching the stored
// RFC3339 strings so events and responses never disagree.
func nowUTC() time.Time { return time.Now().UTC().Truncate(time.Second) }

func timestamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func emit(s *Store, ev model.Event) {
	s.seq++
	ev.Seq = s.seq
	if s.OnEvent != nil {
		s.OnEvent(ev)
	}
}
