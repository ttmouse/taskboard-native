package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"taskboard-native/internal/model"
	"taskboard-native/internal/service"
)

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
	want := []string{"list_projects", "list_tasks", "create_task", "update_task", "move_task", "add_comment", "list_comments"}
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
	if len(projects) != 1 || projects[0].ID != "local" {
		t.Fatalf("projects = %+v", projects)
	}

	// create_task by column name, identity from TASKBOARD_AGENT
	text, isErr = toolText(t, s.call(4, "tools/call", map[string]any{
		"name":      "create_task",
		"arguments": map[string]any{"projectId": "local", "title": "MCP 创建", "columnId": "进行中", "description": "来自 MCP"},
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
		"name": "list_tasks", "arguments": map[string]any{"projectId": "local"},
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

	// Unknown tool and unknown method surface as errors, not crashes.
	// An unknown tool is a tool failure (isError) with a readable message.
	if text, isErr := toolText(t, s.call(12, "tools/call", map[string]any{"name": "nope"})); !isErr || !strings.Contains(text, "unknown tool") {
		t.Fatalf("unknown tool = %q (isError %v)", text, isErr)
	}
	if resp := s.call(13, "nope", nil); resp["error"] == nil {
		t.Fatalf("unknown method returned no error: %v", resp)
	}
	if _, isErr := toolText(t, s.call(14, "tools/call", map[string]any{"name": "create_task", "arguments": map[string]any{"projectId": "missing", "title": "x"}})); !isErr {
		t.Fatalf("create into missing project succeeded")
	}
}

func timeoutAfter() <-chan time.Time { return time.After(20 * time.Second) }
