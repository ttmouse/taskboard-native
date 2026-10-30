package main

import (
	"testing"
	"time"

	"taskboard-native/internal/model"
)

// renamedRoleBoard is the demo board with every column renamed but still
// carrying its workflow role — the shape a project has once roles, not the
// fixed Chinese names, say what a column means.
func renamedRoleBoard() *fakeBoard {
	f := seedBoard()
	f.projects[0].Columns = []model.Column{
		{ID: "c-todo", Name: "收集箱", Role: model.ColumnRoleIntake},
		{ID: "c-doing", Name: "在做", Role: model.ColumnRoleDoing},
		{ID: "c-review", Name: "验收中", Role: model.ColumnRoleReview},
		{ID: "c-done", Name: "已上线", Role: model.ColumnRoleDone},
	}
	return f
}

// legacyNameColumns is an old board: fixed Chinese names, no roles at all.
func legacyNameColumns() []model.Column {
	return []model.Column{
		{ID: "c-todo", Name: "待办"},
		{ID: "c-doing", Name: "进行中"},
		{ID: "c-review", Name: "评审"},
		{ID: "c-done", Name: "完成"},
	}
}

// TestColumnRoleResolution pins the GUI's role lookups: a role wins over a
// name, the legacy name is still the fallback, and an unknown role resolves
// to nothing instead of the wrong column.
func TestColumnRoleResolution(t *testing.T) {
	cases := []struct {
		name    string
		columns []model.Column
		role    string
		want    string
	}{
		{"renamed columns resolve by role", renamedRoleBoard().projects[0].Columns, model.ColumnRoleIntake, "c-todo"},
		{"review role", renamedRoleBoard().projects[0].Columns, model.ColumnRoleReview, "c-review"},
		{"done role", renamedRoleBoard().projects[0].Columns, model.ColumnRoleDone, "c-done"},
		{"legacy names still resolve", legacyNameColumns(), model.ColumnRoleDone, "c-done"},
		{"legacy intake", legacyNameColumns(), model.ColumnRoleIntake, "c-todo"},
		{
			name: "role beats a column still carrying the legacy name",
			columns: []model.Column{
				{ID: "old", Name: "完成"},
				{ID: "new", Name: "已上线", Role: model.ColumnRoleDone},
			},
			role: model.ColumnRoleDone,
			want: "new",
		},
		{"renamed board without roles resolves nothing", []model.Column{{ID: "new", Name: "已上线"}}, model.ColumnRoleDone, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := &board{
				cur:      "local",
				columns:  tc.columns,
				projects: []model.Project{{ID: "local", Columns: tc.columns}},
			}
			if got := b.columnIDByRole(tc.role); got != tc.want {
				t.Fatalf("columnIDByRole(%q) = %q, want %q", tc.role, got, tc.want)
			}
			if tc.want == "" {
				return
			}
			if !b.columnHasRole(tc.want, tc.role) {
				t.Fatalf("columnHasRole(%q, %q) = false, want true", tc.want, tc.role)
			}
			for _, c := range tc.columns {
				if c.ID != tc.want && b.columnHasRole(c.ID, tc.role) {
					t.Fatalf("columnHasRole(%q, %q) = true, only %q plays it", c.ID, tc.role, tc.want)
				}
			}
		})
	}
}

// TestTaskFormTargetsIntakeRole checks the create sheet lands in the intake
// column even after it was renamed.
func TestTaskFormTargetsIntakeRole(t *testing.T) {
	b := &board{
		cur:      "local",
		columns:  renamedRoleBoard().projects[0].Columns,
		projects: renamedRoleBoard().projects,
	}
	if got := b.defaultTaskColumnID(); got != "c-todo" {
		t.Fatalf("defaultTaskColumnID = %q, want c-todo", got)
	}
	if got := b.projectColumnIDByRole("local", model.ColumnRoleIntake); got != "c-todo" {
		t.Fatalf("projectColumnIDByRole = %q, want c-todo", got)
	}
	// A project without the role keeps the old first-column fallback.
	plain := &board{projects: []model.Project{{ID: "plain", Columns: []model.Column{{ID: "only", Name: "一列"}}}}}
	if got := plain.projectColumnIDByRole("plain", model.ColumnRoleIntake); got != "only" {
		t.Fatalf("fallback = %q, want only", got)
	}
	if got := plain.projectColumnIDByRole("missing", model.ColumnRoleIntake); got != "" {
		t.Fatalf("unknown project = %q, want empty", got)
	}
}

// TestReviewChipFollowsRole renders a board whose review column was renamed:
// the ✓ chip must still show on its cards, because it is keyed on the role.
func TestReviewChipFollowsRole(t *testing.T) {
	isolatePrefs(t)
	b := newBoard(renamedRoleBoard())
	tt := newBoardTester(b)
	waitFor(t, tt, "重命名看板载入", func() bool { return tt.HasText("版本冲突时拉一次详情对齐") })
	b.cur = "local"
	tt.Frame()
	settle(t, tt)
	if !tt.HasText("验收中") {
		t.Fatalf("renamed review column missing; texts: %v", tt.Texts())
	}
	if _, ok := tt.Find("验收通过，移到完成"); !ok {
		t.Fatalf("review chip missing on a renamed review column; texts: %v", tt.Texts())
	}
}

// TestMoveToDoneRoleLiftsToTop pins the drop rule: a bare drop into the
// column playing the done role lands at its top, whatever that column is
// called. A drop into any other column still goes to the bottom.
func TestMoveToDoneRoleLiftsToTop(t *testing.T) {
	isolatePrefs(t)
	f := renamedRoleBoard()
	b := newBoard(f)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	b.cur = "local"
	tt.Frame()

	b.moveTask("t-1", "c-done", "", false)
	waitTask(t, f, "t-1", func(tk model.Task) bool { return tk.ColumnID == "c-done" })
	if got := fakeTask(t, f, "t-1").Position; got != 0.5 {
		t.Fatalf("done-role drop position = %g, want 0.5 (column top)", got)
	}

	b.moveTask("t-2", "c-doing", "", false)
	waitTask(t, f, "t-2", func(tk model.Task) bool { return tk.ColumnID == "c-doing" })
	if got := fakeTask(t, f, "t-2").Position; got == 0.5 {
		t.Fatalf("drop into a non-done column landed at the top")
	}
}

func fakeTask(t *testing.T, f *fakeBoard, id string) model.Task {
	t.Helper()
	task, err := f.Task(id)
	if err != nil {
		t.Fatalf("fake task %s: %v", id, err)
	}
	return task
}

func waitTask(t *testing.T, f *fakeBoard, id string, cond func(model.Task) bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond(fakeTask(t, f, id)) {
		if time.Now().After(deadline) {
			t.Fatalf("task %s did not reach the expected state: %+v", id, fakeTask(t, f, id))
		}
		time.Sleep(2 * time.Millisecond)
	}
}
