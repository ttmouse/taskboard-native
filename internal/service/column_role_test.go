package service

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"taskboard-native/internal/model"
)

// TestPatchColumnRole covers the role endpoint: assignment, clearing the
// role, and every rejection the contract names. Each case gets its own
// project so the cases stay independent.
func TestPatchColumnRole(t *testing.T) {
	srv := start(t)
	var seq int
	newProject := func(t *testing.T) model.Project {
		t.Helper()
		seq++
		p, err := srv.store.CreateProject("role-"+string(rune('a'+seq)), "test")
		if err != nil {
			t.Fatalf("create project: %v", err)
		}
		return p
	}
	cases := []struct {
		name       string
		column     int    // index into the project's default columns; -1 = unknown id
		role       string // role to write
		omitRole   bool   // send a body without the role field
		clear      []int  // columns whose role is cleared first
		otherCol   bool   // point at a column of a different project
		badProject bool   // point at a project that does not exist
		wantStatus int
		wantError  string
		wantRole   string
	}{
		{name: "clear a role", column: 2, role: "", wantStatus: http.StatusOK, wantRole: ""},
		{name: "assign the freed role to another column", column: 0, role: model.ColumnRoleReview, clear: []int{2}, wantStatus: http.StatusOK, wantRole: model.ColumnRoleReview},
		{name: "same value on the same column", column: 3, role: model.ColumnRoleDone, wantStatus: http.StatusOK, wantRole: model.ColumnRoleDone},
		{name: "conflict with the column already holding it", column: 1, role: model.ColumnRoleDone, wantStatus: http.StatusConflict, wantError: "role_conflict"},
		{name: "unknown role", column: 3, role: "finished", wantStatus: http.StatusBadRequest},
		{name: "missing role field", column: 3, omitRole: true, wantStatus: http.StatusBadRequest},
		{name: "unknown project", column: 3, role: model.ColumnRoleDone, badProject: true, wantStatus: http.StatusNotFound},
		{name: "unknown column", column: -1, role: model.ColumnRoleDone, wantStatus: http.StatusNotFound},
		{name: "column of another project", column: 3, role: model.ColumnRoleDone, otherCol: true, wantStatus: http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newProject(t)
			projectID := p.ID
			columnID := "missing"
			switch {
			case tc.otherCol:
				columnID = newProject(t).Columns[0].ID
			case tc.column >= 0:
				columnID = p.Columns[tc.column].ID
			}
			if tc.badProject {
				projectID = "missing"
			}
			for _, i := range tc.clear {
				if _, err := srv.store.SetColumnRole(p.ID, p.Columns[i].ID, "", "tester"); err != nil {
					t.Fatalf("clear before patch: %v", err)
				}
			}
			body := map[string]any{"role": tc.role}
			if tc.omitRole {
				body = map[string]any{}
			}
			status, raw := request(t, srv, http.MethodPatch, "/api/projects/"+projectID+"/columns/"+columnID, "tester", body)
			if status != tc.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", status, tc.wantStatus, raw)
			}
			if tc.wantError != "" {
				var errBody struct {
					Error string `json:"error"`
				}
				if err := json.Unmarshal(raw, &errBody); err != nil {
					t.Fatalf("decode error body: %v", err)
				}
				if errBody.Error != tc.wantError {
					t.Fatalf("error = %q, want %q", errBody.Error, tc.wantError)
				}
				// A rejected patch must leave the roles untouched.
				board, err := srv.store.Board(p.ID)
				if err != nil {
					t.Fatal(err)
				}
				if tc.column >= 0 && board.Columns[tc.column].Role == tc.role {
					t.Fatalf("rejected patch still wrote role %q", tc.role)
				}
				return
			}
			if tc.wantStatus != http.StatusOK {
				return
			}
			var column model.Column
			if err := json.Unmarshal(raw, &column); err != nil {
				t.Fatalf("decode column: %v", err)
			}
			if column.ID != columnID || column.Role != tc.wantRole {
				t.Fatalf("column = %+v, want id %s role %q", column, columnID, tc.wantRole)
			}
			// The write must show up on the board, not just in the reply.
			board, err := srv.store.Board(p.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range board.Columns {
				if c.ID == columnID && c.Role != tc.wantRole {
					t.Fatalf("board role = %q, want %q", c.Role, tc.wantRole)
				}
			}
		})
	}
}

// TestPatchColumnRolePushesProjectChanged pins the realtime half of the
// contract: the GUI learns about a role change from project.changed.
func TestPatchColumnRolePushesProjectChanged(t *testing.T) {
	srv := start(t)
	p, err := srv.store.CreateProject("sse-role", "test")
	if err != nil {
		t.Fatal(err)
	}
	stream := subscribe(t, srv, "?projectId="+p.ID)
	// The stream opens with a comment line; wait for it so the subscription
	// is registered before the write.
	select {
	case <-stream.raw:
	case <-time.After(5 * time.Second):
		t.Fatal("event stream did not open")
	}
	status, raw := request(t, srv, http.MethodPatch, "/api/projects/"+p.ID+"/columns/"+p.Columns[0].ID, "tester",
		map[string]any{"role": ""})
	if status != http.StatusOK {
		t.Fatalf("patch: %d %s", status, raw)
	}
	ev := nextEvent(t, stream)
	if ev.Type != model.EventProjectChange || ev.ProjectID != p.ID || ev.By != "tester" {
		t.Fatalf("event = %+v, want project.changed for %s by tester", ev, p.ID)
	}
}
