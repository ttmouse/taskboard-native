package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"taskboard-native/internal/model"
	"taskboard-native/internal/service"
)

// buildBinary compiles the CLI once per test into a temp dir, so the test
// exercises the real binary (flag parsing and exit codes included).
func buildBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "tb")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

// testService starts a service on a random port with a temp database.
func testService(t *testing.T) *service.Server {
	t.Helper()
	srv, err := service.Start(":0", filepath.Join(t.TempDir(), "taskboard.db"))
	if err != nil {
		t.Fatalf("start service: %v", err)
	}
	t.Cleanup(func() {
		if err := srv.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	})
	return srv
}

func runTB(t *testing.T, bin string, env []string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if ok := asExitError(err, &exit); ok {
			code = exit.ExitCode()
		} else {
			t.Fatalf("run %v: %v", args, err)
		}
	}
	return stdout.String(), stderr.String(), code
}

func asExitError(err error, target **exec.ExitError) bool {
	if e, ok := err.(*exec.ExitError); ok {
		*target = e
		return true
	}
	return false
}

func TestCLIEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary")
	}
	srv := testService(t)
	bin := buildBinary(t)
	env := []string{"TASKBOARD_ADDR=" + srv.Addr, "TB_AGENT=cli-e2e"}

	// projects
	stdout, stderr, code := runTB(t, bin, env, "projects")
	if code != 0 {
		t.Fatalf("projects exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "local") {
		t.Fatalf("projects output = %q", stdout)
	}

	// list: three seeded cards
	stdout, stderr, code = runTB(t, bin, env, "list")
	if code != 0 {
		t.Fatalf("list exit %d: %s", code, stderr)
	}
	if lines := nonEmptyLines(stdout); len(lines) != 3 {
		t.Fatalf("list = %q, want 3 seeded rows", stdout)
	}

	// add: by column name, identity from TB_AGENT
	stdout, stderr, code = runTB(t, bin, env, "add", "CLI 创建", "--column", "进行中", "--body", "来自端到端测试")
	if code != 0 {
		t.Fatalf("add exit %d: %s", code, stderr)
	}
	fields := strings.Split(strings.TrimSpace(stdout), "\t")
	if len(fields) < 4 {
		t.Fatalf("add output = %q", stdout)
	}
	taskID := fields[0]
	task := fetchTask(t, srv, taskID)
	if task.Title != "CLI 创建" || task.Description != "来自端到端测试" || task.UpdatedBy != "cli-e2e" {
		t.Fatalf("created task = %+v", task)
	}
	_, raw := fetchBoard(t, srv)
	var board service.Board
	if err := json.Unmarshal(raw, &board); err != nil {
		t.Fatalf("decode board: %v", err)
	}
	if task.ColumnID != board.Columns[1].ID {
		t.Fatalf("created in column %s, want 进行中 %s", task.ColumnID, board.Columns[1].ID)
	}

	// move: by column name
	stdout, stderr, code = runTB(t, bin, env, "move", taskID, "评审")
	if code != 0 {
		t.Fatalf("move exit %d: %s", code, stderr)
	}
	moved := fetchTask(t, srv, taskID)
	if moved.ColumnID != board.Columns[2].ID || moved.Version != task.Version+1 {
		t.Fatalf("moved task = %+v, want 评审 and v%d", moved, task.Version+1)
	}

	// move --top: first card of 完成
	stdout, stderr, code = runTB(t, bin, env, "move", taskID, "完成", "--top")
	if code != 0 {
		t.Fatalf("move --top exit %d: %s", code, stderr)
	}
	topped := fetchTask(t, srv, taskID)
	if topped.ColumnID != board.Columns[3].ID {
		t.Fatalf("move --top column = %s, want 完成", topped.ColumnID)
	}
	if topped.Position != 1000 {
		t.Fatalf("move --top position = %v, want 1000", topped.Position)
	}

	// comment
	stdout, stderr, code = runTB(t, bin, env, "comment", taskID, "端到端评论")
	if code != 0 || !strings.Contains(stdout, "端到端评论") {
		t.Fatalf("comment exit %d: %s%s", code, stderr, stdout)
	}

	// archive
	stdout, stderr, code = runTB(t, bin, env, "archive", taskID)
	if code != 0 || !strings.Contains(stdout, "archived") {
		t.Fatalf("archive exit %d: %s%s", code, stderr, stdout)
	}
	if archived := fetchTask(t, srv, taskID); !archived.Archived {
		t.Fatalf("task not archived: %+v", archived)
	}

	// unknown column is a usage-level failure, not a crash
	_, stderr, code = runTB(t, bin, env, "move", taskID, "不存在的列")
	if code == 0 {
		t.Fatalf("move to unknown column succeeded")
	}
	if !strings.Contains(stderr, "unknown column") {
		t.Fatalf("stderr = %q, want unknown column", stderr)
	}
}

func TestCLIStartsServiceWhenDown(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary")
	}
	bin := buildBinary(t)
	db := filepath.Join(t.TempDir(), "auto.db")
	addr := freeAddr(t)

	env := []string{"TASKBOARD_ADDR=" + addr, "TASKBOARD_DB=" + db, "TB_AGENT=cli-e2e"}
	stdout, stderr, code := runTB(t, bin, env, "projects")
	if code != 0 {
		t.Fatalf("projects exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "local") {
		t.Fatalf("output = %q", stdout)
	}

	// The detached service must be reachable and killable afterwards.
	req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/api/health", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("auto-started service unreachable: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health = %d", resp.StatusCode)
	}
	// Stop the detached process so it does not outlive the test.
	killPort(t, addr)
}

// freeAddr reserves a loopback port and releases it for the CLI to claim.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	return addr
}

// killPort stops whatever listens on addr's port, so a detached service
// started by the CLI during the test is cleaned up.
func killPort(t *testing.T, addr string) {
	t.Helper()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}
	out, err := exec.Command("lsof", "-ti", "tcp:"+port, "-sTCP:LISTEN").Output()
	if err != nil {
		return // nothing listening
	}
	for _, pid := range strings.Fields(string(out)) {
		_ = exec.Command("kill", pid).Run()
	}
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

func fetchTask(t *testing.T, srv *service.Server, id string) model.Task {
	t.Helper()
	resp, err := http.Get(srv.URL + "/api/tasks/" + id)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get task status = %d %s", resp.StatusCode, raw)
	}
	var task model.Task
	if err := json.Unmarshal(raw, &task); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	return task
}

func fetchBoard(t *testing.T, srv *service.Server) (int, []byte) {
	t.Helper()
	resp, err := http.Get(srv.URL + "/api/projects/local/board")
	if err != nil {
		t.Fatalf("get board: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}
