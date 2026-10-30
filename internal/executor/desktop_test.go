package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Real socket framing and authentication, without launching a model or touching
// the user's desktop. The handler controls Host state independently of the board.
func fakeDesktop(t *testing.T, handle func(string, map[string]any) any) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				scanner := bufio.NewScanner(conn)
				for scanner.Scan() {
					var req struct {
						ID     int            `json:"id"`
						Method string         `json:"method"`
						Params map[string]any `json:"params"`
					}
					if json.Unmarshal(scanner.Bytes(), &req) != nil {
						return
					}
					var result any
					if req.Method == "initialize" {
						if req.Params["authToken"] != "test-secret" || req.Params["sessionNamespace"] != "taskboard-native" {
							return
						}
						result = map[string]any{"features": map[string]any{"readOnly": false}}
					} else {
						result = handle(req.Method, req.Params)
					}
					if json.NewEncoder(conn).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}) != nil {
						return
					}
				}
			}()
		}
	}()
	t.Cleanup(func() { listener.Close(); wg.Wait() })
	endpoint := desktopEndpoint{Version: 1, Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, Token: "test-secret"}
	raw, _ := json.Marshal(endpoint)
	path := filepath.Join(t.TempDir(), "endpoint.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TASKBOARD_DSH_ENDPOINT", path)
}

func TestDesktopDispatchTracksRealTurnAndWritesSession(t *testing.T) {
	var mu sync.Mutex
	polls := 0
	var received map[string]any
	fakeDesktop(t, func(method string, params map[string]any) any {
		mu.Lock()
		defer mu.Unlock()
		switch method {
		case "session.createAndStart":
			received = params
			return map[string]any{"accepted": true, "externalSessionId": "desktop-session-123"}
		case "session.getState":
			polls++
			if polls == 1 {
				return map[string]any{"status": "blocked"}
			}
			return map[string]any{"status": "idle"}
		case "session.getSnapshot":
			// The first idle state is before the Host begins the turn.
			if polls == 2 {
				return map[string]any{"items": []any{}, "snapshotComplete": true}
			}
			return map[string]any{"snapshotComplete": true, "items": []any{
				map[string]any{"type": "message", "role": "assistant", "status": "done", "turnId": "t1", "content": map[string]any{"text": "desktop-ok"}},
				map[string]any{"type": "turn.end", "status": "done", "turnId": "t1"},
			}}
		}
		return nil
	})
	st := newTestStore(t)
	d := NewDispatcher(st, desktopTestConfig(t))
	defer d.Close()
	p, task := projectWithWorkspace(t, st)
	if err := d.Dispatch(task.ID, RuntimeDSH, "test"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "desktop returned", func() (bool, string) {
		e, _ := st.Execution(task.ID)
		return e != nil && e.Status == "returned", fmt.Sprint(e)
	})
	cur, _ := st.Task(task.ID)
	if cur.ColumnID != task.ColumnID || cur.Version != task.Version {
		t.Fatalf("program changed card: %+v", cur)
	}
	comments, _ := st.Comments(task.ID)
	all := fmt.Sprint(comments)
	if !strings.Contains(all, "desktop-session-123") || strings.Contains(all, "desktop-ok") {
		t.Fatal(all)
	}
	mu.Lock()
	defer mu.Unlock()
	if received["cwd"] != p.WorkspacePath || received["content"] != BuildTaskText(task) || polls < 3 {
		t.Fatalf("bad dispatch: %#v, polls=%d", received, polls)
	}
}

func TestDesktopUnavailableNeverFallsBackToCLI(t *testing.T) {
	t.Setenv("TASKBOARD_DSH_ENDPOINT", filepath.Join(t.TempDir(), "missing.json"))
	marker := filepath.Join(t.TempDir(), "unexpected-cli")
	t.Setenv("UNEXPECTED_DSH_MARKER", marker)
	installFakeRuntime(t, RuntimeDSH, `printf unexpected > "$UNEXPECTED_DSH_MARKER"`)
	st := newTestStore(t)
	d := NewDispatcher(st, desktopTestConfig(t))
	defer d.Close()
	p, task := projectWithWorkspace(t, st)
	if err := d.Dispatch(task.ID, RuntimeDSH, "test"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "desktop unavailable", func() (bool, string) {
		cs, _ := st.Comments(task.ID)
		all := fmt.Sprint(cs)
		return strings.Contains(all, "会话执行失败"), all
	})
	cur, _ := st.Task(task.ID)
	if cur.ColumnID != p.Columns[0].ID {
		t.Fatal("failed task did not return to backlog")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("desktop dispatch launched a CLI")
	}
}

func TestDesktopWaitingApprovalAndCloseDetach(t *testing.T) {
	fakeDesktop(t, func(method string, _ map[string]any) any {
		switch method {
		case "session.createAndStart":
			return map[string]any{"accepted": true, "externalSessionId": "waiting-session"}
		case "session.getState":
			return map[string]any{"status": "waiting_approval"}
		default:
			t.Errorf("unexpected call %s", method)
			return nil
		}
	})
	st := newTestStore(t)
	d := NewDispatcher(st, desktopTestConfig(t))
	defer d.Close()
	p, task := projectWithWorkspace(t, st)
	if err := d.Dispatch(task.ID, RuntimeDSH, "test"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "approval notice", func() (bool, string) {
		cs, _ := st.Comments(task.ID)
		s := fmt.Sprint(cs)
		return strings.Contains(s, "等待你的确认"), s
	})
	cur, _ := st.Task(task.ID)
	if cur.ColumnID != p.Columns[0].ID {
		t.Fatal("program claimed waiting task")
	}
	d.Close()
	cs, _ := st.Comments(task.ID)
	if !strings.Contains(fmt.Sprint(cs), "桌面会话继续保留") {
		t.Fatal(cs)
	}
}

