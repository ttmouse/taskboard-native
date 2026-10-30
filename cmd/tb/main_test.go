package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tbclient "taskboard-native/internal/client"
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
	// A newly opened service starts projectless; the CLI scenarios below
	// expect the legacy「local」board with its three demo cards, so seed it
	// through the client API.
	c := tbclient.New(&tbclient.Discover{Addr: srv.Addr}, "test-agent")
	proj, err := c.CreateProject("local")
	if err != nil {
		t.Fatalf("fixture project: %v", err)
	}
	for i, title := range []string{"欢迎使用 taskboard", "接上 CLI 与 MCP", "检查实时推送"} {
		if _, err := c.CreateTask(proj.ID, proj.Columns[i].ID, title, title+"的说明", ""); err != nil {
			t.Fatalf("fixture task %d: %v", i, err)
		}
	}
	return srv
}

func installFakeHerdr(t *testing.T, folder, state string) {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate fake Herdr fixture")
	}
	binDir := filepath.Join(filepath.Dir(source), "..", "..", "testdata", "fake-herdr")
	logs := t.TempDir()
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TASKBOARD_TEST_HERDR_FOLDER", folder)
	t.Setenv("TASKBOARD_TEST_HERDR_STATE", state)
	t.Setenv("TASKBOARD_TEST_HERDR_ARGS", filepath.Join(logs, "herdr-args"))
	t.Setenv("TASKBOARD_TEST_HC_SENDS", filepath.Join(logs, "hc-sends"))
	t.Setenv("TASKBOARD_TEST_HERDR_COUNTER", filepath.Join(logs, "counter"))
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

