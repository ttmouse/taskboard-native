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
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"

	"taskboard-native/internal/model"
)

// ErrNotFound reports a missing project, column, task or comment.
var ErrNotFound = errors.New("not found")

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
  position INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS "columns" (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  name TEXT NOT NULL,
  position INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS columns_project ON "columns"(project_id, position);
CREATE TABLE IF NOT EXISTS tasks (
  id TEXT PRIMARY KEY,
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
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS comments_task ON comments(task_id, created_at);
`

// DefaultColumns returns the seeded board layout.
func DefaultColumns() []model.Column {
	return []model.Column{
		{Name: "待办"},
		{Name: "进行中"},
		{Name: "评审"},
		{Name: "完成"},
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

// Open opens (creating and seeding when empty) the database at path.
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
	if err := s.seed(ctx); err != nil {
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
	return nil
}

// seed installs the local project, its four columns and three demo cards
// the first time the database is opened.
func (s *Store) seed(ctx context.Context) error {
	rows, err := s.db.Query(ctx, `SELECT COUNT(*) FROM projects`)
	if err != nil {
		return err
	}
	if n, _ := rows.Values[0][0].(int64); n > 0 {
		return nil
	}
	now := timestamp(nowUTC())
	stmts := []sqlite.Statement{{
		SQL:  `INSERT INTO projects (id, name, position, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		Args: []any{"local", "local", 0, now, now},
	}}
	cols := DefaultColumns()
	for i := range cols {
		cols[i].ID = newID()
		stmts = append(stmts, sqlite.Statement{
			SQL:  `INSERT INTO "columns" (id, project_id, name, position) VALUES (?, ?, ?, ?)`,
			Args: []any{cols[i].ID, "local", cols[i].Name, (i + 1) * 1000},
		})
	}
	demos := []struct{ title, body, column string }{
		{"欢迎使用 taskboard", "这是本地看板的第一张卡片：拖动它试试。", cols[0].ID},
		{"接上 CLI 与 MCP", "代理可用 tb 或 MCP 写入，窗口会实时收到事件。", cols[1].ID},
		{"检查实时推送", "任何写入都会通过 SSE 推送到已打开窗口。", cols[2].ID},
	}
	// One demo card per column, each the first card of its column.
	for _, d := range demos {
		stmts = append(stmts, sqlite.Statement{
			SQL: `INSERT INTO tasks (id, project_id, column_id, title, body, assignee, position, version, archived, last_agent, created_at, updated_at)
			      VALUES (?, ?, ?, ?, ?, '', 1000, 1, 0, 'local', ?, ?)`,
			Args: []any{newID(), "local", d.column, d.title, d.body, now, now},
		})
	}
	_, err = s.db.Transaction(ctx, stmts)
	return err
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
