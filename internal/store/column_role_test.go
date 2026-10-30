package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/plugins/sqlite"

	"taskboard-native/internal/model"
)

// legacyColumn is one column of a database written by an older build.
type legacyColumn struct {
	id   string
	name string
	role string // only stored when the fixture has the role field
}

// writeLegacyDB writes a pre-role database: no workspace_path and, unless
// withRole is set, no role on columns. Opening it exercises the migration.
func writeLegacyDB(t *testing.T, path string, withRole bool, columns []legacyColumn) {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, path, sqlite.OpenOptions{})
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	columnDDL := `CREATE TABLE "columns" (id TEXT PRIMARY KEY, project_id TEXT NOT NULL, name TEXT NOT NULL, position INTEGER NOT NULL DEFAULT 0)`
	if withRole {
		columnDDL = `CREATE TABLE "columns" (id TEXT PRIMARY KEY, project_id TEXT NOT NULL, name TEXT NOT NULL, position INTEGER NOT NULL DEFAULT 0, role TEXT NOT NULL DEFAULT '')`
	}
	stmts := []sqlite.Statement{
		{SQL: `CREATE TABLE projects (id TEXT PRIMARY KEY, name TEXT NOT NULL, position INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`},
		{SQL: columnDDL},
		{SQL: `INSERT INTO projects (id, name, position, created_at, updated_at) VALUES ('local', 'local', 0, '2026-10-01T00:00:00Z', '2026-10-01T00:00:00Z')`},
	}
	for i, c := range columns {
		if withRole {
			stmts = append(stmts, sqlite.Statement{
				SQL:  `INSERT INTO "columns" (id, project_id, name, position, role) VALUES (?, 'local', ?, ?, ?)`,
				Args: []any{c.id, c.name, (i + 1) * 1000, c.role},
			})
			continue
		}
		stmts = append(stmts, sqlite.Statement{
			SQL:  `INSERT INTO "columns" (id, project_id, name, position) VALUES (?, 'local', ?, ?)`,
			Args: []any{c.id, c.name, (i + 1) * 1000},
		})
	}
	if _, err := db.Transaction(ctx, stmts); err != nil {
		t.Fatalf("write legacy db: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}
}

func rolesByName(t *testing.T, s *Store) map[string]string {
	t.Helper()
	p, err := s.Project("local")
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	out := make(map[string]string, len(p.Columns))
	for _, c := range p.Columns {
		out[c.Name] = c.Role
	}
	return out
}

// TestColumnRoleBackfill covers the old-database upgrade: legacy names gain
// their role, other names stay empty, an explicitly set role survives, and
// reopening changes nothing (idempotent).
func TestColumnRoleBackfill(t *testing.T) {
	cases := []struct {
		name     string
		withRole bool
		columns  []legacyColumn
		want     map[string]string
	}{
		{
			name: "legacy four columns",
			columns: []legacyColumn{
				{id: "c1", name: "待办"},
				{id: "c2", name: "进行中"},
				{id: "c3", name: "评审"},
				{id: "c4", name: "完成"},
			},
			want: map[string]string{
				"待办":  model.ColumnRoleIntake,
				"进行中": model.ColumnRoleDoing,
				"等你":  model.ColumnRoleReview,
				"完成":  model.ColumnRoleDone,
			},
		},
		{
			name: "renamed columns stay empty",
			columns: []legacyColumn{
				{id: "c1", name: "收集箱"},
				{id: "c2", name: "进行中"},
				{id: "c3", name: "上线"},
				{id: "c4", name: "完成"},
			},
			want: map[string]string{
				"收集箱": "",
				"进行中": model.ColumnRoleDoing,
				"上线":  "",
				"完成":  model.ColumnRoleDone,
			},
		},
		{
			name:     "explicit role is not overwritten and stays unique",
			withRole: true,
			columns: []legacyColumn{
				// A custom label with the review role must stay unchanged; the
				// legacy 评审 column must not grab the role a second time.
				{id: "c1", name: "客户确认", role: model.ColumnRoleReview},
				{id: "c2", name: "进行中"},
				{id: "c3", name: "评审"},
				{id: "c4", name: "完成"},
			},
			want: map[string]string{
				"客户确认": model.ColumnRoleReview,
				"进行中":  "",
				"评审":   "",
				"完成":   "",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "legacy.db")
			writeLegacyDB(t, path, tc.withRole, tc.columns)

			s, err := Open(path)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			got := rolesByName(t, s)
			for name, want := range tc.want {
				if got[name] != want {
					t.Errorf("column %q role = %q, want %q", name, got[name], want)
				}
			}
			if len(got) != len(tc.want) {
				t.Errorf("columns = %v, want %d", got, len(tc.want))
			}
			if err := s.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}

			// Reopening runs the migration again: same roles, no duplicates.
			again, err := Open(path)
			if err != nil {
				t.Fatalf("reopen: %v", err)
			}
			defer again.Close()
			second := rolesByName(t, again)
			if len(second) != len(got) {
				t.Fatalf("reopen changed columns: %v, want %v", second, got)
			}
			for name, want := range got {
				if second[name] != want {
					t.Errorf("reopen: column %q role = %q, want %q", name, second[name], want)
				}
			}
		})
	}
}

