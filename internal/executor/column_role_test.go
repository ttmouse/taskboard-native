package executor

import (
	"testing"

	"taskboard-native/internal/model"
	"taskboard-native/internal/store"
)

// TestLegacyReceiptUsesColumnRole covers both halves of the doing-column
// lookup: a project whose doing role sits on a renamed column recovers the
// legacy receipt, and a project with no roles at all still finds「进行中」by
// its old name.
func TestLegacyReceiptUsesColumnRole(t *testing.T) {
	const original = "aa_ebf99dac28d7d70e_taskboard-b5bc2c2f62af492e51edcdf429bfbce4"
	cases := []struct {
		name  string
		setup func(t *testing.T, st *store.Store, p model.Project) string // returns the doing column id
	}{
		{
			name: "role on a renamed column",
			setup: func(t *testing.T, st *store.Store, p model.Project) string {
				// Clear the seeded roles, then hand doing to another column —
				// the renamed board a user can build through the API.
				for _, c := range p.Columns {
					if _, err := st.SetColumnRole(p.ID, c.ID, "", "test"); err != nil {
						t.Fatal(err)
					}
				}
				renamed, err := st.SetColumnRole(p.ID, p.Columns[2].ID, model.ColumnRoleDoing, "test")
				if err != nil {
					t.Fatal(err)
				}
				return renamed.ID
			},
		},
		{
			name: "no roles, legacy name fallback",
			setup: func(t *testing.T, st *store.Store, p model.Project) string {
				for _, c := range p.Columns {
					if _, err := st.SetColumnRole(p.ID, c.ID, "", "test"); err != nil {
						t.Fatal(err)
					}
				}
				for _, c := range p.Columns {
					if c.Name == "进行中" {
						return c.ID
					}
				}
				t.Fatal("seeded 进行中 column missing")
				return ""
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fakeDesktop(t, func(method string, params map[string]any) any {
				switch method {
				case "session.createAndStart":
					t.Error("legacy upgrade resubmitted task")
					return nil
				case "session.getState":
					return map[string]any{"status": "idle"}
				case "session.getSnapshot":
					return map[string]any{"snapshotComplete": true, "items": []any{
						map[string]any{"type": "turn.end", "status": "done", "turnId": "t1"},
					}}
				}
				return nil
			})
			st := newTestStore(t)
			p, task := projectWithWorkspace(t, st)
			doing := tc.setup(t, st, p)
			if _, err := st.MoveTask(task.ID, task.Version, doing, "", "test"); err != nil {
				t.Fatal(err)
			}
			if _, err := st.AddComment(task.ID, "已交给 DSH 桌面端。会话 ID："+original, "DSH"); err != nil {
				t.Fatal(err)
			}
			d := NewDispatcher(st, desktopTestConfig(t))
			defer d.Close()
			if d.recoveryErr != nil {
				t.Fatalf("recovery: %v", d.recoveryErr)
			}
			run, err := st.Execution(task.ID)
			if err != nil || run == nil || run.SessionID != original {
				t.Fatalf("execution = %+v, %v; want the legacy receipt imported", run, err)
			}
		})
	}
}
