package main

import (
	"encoding/json"
	"strings"
	"testing"

	"taskboard-native/internal/model"
)

// cliColumns reads the projects endpoint and indexes local's columns by name.
func cliColumns(t *testing.T, bin string, env []string) map[string]model.Column {
	t.Helper()
	stdout, stderr, code := runTB(t, bin, env, "projects", "--json")
	if code != 0 {
		t.Fatalf("projects --json exit %d: %s", code, stderr)
	}
	var projects []projectInfo
	if err := json.Unmarshal([]byte(stdout), &projects); err != nil {
		t.Fatalf("decode projects: %v", err)
	}
	columns := map[string]model.Column{}
	for _, p := range projects {
		if p.Name != "local" {
			continue
		}
		for _, c := range p.Columns {
			columns[c.Name] = c
		}
	}
	return columns
}

// TestCLIColumnRole drives `tb column` end to end: assignment, clearing,
// conflict, unknown values and the --json shape.
func TestCLIColumnRole(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary")
	}
	srv := testService(t)
	bin := buildBinary(t)
	env := []string{"TASKBOARD_ADDR=" + srv.Addr, "TB_AGENT=cli-role"}
	columns := cliColumns(t, bin, env)
	if len(columns) != 4 {
		t.Fatalf("seeded columns = %v", columns)
	}
	if got := columns["完成"].Role; got != model.ColumnRoleDone {
		t.Fatalf("seeded 完成 role = %q, want done", got)
	}

	// Clear the review column, then hand its role to 待办.
	if stdout, stderr, code := runTB(t, bin, env, "column", columns["等你"].ID, ""); code != 0 {
		t.Fatalf("clear exit %d: %s %s", code, stderr, stdout)
	}
	if got := cliColumns(t, bin, env)["等你"].Role; got != "" {
		t.Fatalf("等你 role after clear = %q, want empty", got)
	}

	stdout, stderr, code := runTB(t, bin, env, "column", columns["待办"].ID, model.ColumnRoleReview)
	if code != 0 {
		t.Fatalf("assign exit %d: %s", code, stderr)
	}
	if fields := strings.Split(strings.TrimSpace(stdout), "\t"); len(fields) != 3 || fields[2] != model.ColumnRoleReview {
		t.Fatalf("assign output = %q", stdout)
	}
	if got := cliColumns(t, bin, env)["待办"].Role; got != model.ColumnRoleReview {
		t.Fatalf("待办 role = %q, want review", got)
	}

	// Setting the same role again on the same column is allowed.
	if _, stderr, code := runTB(t, bin, env, "column", columns["待办"].ID, model.ColumnRoleReview); code != 0 {
		t.Fatalf("same role exit %d: %s", code, stderr)
	}

	// A role another column holds is a 409 the CLI reports in Chinese.
	_, stderr, code = runTB(t, bin, env, "column", columns["进行中"].ID, model.ColumnRoleReview)
	if code != 1 || !strings.Contains(stderr, "该角色已被其他列占用") {
		t.Fatalf("conflict exit %d stderr %q", code, stderr)
	}

	// Unknown role values never reach the service.
	if _, stderr, code := runTB(t, bin, env, "column", columns["完成"].ID, "finished"); code != 2 || !strings.Contains(stderr, "unknown role") {
		t.Fatalf("unknown role exit %d stderr %q", code, stderr)
	}
	if _, stderr, code := runTB(t, bin, env, "column", "missing", model.ColumnRoleDone); code != 1 || !strings.Contains(stderr, "unknown column") {
		t.Fatalf("unknown column exit %d stderr %q", code, stderr)
	}

	// --json returns the updated column as JSON; a name also resolves.
	stdout, stderr, code = runTB(t, bin, env, "column", "完成", model.ColumnRoleDone, "--json")
	if code != 0 {
		t.Fatalf("json exit %d: %s", code, stderr)
	}
	var column model.Column
	if err := json.Unmarshal([]byte(stdout), &column); err != nil {
		t.Fatalf("decode json output %q: %v", stdout, err)
	}
	if column.Name != "完成" || column.Role != model.ColumnRoleDone {
		t.Fatalf("json column = %+v", column)
	}

	// Usage errors stay usage errors.
	if _, _, code := runTB(t, bin, env, "column", "only-one-arg"); code != 2 {
		t.Fatalf("missing argument exit = %d, want 2", code)
	}
}