func TestSeedColumnsCarryRoles(t *testing.T) {
	s := openTemp(t)
	want := map[string]string{
		"待办":  model.ColumnRoleIntake,
		"进行中": model.ColumnRoleDoing,
		"等你":  model.ColumnRoleReview,
		"完成":  model.ColumnRoleDone,
	}
	got := rolesByName(t, s)
	if len(got) != len(want) {
		t.Fatalf("seeded columns = %v, want %v", got, want)
	}
	for name, role := range want {
		if got[name] != role {
			t.Errorf("seeded column %q role = %q, want %q", name, got[name], role)
		}
	}
}

func TestCreateProjectColumnsCarryRoles(t *testing.T) {
	s := openTemp(t)
	p, err := s.CreateProject("roles", "test")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{model.ColumnRoleIntake, model.ColumnRoleDoing, model.ColumnRoleReview, model.ColumnRoleDone}
	if len(p.Columns) != len(want) {
		t.Fatalf("columns = %+v", p.Columns)
	}
	for i, role := range want {
		if p.Columns[i].Role != role {
			t.Errorf("column %d role = %q, want %q", i, p.Columns[i].Role, role)
		}
	}
	if p.Columns[2].Name != "等你" {
		t.Errorf("review column name = %q, want 等你", p.Columns[2].Name)
	}
	got, err := s.Project(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i, role := range want {
		if got.Columns[i].Role != role {
			t.Errorf("reread column %d role = %q, want %q", i, got.Columns[i].Role, role)
		}
	}
}

// TestSetColumnRole covers assignment, conflicts, clearing and the emitted
// project.changed event.
func TestSetColumnRole(t *testing.T) {
	cases := []struct {
		name       string
		column     int    // index into the project's default columns
		role       string // role to write
		clear      []int  // columns whose role is cleared before the write
		wantRole   string
		wantErr    string // "conflict" or "invalid", "" for success
		wantEvents int
	}{
		{name: "assign a free role", column: 1, role: model.ColumnRoleIntake, clear: []int{0}, wantRole: model.ColumnRoleIntake, wantEvents: 1},
		{name: "same value on the same column", column: 0, role: model.ColumnRoleIntake, wantRole: model.ColumnRoleIntake, wantEvents: 1},
		{name: "clear a role", column: 0, role: "", wantRole: "", wantEvents: 1},
		{name: "conflict with another column", column: 0, role: model.ColumnRoleDoing, wantErr: "conflict"},
		{name: "unknown role", column: 0, role: "finished", wantErr: "invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := openTemp(t)
			p, err := s.CreateProject("p-"+tc.name, "t")
			if err != nil {
				t.Fatal(err)
			}
			for _, i := range tc.clear {
				if _, err := s.SetColumnRole(p.ID, p.Columns[i].ID, "", "tester"); err != nil {
					t.Fatalf("clear before set: %v", err)
				}
			}
			var events []model.Event
			s.OnEvent = func(ev model.Event) { events = append(events, ev) }

			column, err := s.SetColumnRole(p.ID, p.Columns[tc.column].ID, tc.role, "tester")
			switch tc.wantErr {
			case "conflict":
				var conflict *RoleConflictError
				if !errors.As(err, &conflict) {
					t.Fatalf("err = %v, want RoleConflictError", err)
				}
				if conflict.ColumnID == p.Columns[tc.column].ID {
					t.Errorf("conflict names the column being written: %+v", conflict)
				}
				if len(events) != 0 {
					t.Errorf("conflict emitted %d events, want 0", len(events))
				}
				return
			case "invalid":
				var invalidErr *InvalidError
				if !errors.As(err, &invalidErr) {
					t.Fatalf("err = %v, want InvalidError", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("SetColumnRole: %v", err)
			}
			if column.Role != tc.wantRole || column.ID != p.Columns[tc.column].ID {
				t.Fatalf("returned column = %+v, want role %q", column, tc.wantRole)
			}
			if len(events) != tc.wantEvents {
				t.Fatalf("events = %d, want %d", len(events), tc.wantEvents)
			}
			ev := events[0]
			if ev.Type != model.EventProjectChange || ev.ProjectID != p.ID || ev.By != "tester" {
				t.Errorf("event = %+v", ev)
			}
			got, err := s.Project(p.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Columns[tc.column].Role != tc.wantRole {
				t.Fatalf("reread role = %q, want %q", got.Columns[tc.column].Role, tc.wantRole)
			}
		})
	}
}

func TestSetColumnRoleNotFound(t *testing.T) {
	s := openTemp(t)
	other, err := s.CreateProject("other", "t")
	if err != nil {
		t.Fatal(err)
	}
	local, err := s.Project("local")
	if err != nil {
		t.Fatalf("local project: %v", err)
	}
	cases := []struct {
		name      string
		projectID string
		columnID  string
	}{
		{"unknown project", "missing", local.Columns[0].ID},
		{"unknown column", "local", "missing"},
		{"column of another project", "local", other.Columns[0].ID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.SetColumnRole(tc.projectID, tc.columnID, "", "tester"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("err = %v, want ErrNotFound", err)
			}
		})
	}
}
