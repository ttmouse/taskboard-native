package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"taskboard-native/internal/client"
	"taskboard-native/internal/executor"
	"taskboard-native/internal/model"
)

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

func TestDispatchHTTPChannels(t *testing.T) {
	for _, runtime := range []string{"", executor.RuntimeDSH, executor.RuntimeCodex, executor.RuntimePi} {
		t.Run("runtime="+runtime, func(t *testing.T) {
			selected := runtime
			if selected == "" {
				selected = executor.RuntimeDSH
			}
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, selected), []byte("#!/bin/sh\nprintf 'HTTP_CHANNEL_OK\\n'\n"), 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			if selected == executor.RuntimeDSH {
				// DSH has no CLI fallback. Never touch the real desktop in unit tests.
				t.Setenv("TASKBOARD_DSH_ENDPOINT", filepath.Join(t.TempDir(), "missing.json"))
			}
			srv := start(t)
			c := client.New(&client.Discover{Addr: srv.Addr}, "channel-test")
			p, err := c.CreateProjectWithPath("执行项目", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if selected != executor.RuntimeDSH {
				installFakeHerdr(t, p.WorkspacePath, "done")
			}
			task, err := c.CreateTask(p.ID, p.Columns[0].ID, "渠道验收", "仅回复固定标记", " ")
			if err != nil {
				t.Fatal(err)
			}
			status, raw := request(t, srv, "POST", "/api/tasks/"+task.ID+"/dispatch", "channel-test", map[string]string{"runtime": runtime})
			if status != 202 {
				t.Fatalf("dispatch=%d %s", status, raw)
			}
			var accepted map[string]string
			if err := json.Unmarshal(raw, &accepted); err != nil || accepted["runtime"] != selected {
				t.Fatalf("response=%s err=%v", raw, err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				current, err := c.Task(task.ID)
				if selected == executor.RuntimeDSH && err == nil && current.ColumnID == p.Columns[0].ID && len(current.Comments) >= 1 {
					// The desktop path leaves one comment only: the failure notice.
					if strings.Contains(current.Comments[len(current.Comments)-1].Text, "会话执行失败") {
						return
					}
				}
				if selected != executor.RuntimeDSH && err == nil && current.Execution != nil && current.Execution.Status == "returned" {
					doing, _ := p.ColumnByRole(model.ColumnRoleDoing)
					if current.ColumnID != doing.ID || !strings.HasPrefix(current.Execution.SessionID, "herdr:") {
						t.Fatalf("result=%+v", current)
					}
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("HTTP dispatch did not write its result and move to review")
		})
	}
}

func TestDispatchHTTPRejectsUnknownChannel(t *testing.T) {
	srv := start(t)
	status, raw := request(t, srv, "POST", "/api/tasks/missing/dispatch", "channel-test", map[string]string{"runtime": "unregistered"})
	if status != 400 || !strings.Contains(string(raw), "unknown runtime") {
		t.Fatalf("%d %s", status, raw)
	}
}

// One call, two modes: with dispatchRuntime the create hands the new card
// straight to that channel; without it the card is only created.
func TestCreateTaskWithDispatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte("#!/bin/sh\nprintf 'CREATE_DISPATCH_OK\\n'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	srv := start(t)
	c := client.New(&client.Discover{Addr: srv.Addr}, "create-dispatch-test")
	p, err := c.CreateProjectWithPath("一步建卡", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	installFakeHerdr(t, p.WorkspacePath, "done")
	status, raw := request(t, srv, "POST", "/api/tasks", "create-dispatch-test", map[string]any{
		"projectId": p.ID, "columnId": p.Columns[0].ID, "title": "建卡即派发", "dispatchRuntime": executor.RuntimeCodex,
	})
	if status != 201 {
		t.Fatalf("create+dispatch=%d %s", status, raw)
	}
	task, err := c.Task(decode[model.Task](t, raw).ID)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		current, err := c.Task(task.ID)
		if err == nil && current.Execution != nil && current.Execution.Status == "returned" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	current, err := c.Task(task.ID)
	if err != nil || current.Execution == nil || current.Execution.Status != "returned" || !strings.HasPrefix(current.Execution.SessionID, "herdr:") {
		t.Fatalf("create+dispatch Herdr run did not return on the card: %+v %v", current, err)
	}

	// An unknown channel is rejected before the card exists.
	count := projectTaskCount(t, srv, p.ID)
	status, raw = request(t, srv, "POST", "/api/tasks", "create-dispatch-test", map[string]any{
		"projectId": p.ID, "title": "无效渠道", "dispatchRuntime": "unregistered",
	})
	if status != 400 || !strings.Contains(string(raw), "unknown runtime") {
		t.Fatalf("unknown runtime: %d %s", status, raw)
	}
	// A project without a workspacePath is rejected before the card exists.
	status, raw = request(t, srv, "POST", "/api/tasks", "create-dispatch-test", map[string]any{
		"projectId": fixtureProjectID(t, srv), "title": "无工作目录", "dispatchRuntime": executor.RuntimeCodex,
	})
	if status != 400 || !strings.Contains(string(raw), "workspacePath") {
		t.Fatalf("no workspace: %d %s", status, raw)
	}
	if after := projectTaskCount(t, srv, p.ID); after != count {
		t.Fatalf("rejected creates left cards behind: %d -> %d", count, after)
	}
	// Plain create stays create-only.
	status, raw = request(t, srv, "POST", "/api/tasks", "create-dispatch-test", map[string]any{
		"projectId": p.ID, "title": "仅建卡",
	})
	if status != 201 {
		t.Fatalf("create-only=%d %s", status, raw)
	}
	plain := decode[model.Task](t, raw)
	if got := projectTaskCount(t, srv, p.ID); got != count+1 {
		t.Fatalf("create-only count=%d want %d", got, count+1)
	}
	if plain, err := c.Task(plain.ID); err != nil || len(plain.Comments) != 0 {
		t.Fatalf("create-only card has comments: %+v %v", plain, err)
	}
}

func projectTaskCount(t *testing.T, srv *Server, projectID string) int {
	t.Helper()
	status, raw := request(t, srv, "GET", "/api/tasks?projectId="+projectID, "create-dispatch-test", nil)
	if status != 200 {
		t.Fatalf("tasks=%d %s", status, raw)
	}
	return len(decode[[]model.Task](t, raw))
}

// Opt-in smoke test: exercises a real installed CLI in an isolated workspace
// and database, and verifies a new artifact as well as the result comment.
func TestLiveChannelDispatch(t *testing.T) {
	runtime := os.Getenv("TASKBOARD_LIVE_RUNTIME")
	if runtime == "" {
		t.Skip("set TASKBOARD_LIVE_RUNTIME to opt into a real CLI run")
	}
	srv := start(t)
	c := client.New(&client.Discover{Addr: srv.Addr}, "channel-smoke-test")
	if path := os.Getenv("TASKBOARD_LIVE_RUNTIME_CONFIG"); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(srv.DBPath+".runtimes.json", raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	workspace := t.TempDir()
	artifact := filepath.Join(workspace, "channel-proof.txt")
	const marker = "TASKBOARD_CHANNEL_ARTIFACT_OK"
	p, err := c.CreateProjectWithPath("channel-smoke", workspace)
	if err != nil {
		t.Fatal(err)
	}
	prompt := "Only create the file " + artifact + " containing exactly " + marker + ". Do not run commands, read other files, change other files, or create subagents. Use the file-writing tool. Finally reply with " + marker + "."
	task, err := c.CreateTask(p.ID, p.Columns[0].ID, "Channel artifact smoke test", prompt, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Dispatch(task.ID, runtime); err != nil {
		t.Fatal(err)
	}
	// A real agent run reads the skill and the card before writing anything;
	// Claude Code needs ~3 minutes for that, so the window must fit the slowest
	// channel rather than the fastest.
	deadline := time.Now().Add(240 * time.Second)
	for time.Now().Before(deadline) {
		current, err := c.Task(task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.ColumnID == p.Columns[2].ID {
			data, err := os.ReadFile(artifact)
			if err != nil || strings.TrimSpace(string(data)) != marker {
				t.Fatalf("artifact=%q error=%v", data, err)
			}
			if len(current.Comments) < 2 || !strings.Contains(current.Comments[len(current.Comments)-1].Text, marker) {
				t.Fatalf("no final marker: %+v", current.Comments)
			}
			t.Logf("%s: new artifact, result comment, and review state verified", runtime)
			return
		}
		if len(current.Comments) > 1 {
			last := current.Comments[len(current.Comments)-1].Text
			if strings.Contains(last, "跟踪失败") || strings.Contains(last, "未确认接收") || strings.Contains(last, "执行失败") || strings.Contains(last, "未返回结果") || strings.Contains(last, "被取消") || strings.Contains(last, "派发 "+runtime+" 失败") {
				t.Fatalf("CLI did not complete: %+v", current.Comments)
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("real channel did not finish within 60 seconds")
}