// One desktop dispatch leaves one receipt. The generic "派发 …" ack belongs to
// CLI channels only: the receipt already names the session, so writing both put
// two near-identical messages on the card for every dispatch.
func TestDesktopDispatchWritesSingleComment(t *testing.T) {
	fakeDesktop(t, func(method string, _ map[string]any) any {
		switch method {
		case "session.createAndStart":
			return map[string]any{"accepted": true, "externalSessionId": "single-comment-session"}
		case "session.getState":
			return map[string]any{"status": "waiting_approval"}
		}
		return nil
	})
	st := newTestStore(t)
	d := NewDispatcher(st, desktopTestConfig(t))
	defer d.Close()
	_, task := projectWithWorkspace(t, st)
	if err := d.Dispatch(task.ID, RuntimeDSH, "test"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "desktop receipt", func() (bool, string) {
		cs, _ := st.Comments(task.ID)
		return strings.Contains(fmt.Sprint(cs), "single-comment-session"), fmt.Sprint(cs)
	})
	comments, _ := st.Comments(task.ID)
	receipts := 0
	for _, comment := range comments {
		if strings.HasPrefix(comment.Text, "派发 ") {
			t.Errorf("duplicate dispatch ack: %q", comment.Text)
		}
		if strings.Contains(comment.Text, "会话 ID：") {
			receipts++
		}
	}
	if receipts != 1 {
		t.Fatalf("receipts = %d, comments = %+v", receipts, comments)
	}
}

func TestDesktopCannotUseCLICommand(t *testing.T) {
	if _, err := runtimeArgs(t.TempDir(), "task", RuntimeDSH); err == nil {
		t.Fatal("DSH allowed CLI execution")
	}
}

func TestDesktopSessionEndDoesNotDecideTaskCompletion(t *testing.T) {
	for _, status := range []string{"failed", "interrupted", "done"} {
		t.Run(status, func(t *testing.T) {
			fakeDesktop(t, func(_ string, _ map[string]any) any {
				return map[string]any{"snapshotComplete": true, "items": []any{map[string]any{"type": "turn.end", "status": status}}}
			})
			b, err := connectDesktop(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			_, done, err := b.result(context.Background(), "test")
			if status == "done" {
				if err != nil || !done {
					t.Fatalf("a session can return without text: done=%v err=%v", done, err)
				}
			} else if err == nil || done {
				t.Fatalf("done=%v err=%v", done, err)
			}
		})
	}
}

// Explicit opt-in end-to-end check: dispatch through the real desktop Host and
// count what the card actually receives.
func TestDesktopLiveSingleComment(t *testing.T) {
	if os.Getenv("TASKBOARD_TEST_DSH_LIVE") != "1" {
		t.Skip("requires running DSH desktop")
	}
	st := newTestStore(t)
	d := NewDispatcher(st, desktopTestConfig(t))
	defer d.Close()
	p, task := projectWithWorkspace(t, st)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.runWithText(ctx, task, p.Name, p.WorkspacePath, RuntimeDSH, "live",
		"这是看板与 DSH 桌面端的连接测试。不要调用工具，不要修改文件。只回复：TASKBOARD_DESKTOP_OK", "")
	waitFor(t, "desktop receipt", func() (bool, string) {
		cs, _ := st.Comments(task.ID)
		return strings.Contains(fmt.Sprint(cs), "会话 ID："), fmt.Sprint(cs)
	})
	cs, _ := st.Comments(task.ID)
	acks, receipts := 0, 0
	for _, comment := range cs {
		if strings.HasPrefix(comment.Text, "派发 ") {
			acks++
		}
		if strings.Contains(comment.Text, "会话 ID：") {
			receipts++
		}
	}
	if acks != 0 || receipts != 1 {
		t.Fatalf("acks=%d receipts=%d comments=%+v", acks, receipts, cs)
	}
	t.Logf("card comment: %s", cs[0].Text)
}

// Explicit opt-in smoke test: one harmless session in the actual desktop Host.
func TestDesktopLive(t *testing.T) {
	if os.Getenv("TASKBOARD_TEST_DSH_LIVE") != "1" {
		t.Skip("requires running DSH desktop")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	b, err := connectDesktop(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	selections, err := desktopSelections(desktopTestConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("taskboard-smoke-%x", randomDesktopID())
	var reply struct {
		Accepted          bool   `json:"accepted"`
		ExternalSessionID string `json:"externalSessionId"`
		Message           string `json:"message"`
	}
	err = b.call(ctx, "session.createAndStart", map[string]any{"sessionId": id, "clientMessageId": id, "cwd": t.TempDir(), "content": "这是看板与 DSH 桌面端的连接测试。不要调用工具，不要修改文件。只回复：TASKBOARD_DESKTOP_OK", "agentPreset": "standard", "selections": selections}, &reply)
	if err != nil || !reply.Accepted {
		t.Fatalf("%v %#v", err, reply)
	}
	t.Logf("desktop session: %s", reply.ExternalSessionID)
	for ctx.Err() == nil {
		var state struct {
			Status string `json:"status"`
		}
		if err := b.call(ctx, "session.getState", map[string]any{"sessionId": id}, &state); err != nil {
			t.Fatal(err)
		}
		if state.Status == "idle" {
			answer, done, err := b.result(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if done {
				if !strings.Contains(answer, "TASKBOARD_DESKTOP_OK") {
					t.Fatal(answer)
				}
				return
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
	t.Fatal(ctx.Err())
}

func desktopTestConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runtimes.json")
	if err := os.WriteFile(path, []byte(`{"dsh":{"provider":"magpie-responses","model":"grok/grok-4.7"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
