package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"taskboard-native/internal/client"
	"taskboard-native/internal/model"
	"taskboard-native/internal/service"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// buildBinary compiles the MCP server once per test.
func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "taskboard-mcp")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

// mcpSession pipes newline-delimited JSON-RPC into a real MCP process.
type mcpSession struct {
	t      *testing.T
	cmd    *exec.Cmd
	stdin  *os.File
	writer *bufio.Writer
	lines  chan map[string]any
	stderr *bytes.Buffer
}

func startMCP(t *testing.T, env []string) *mcpSession {
	t.Helper()
	bin := buildBinary(t)
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), env...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout: %v", err)
	}
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	s := &mcpSession{
		t:      t,
		cmd:    cmd,
		writer: bufio.NewWriter(stdin),
		lines:  make(chan map[string]any, 16),
		stderr: stderr,
	}
	go func() {
		defer close(s.lines)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var msg map[string]any
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				s.t.Errorf("non-JSON line %q: %v", line, err)
				continue
			}
			s.lines <- msg
		}
	}()
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	})
	return s
}

// call sends one request and returns its response.
func (s *mcpSession) call(id int, method string, params any) map[string]any {
	s.t.Helper()
	msg := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		s.t.Fatalf("marshal: %v", err)
	}
	if _, err := s.writer.Write(append(raw, '\n')); err != nil {
		s.t.Fatalf("write: %v", err)
	}
	if err := s.writer.Flush(); err != nil {
		s.t.Fatalf("flush: %v", err)
	}
	select {
	case resp, ok := <-s.lines:
		if !ok {
			s.t.Fatalf("%s: server closed stdout (stderr: %s)", method, s.stderr)
		}
		if got := resp["id"]; got != float64(id) {
			s.t.Fatalf("%s: response id = %v, want %d (%v)", method, got, id, resp)
		}
		return resp
	case <-timeoutAfter():
		s.t.Fatalf("%s: no response within 20s (stderr: %s)", method, s.stderr)
		return nil
	}
}

func (s *mcpSession) notify(method string, params any) {
	s.t.Helper()
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		msg["params"] = params
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		s.t.Fatalf("marshal: %v", err)
	}
	if _, err := s.writer.Write(append(raw, '\n')); err != nil {
		s.t.Fatalf("write: %v", err)
	}
	if err := s.writer.Flush(); err != nil {
		s.t.Fatalf("flush: %v", err)
	}
}

// result decodes the "result" object of a response.
func result(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("unexpected error: %v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("result = %v", resp["result"])
	}
	return res
}

// toolText extracts the single text content block of a tools/call result.
func toolText(t *testing.T, resp map[string]any) (string, bool) {
	t.Helper()
	res := result(t, resp)
	if isErr, _ := res["isError"].(bool); isErr {
		content, _ := res["content"].([]any)
		if len(content) == 0 {
			t.Fatalf("tool error without content: %v", res)
		}
		block, _ := content[0].(map[string]any)
		return block["text"].(string), true
	}
	content, ok := res["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("content = %v", res["content"])
	}
	block, ok := content[0].(map[string]any)
	if !ok || block["type"] != "text" {
		t.Fatalf("content block = %v", content[0])
	}
	text, ok := block["text"].(string)
	if !ok {
		t.Fatalf("content text = %v", block["text"])
	}
	return text, false
}

func TestMCPEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary")
	}
	srv, err := service.Start(":0", filepath.Join(t.TempDir(), "taskboard.db"))
	if err != nil {
		t.Fatalf("start service: %v", err)
	}
	t.Cleanup(func() {
		if err := srv.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	})

	// A newly opened service starts projectless; this scenario expects the
	// legacy「local」board with its three demo cards, so seed it via the API.
	fc := client.New(&client.Discover{Addr: srv.Addr}, "test-agent")
	proj, err := fc.CreateProject("local")
	if err != nil {
		t.Fatalf("fixture project: %v", err)
	}
	for i, title := range []string{"欢迎使用 taskboard", "接上 CLI 与 MCP", "检查实时推送"} {
		if _, err := fc.CreateTask(proj.ID, proj.Columns[i].ID, title, "", ""); err != nil {
			t.Fatalf("fixture task %d: %v", i, err)
		}
	}

	s := startMCP(t, []string{"TASKBOARD_ADDR=" + srv.Addr, "TASKBOARD_AGENT=mcp-e2e"})

	// initialize
	res := result(t, s.call(1, "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test", "version": "0"},
	}))
	if res["protocolVersion"] != "2025-06-18" {
		t.Fatalf("protocolVersion = %v", res["protocolVersion"])
	}
	info, _ := res["serverInfo"].(map[string]any)
	if info["name"] != "taskboard-native" {
		t.Fatalf("serverInfo = %v", info)
	}
	if caps, ok := res["capabilities"].(map[string]any); !ok || caps["tools"] == nil {
		t.Fatalf("capabilities = %v", res["capabilities"])
	}
	s.notify("notifications/initialized", nil)

	// tools/list
	res = result(t, s.call(2, "tools/list", nil))
	tools, _ := res["tools"].([]any)
	names := map[string]bool{}
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		name, _ := tool["name"].(string)
		names[name] = true
		if schema, ok := tool["inputSchema"].(map[string]any); !ok || schema["type"] != "object" {
			t.Fatalf("tool %s schema = %v", name, tool["inputSchema"])
		}
	}
	want := []string{"list_projects", "create_project", "list_tasks", "get_task", "create_task", "update_task", "move_task", "add_comment", "list_comments"}
	if len(tools) != len(want) {
		t.Fatalf("tools = %v, want %d", names, len(want))
	}
	for _, name := range want {
		if !names[name] {
			t.Fatalf("missing tool %s (have %v)", name, names)
		}
	}

	// list_projects
	text, isErr := toolText(t, s.call(3, "tools/call", map[string]any{"name": "list_projects", "arguments": map[string]any{}}))
	if isErr {
		t.Fatalf("list_projects failed: %s", text)
	}
	var projects []model.Project
	if err := json.Unmarshal([]byte(text), &projects); err != nil {
		t.Fatalf("decode projects %q: %v", text, err)
	}
	if len(projects) != 1 || projects[0].ID != proj.ID {
		t.Fatalf("projects = %+v", projects)
	}

	// create_task by column name, identity from TASKBOARD_AGENT
	text, isErr = toolText(t, s.call(4, "tools/call", map[string]any{
		"name":      "create_task",
		"arguments": map[string]any{"projectId": proj.ID, "title": "MCP 创建", "columnId": "进行中", "description": "来自 MCP"},
	}))
	if isErr {
		t.Fatalf("create_task failed: %s", text)
	}
	var created model.Task
	if err := json.Unmarshal([]byte(text), &created); err != nil {
		t.Fatalf("decode task %q: %v", text, err)
	}
	if created.Title != "MCP 创建" || created.Version != 1 || created.UpdatedBy != "mcp-e2e" {
		t.Fatalf("created = %+v", created)
	}
	if created.ColumnID != projects[0].Columns[1].ID {
		t.Fatalf("created column = %s, want 进行中", created.ColumnID)
	}

	// list_tasks requires projectId and returns the new card
	text, isErr = toolText(t, s.call(5, "tools/call", map[string]any{
		"name": "list_tasks", "arguments": map[string]any{"projectId": proj.ID},
	}))
	if isErr {
		t.Fatalf("list_tasks failed: %s", text)
	}
	var tasks []model.Task
	if err := json.Unmarshal([]byte(text), &tasks); err != nil {
		t.Fatalf("decode tasks: %v", err)
	}
	if len(tasks) != 4 {
		t.Fatalf("tasks = %d, want 4 (3 seeded + 1 new)", len(tasks))
	}
	if text, isErr = toolText(t, s.call(6, "tools/call", map[string]any{"name": "list_tasks", "arguments": map[string]any{}})); !isErr {
		t.Fatalf("list_tasks without projectId succeeded: %s", text)
	}

	// update_task (optimistic concurrency)
	text, isErr = toolText(t, s.call(7, "tools/call", map[string]any{
		"name":      "update_task",
		"arguments": map[string]any{"id": created.ID, "version": created.Version, "title": "MCP 改名"},
	}))
	if isErr {
		t.Fatalf("update_task failed: %s", text)
	}
	var updated model.Task
	if err := json.Unmarshal([]byte(text), &updated); err != nil {
		t.Fatalf("decode updated: %v", err)
	}
	if updated.Title != "MCP 改名" || updated.Version != 2 {
		t.Fatalf("updated = %+v", updated)
	}
	text, isErr = toolText(t, s.call(8, "tools/call", map[string]any{
		"name":      "update_task",
		"arguments": map[string]any{"id": created.ID, "version": created.Version, "title": "过期"},
	}))
	if !isErr || !strings.Contains(text, "version_conflict") || !strings.Contains(text, "v2") {
		t.Fatalf("stale update = %q (isError %v), want version_conflict with v2", text, isErr)
	}

	// move_task by column name
	text, isErr = toolText(t, s.call(9, "tools/call", map[string]any{
		"name":      "move_task",
		"arguments": map[string]any{"id": created.ID, "version": updated.Version, "columnId": "完成"},
	}))
	if isErr {
		t.Fatalf("move_task failed: %s", text)
	}
	var moved model.Task
	if err := json.Unmarshal([]byte(text), &moved); err != nil {
		t.Fatalf("decode moved: %v", err)
	}
	if moved.ColumnID != projects[0].Columns[3].ID || moved.Version != 3 {
		t.Fatalf("moved = %+v", moved)
	}

	// add_comment + list_comments
	text, isErr = toolText(t, s.call(10, "tools/call", map[string]any{
		"name": "add_comment", "arguments": map[string]any{"taskId": created.ID, "text": "MCP 评论"},
	}))
	if isErr {
		t.Fatalf("add_comment failed: %s", text)
	}
	var comment model.Comment
	if err := json.Unmarshal([]byte(text), &comment); err != nil {
		t.Fatalf("decode comment: %v", err)
	}
	if comment.Author != "mcp-e2e" || comment.Text != "MCP 评论" {
		t.Fatalf("comment = %+v", comment)
	}
	text, isErr = toolText(t, s.call(11, "tools/call", map[string]any{
		"name": "list_comments", "arguments": map[string]any{"taskId": created.ID},
	}))
	if isErr {
		t.Fatalf("list_comments failed: %s", text)
	}
	var comments []model.Comment
	if err := json.Unmarshal([]byte(text), &comments); err != nil {
		t.Fatalf("decode comments: %v", err)
	}
	if len(comments) != 1 || comments[0].ID != comment.ID {
		t.Fatalf("comments = %+v", comments)
	}
	text, isErr = toolText(t, s.call(12, "tools/call", map[string]any{
		"name": "get_task", "arguments": map[string]any{"id": created.ID},
	}))
	if isErr {
		t.Fatalf("get_task failed: %s", text)
	}
	var detailed model.Task
	if err := json.Unmarshal([]byte(text), &detailed); err != nil {
		t.Fatalf("decode task details: %v", err)
	}
	if detailed.Description != "来自 MCP" || len(detailed.Comments) != 1 || detailed.Comments[0].ID != comment.ID {
		t.Fatalf("task details = %+v, want description and comment preserved", detailed)
	}

	// Unknown tool and unknown method surface as errors, not crashes.
	// An unknown tool is a tool failure (isError) with a readable message.
	if text, isErr := toolText(t, s.call(13, "tools/call", map[string]any{"name": "nope"})); !isErr || !strings.Contains(text, "unknown tool") {
		t.Fatalf("unknown tool = %q (isError %v)", text, isErr)
	}
	if resp := s.call(14, "nope", nil); resp["error"] == nil {
		t.Fatalf("unknown method returned no error: %v", resp)
	}
	if _, isErr := toolText(t, s.call(15, "tools/call", map[string]any{"name": "create_task", "arguments": map[string]any{"projectId": "missing", "title": "x"}})); !isErr {
		t.Fatalf("create into missing project succeeded")
	}
}