// `tb add --dispatch` must reach the one-call create+dispatch mode: the
// service creates the card and the channel run finishes on it.
func TestCLIAddDispatch(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte("#!/bin/sh\nprintf 'CLI_ADD_DISPATCH_OK\\n'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	srv := testService(t)
	c := clientFor(srv.Addr)
	c.agent = "cli-dispatch-test"
	raw, err := c.createProject("CLI 一步派发", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var p projectInfo
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	installFakeHerdr(t, p.WorkspacePath, "done")
	bin := buildBinary(t)
	env := []string{"TASKBOARD_ADDR=" + srv.Addr, "TB_AGENT=cli-dispatch"}
	stdout, stderr, code := runTB(t, bin, env, "add", "CLI 建卡即派发", "--project", p.ID, "--dispatch", "codex")
	if code != 0 {
		t.Fatalf("add --dispatch exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "已派发 codex") {
		t.Fatalf("add --dispatch output = %q", stdout)
	}
	taskID := strings.Fields(strings.TrimSpace(stdout))[0]
	deadline := time.Now().Add(5 * time.Second)
	for {
		current, err := c.task(taskID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Execution != nil && current.Execution.Status == "returned" && strings.HasPrefix(current.Execution.SessionID, "herdr:") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dispatched run did not finish: %+v", current)
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Unknown channel: the CLI fails without leaving a card behind.
	before := cliProjectTaskCount(t, bin, env, p.ID)
	stdout, stderr, code = runTB(t, bin, env, "add", "坏渠道", "--project", p.ID, "--dispatch", "nope")
	if code == 0 || !strings.Contains(stderr, "unknown runtime") {
		t.Fatalf("bad channel exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	if after := cliProjectTaskCount(t, bin, env, p.ID); after != before {
		t.Fatalf("rejected add left a card: %d -> %d", before, after)
	}
}

func cliProjectTaskCount(t *testing.T, bin string, env []string, projectID string) int {
	t.Helper()
	stdout, stderr, code := runTB(t, bin, env, "list", "--project", projectID)
	if code != 0 {
		t.Fatalf("list exit %d: %s", code, stderr)
	}
	return len(nonEmptyLines(stdout))
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
	stdout, stderr, code = runTB(t, bin, env, "move", taskID, "等你")
	if code != 0 {
		t.Fatalf("move exit %d: %s", code, stderr)
	}
	moved := fetchTask(t, srv, taskID)
	if moved.ColumnID != board.Columns[2].ID || moved.Version != task.Version+1 {
		t.Fatalf("moved task = %+v, want 等你 and v%d", moved, task.Version+1)
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
	// A freshly opened database starts projectless — seeding was removed —
	// so the list comes back empty rather than containing a demo board.
	if strings.Contains(stdout, "local") {
		t.Fatalf("fresh projects = %q, want no seeded board", stdout)
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

func TestCLIReadAndProjectCommands(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary")
	}
	listQueries := make(chan url.Values, 2)
	createRequests := make(chan map[string]string, 1)
	const projectJSON = `[{"id":"local","name":"Local","workspacePath":"/tmp/workspace","columns":[{"id":"col-todo","name":"To do"},{"id":"col-doing","name":"Doing"}]}]`
	const fullTaskJSON = `{"id":"task-1","projectId":"local","columnId":"col-doing","title":"Detailed","description":"Full body\nSecond line","assignee":"Alice","position":1000,"version":3,"archived":false,"comments":[{"id":"comment-1","author":"Bob","text":"Review note","createdAt":"2026-10-25T00:01:00Z"}],"createdAt":"2026-10-25T00:00:00Z","updatedAt":"2026-10-25T00:02:00Z","updatedBy":"writer"}`
	const summaryJSON = `[{"id":"task-1","projectId":"local","columnId":"col-doing","title":"Summary","version":2,"updatedAt":"2026-10-25T00:00:00Z","updatedBy":"agent","assignee":"Alice"}]`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/health":
			_, _ = io.WriteString(w, `{"ok":true}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects":
			_, _ = io.WriteString(w, projectJSON)
		case r.Method == http.MethodPost && r.URL.Path == "/api/projects":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			createRequests <- body
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"project-new","name":"New board","workspacePath":"/tmp/work","columns":[]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/tasks":
			query := r.URL.Query()
			listQueries <- query
			if query.Get("summary") == "true" {
				_, _ = io.WriteString(w, summaryJSON)
			} else {
				_, _ = io.WriteString(w, `[`+fullTaskJSON+`]`)
			}
		case r.Method == http.MethodGet && r.URL.Path == "/api/tasks/task-1":
			_, _ = io.WriteString(w, fullTaskJSON)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	addr := strings.TrimPrefix(server.URL, "http://")
	bin := buildBinary(t)

	stdout, stderr, code := runTB(t, bin, nil, "projects", "--json", "--addr", addr)
	if code != 0 || !strings.Contains(stdout, `"workspacePath":"/tmp/workspace"`) {
		t.Fatalf("projects --json exit %d: %s%s", code, stderr, stdout)
	}
	stdout, stderr, code = runTB(t, bin, nil, "projects", "--addr", addr)
	if code != 0 || !strings.Contains(stdout, "local\tLocal\t/tmp/workspace") {
		t.Fatalf("projects text exit %d: %s%s", code, stderr, stdout)
	}

	stdout, stderr, code = runTB(t, bin, nil, "list", "--project", "local", "--column", "Doing", "--assignee", "Alice", "--limit", "2", "--offset", "1", "--summary", "--json", "--addr", addr)
	if code != 0 {
		t.Fatalf("filtered list exit %d: %s%s", code, stderr, stdout)
	}
	var gotSummary []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &gotSummary); err != nil || len(gotSummary) != 1 {
		t.Fatalf("summary JSON = %q, err %v", stdout, err)
	}
	for _, forbidden := range []string{"description", "comments", "position", "archived"} {
		if _, ok := gotSummary[0][forbidden]; ok {
			t.Fatalf("summary JSON leaked %q: %s", forbidden, stdout)
		}
	}
	query := <-listQueries
	for key, want := range map[string]string{"projectId": "local", "columnId": "col-doing", "assignee": "Alice", "limit": "2", "offset": "1", "summary": "true"} {
		if got := query.Get(key); got != want {
			t.Fatalf("list query %s = %q, want %q", key, got, want)
		}
	}

	stdout, stderr, code = runTB(t, bin, nil, "list", "--project", "local", "--addr", addr)
	if code != 0 || !strings.Contains(stdout, "task-1\tDoing\tDetailed\tv3\twriter") {
		t.Fatalf("legacy list table exit %d: %s%s", code, stderr, stdout)
	}
	<-listQueries

	stdout, stderr, code = runTB(t, bin, nil, "show", "task-1", "--json", "--addr", addr)
	if code != 0 || !strings.Contains(stdout, `"comments":[{"id":"comment-1"`) {
		t.Fatalf("show --json exit %d: %s%s", code, stderr, stdout)
	}
	stdout, stderr, code = runTB(t, bin, nil, "show", "task-1", "--addr", addr)
	if code != 0 || !strings.Contains(stdout, "Full body") || !strings.Contains(stdout, "Bob:") || !strings.Contains(stdout, "Review note") {
		t.Fatalf("show text exit %d: %s%s", code, stderr, stdout)
	}

	stdout, stderr, code = runTB(t, bin, nil, "project", "add", "New board", "--path", "/tmp/work", "--json", "--addr", addr)
	if code != 0 || !strings.Contains(stdout, `"id":"project-new"`) {
		t.Fatalf("project add exit %d: %s%s", code, stderr, stdout)
	}
	created := <-createRequests
	if created["name"] != "New board" || created["workspacePath"] != "/tmp/work" {
		t.Fatalf("project request body = %#v", created)
	}
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
	// Resolve the fixture project by name: created projects no longer get
	// the literal "local" id.
	listResp, err := http.Get(srv.URL + "/api/projects")
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	defer listResp.Body.Close()
	var projects []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&projects); err != nil {
		t.Fatalf("decode projects: %v", err)
	}
	id := ""
	for _, p := range projects {
		if p.Name == "local" {
			id = p.ID
		}
	}
	if id == "" {
		t.Fatal("fixture project not found")
	}
	resp, err := http.Get(srv.URL + "/api/projects/" + id + "/board")
	if err != nil {
		t.Fatalf("get board: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// TestGlobalFlagsBeforeCommand checks the documented form
// `tb --agent rams list` parses, not only the flag-after-command form.
func TestGlobalFlagsBeforeCommand(t *testing.T) {
	if got := run([]string{"--agent", "rams", "projects", "--addr", "127.0.0.1:1"}); got != exitError {
		t.Fatalf("global flags before command were not parsed as such; exit=%d", got)
	}
}