func TestMCPToolSchemaAndHTTPForwarding(t *testing.T) {
	definitions := toolDefinitions()
	byName := make(map[string]map[string]any, len(definitions))
	for _, definition := range definitions {
		byName[definition["name"].(string)] = definition
	}
	listSchema := byName["list_tasks"]["inputSchema"].(map[string]any)
	listProperties := listSchema["properties"].(map[string]any)
	for _, key := range []string{"projectId", "columnId", "assignee", "limit", "offset", "summary", "includeArchived"} {
		if _, ok := listProperties[key]; !ok {
			t.Errorf("list_tasks schema missing %s", key)
		}
	}
	if got := listSchema["required"].([]string); len(got) != 1 || got[0] != "projectId" {
		t.Errorf("list_tasks required = %v, want projectId only", got)
	}
	if limit := listProperties["limit"].(map[string]any); limit["minimum"] != 1 || limit["maximum"] != 200 {
		t.Errorf("list_tasks limit schema = %v, want range 1..200", limit)
	}
	for _, tc := range []struct {
		name    string
		field   string
		wantReq string
	}{
		{name: "get_task", field: "id", wantReq: "id"},
		{name: "create_project", field: "name", wantReq: "name"},
	} {
		schema := byName[tc.name]["inputSchema"].(map[string]any)
		if _, ok := schema["properties"].(map[string]any)[tc.field]; !ok {
			t.Errorf("%s schema missing %s", tc.name, tc.field)
		}
		required := schema["required"].([]string)
		if len(required) != 1 || required[0] != tc.wantReq {
			t.Errorf("%s required = %v, want %s", tc.name, required, tc.wantReq)
		}
	}
	if _, ok := byName["create_project"]["inputSchema"].(map[string]any)["properties"].(map[string]any)["workspacePath"]; !ok {
		t.Error("create_project schema missing workspacePath")
	}
	updateSchema := byName["update_task"]["inputSchema"].(map[string]any)
	if required := updateSchema["required"].([]string); len(required) != 2 || required[0] != "id" || required[1] != "version" {
		t.Errorf("update_task required = %v, want id and version", required)
	}

	const detailBody = `{"id":"task-1","projectId":"project-1","columnId":"col-1","title":"A","description":"正文\n保留原样","version":3,"comments":[{"id":"comment-1","author":"mcp","text":"评论原样"}]}`
	var listQueries []url.Values
	var gotCreate map[string]any
	var sawCreate, sawDetail bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"project-1","workspacePath":"/tmp/project"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/projects":
			sawCreate = true
			if err := json.NewDecoder(r.Body).Decode(&gotCreate); err != nil {
				t.Errorf("decode create_project body: %v", err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"project-1","workspacePath":"/tmp/project"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/tasks":
			listQueries = append(listQueries, r.URL.Query())
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/tasks/task-1":
			sawDetail = true
			_, _ = w.Write([]byte(detailBody))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	})
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder.Result(), nil
	})}
	s := &server{agent: "mcp-test", base: "http://taskboard.test", http: httpClient}

	projects, err := s.callTool("list_projects", nil)
	if err != nil || projects != `[{"id":"project-1","workspacePath":"/tmp/project"}]` {
		t.Fatalf("list_projects = %q, %v", projects, err)
	}
	_, err = s.callTool("create_project", json.RawMessage(`{"name":"No path"}`))
	if err != nil || gotCreate["name"] != "No path" {
		t.Fatalf("create_project without path body = %v, err = %v", gotCreate, err)
	}
	if _, ok := gotCreate["workspacePath"]; ok {
		t.Fatalf("create_project without path sent workspacePath: %v", gotCreate)
	}
	created, err := s.callTool("create_project", json.RawMessage(`{"name":"Work","workspacePath":"/does/not/exist"}`))
	if err != nil || !sawCreate || gotCreate["name"] != "Work" || gotCreate["workspacePath"] != "/does/not/exist" {
		t.Fatalf("create_project body = %v, response = %q, err = %v", gotCreate, created, err)
	}
	if !strings.Contains(created, `"workspacePath":"/tmp/project"`) {
		t.Errorf("create_project response lost workspacePath: %q", created)
	}
	_, err = s.callTool("list_tasks", json.RawMessage(`{"projectId":"project 1","columnId":"col/1","assignee":"Ada Lovelace","limit":20,"offset":5,"summary":true}`))
	if err != nil {
		t.Fatalf("list_tasks: %v", err)
	}
	if len(listQueries) != 1 {
		t.Fatalf("list_tasks requests = %d, want 1", len(listQueries))
	}
	for key, want := range map[string]string{
		"projectId": "project 1", "columnId": "col/1", "assignee": "Ada Lovelace",
		"limit": "20", "offset": "5", "summary": "true",
	} {
		if got := listQueries[0].Get(key); got != want {
			t.Errorf("list_tasks %s = %q, want %q", key, got, want)
		}
	}
	_, err = s.callTool("list_tasks", json.RawMessage(`{"projectId":"local","includeArchived":true}`))
	if len(listQueries) != 2 {
		t.Fatalf("list_tasks requests = %d, want 2", len(listQueries))
	}
	if err != nil || listQueries[1].Get("includeArchived") != "true" || listQueries[1].Get("projectId") != "local" {
		t.Errorf("legacy includeArchived call: query=%v, err=%v", listQueries[1], err)
	}
	gotDetail, err := s.callTool("get_task", json.RawMessage(`{"id":"task-1"}`))
	if err != nil || gotDetail != detailBody || !sawDetail {
		t.Fatalf("get_task = %q, err=%v, saw request=%v", gotDetail, err, sawDetail)
	}
	if _, err := s.callTool("list_tasks", json.RawMessage(`{"projectId":"local","limit":"20"}`)); err == nil {
		t.Fatal("list_tasks accepted a non-integer limit")
	}
}

func timeoutAfter() <-chan time.Time { return time.After(20 * time.Second) }
