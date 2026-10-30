package service

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"taskboard-native/internal/client"
	"taskboard-native/internal/model"
)

// This relay changes only the isolated board's connection. The actual desktop
// Host, model and tool execution remain real and other sessions are untouched.
type desktopFaultRelay struct {
	listener   net.Listener
	host       string
	token      string
	port       int
	reject     atomic.Bool
	disconnect atomic.Bool
	loseAck    atomic.Bool
	creates    atomic.Int32
	wg         sync.WaitGroup
}

func liveDesktopRelay(t *testing.T, root string) *desktopFaultRelay {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".agents-anywhere", "dsh-bridge", "endpoint.json"))
	if err != nil {
		t.Fatal(err)
	}
	var endpoint struct {
		Host  string `json:"host"`
		Port  int    `json:"port"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &endpoint); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r := &desktopFaultRelay{listener: ln, host: net.JoinHostPort(endpoint.Host, fmt.Sprint(endpoint.Port)), token: endpoint.Token, port: ln.Addr().(*net.TCPAddr).Port}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			r.wg.Add(1)
			go func() { defer r.wg.Done(); r.serve(conn) }()
		}
	}()
	t.Cleanup(func() { ln.Close(); r.wg.Wait() })
	r.writeEndpoint(t, root, endpoint.Token)
	return r
}

func (r *desktopFaultRelay) writeEndpoint(t *testing.T, root, token string) {
	t.Helper()
	path := filepath.Join(root, "endpoint.json")
	raw, _ := json.Marshal(map[string]any{"version": 1, "host": "127.0.0.1", "port": r.port, "token": token})
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TASKBOARD_DSH_ENDPOINT", path)
}

func (r *desktopFaultRelay) serve(down net.Conn) {
	defer down.Close()
	up, err := net.DialTimeout("tcp", r.host, 5*time.Second)
	if err != nil {
		return
	}
	defer up.Close()
	requests := bufio.NewScanner(down)
	requests.Buffer(make([]byte, 4096), 8*1024*1024)
	responses := bufio.NewScanner(up)
	responses.Buffer(make([]byte, 4096), 8*1024*1024)
	for requests.Scan() {
		var req struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(requests.Bytes(), &req) != nil {
			return
		}
		if req.Method == "session.createAndStart" && r.reject.Load() {
			json.NewEncoder(down).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"ok": false, "message": "AUTH 403: injected upstream authorization refusal"}})
			continue
		}
		if req.Method == "session.getState" && r.disconnect.Load() {
			return
		}
		if req.Method == "session.createAndStart" {
			r.creates.Add(1)
		}
		up.SetDeadline(time.Now().Add(65 * time.Second))
		if _, err := fmt.Fprintln(up, string(requests.Bytes())); err != nil {
			return
		}
		if !responses.Scan() {
			return
		}
		if req.Method == "session.createAndStart" && r.loseAck.Swap(false) {
			return
		}
		if _, err := fmt.Fprintln(down, string(responses.Bytes())); err != nil {
			return
		}
	}
}

func awaitDesktopTask(t *testing.T, c *client.Client, id string, timeout time.Duration, condition func(model.Task) bool) model.Task {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last model.Task
	for time.Now().Before(deadline) {
		cur, err := c.Task(id)
		if err != nil {
			t.Fatal(err)
		}
		last = cur
		if condition(cur) {
			return cur
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("desktop flow timed out: %+v", last)
	return last
}

// Real desktop + real model + real artifact, with precise failure injection at
// the transport boundary. Does not alter real credentials or stop the desktop.
func TestLiveDesktopRecoveryFlow(t *testing.T) {
	if os.Getenv("TASKBOARD_TEST_DESKTOP_RECOVERY") != "1" {
		t.Skip("opt-in real desktop recovery acceptance")
	}
	root := os.Getenv("TASKBOARD_RECOVERY_DIR")
	if root == "" {
		root = t.TempDir()
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	relay := liveDesktopRelay(t, root)
	db := filepath.Join(root, "board.db")
	config, err := os.ReadFile(os.Getenv("TASKBOARD_LIVE_RUNTIME_CONFIG"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(db+".runtimes.json", config, 0600); err != nil {
		t.Fatal(err)
	}
	srv, err := Start("127.0.0.1:0", db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	c := client.New(&client.Discover{Addr: srv.Addr}, "recovery-acceptance")
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	p, err := c.CreateProjectWithPath("桌面链路隔离验收", workspace)
	if err != nil {
		t.Fatal(err)
	}
	proof := filepath.Join(workspace, "proof.txt")
	prompt := fmt.Sprintf("This is an isolated dispatch recovery test. For the file action, execute exactly this command once using bash: printf 'START\\n' >> '%s'; sleep 12; printf 'DONE\\n' >> '%s'. Do not read or modify other files, do not create subagents, and do not repeat the command. Write your result and verification with RECOVERY_FLOW_OK to the original card, move it to review, GET to confirm, then reply.", proof, proof)
	task, err := c.CreateTask(p.ID, p.Columns[0].ID, "故障恢复与防重复验收", prompt, "")
	if err != nil {
		t.Fatal(err)
	}
	var evidence []map[string]any
	record := func(step string, cur model.Task) {
		entry := map[string]any{"step": step, "taskId": cur.ID, "columnId": cur.ColumnID, "execution": cur.Execution, "time": time.Now().UTC().Format(time.RFC3339)}
		evidence = append(evidence, entry)
		t.Logf("PASS %s: %+v", step, cur.Execution)
		raw, _ := json.MarshalIndent(evidence, "", "  ")
		if err := os.WriteFile(filepath.Join(root, "evidence.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}

	// Real bridge authentication refusal: wrong token only in our endpoint.
	relay.writeEndpoint(t, root, "invalid-test-token")
	if err := c.Dispatch(task.ID, "dsh"); err != nil {
		t.Fatal(err)
	}
	cur := awaitDesktopTask(t, c, task.ID, 10*time.Second, func(cur model.Task) bool {
		return len(cur.Comments) >= 2 && strings.Contains(cur.Comments[len(cur.Comments)-1].Text, "执行失败") && cur.ColumnID == p.Columns[0].ID
	})
	if relay.creates.Load() != 0 {
		t.Fatal("unauthorized request reached session creation")
	}
	record("real_bridge_auth_rejected_task_untouched", cur)
	relay.writeEndpoint(t, root, relay.token)

	// Upstream-style authorization failure is a deliberate injected response.
	relay.reject.Store(true)
	if err := c.Dispatch(task.ID, "dsh"); err != nil {
		t.Fatal(err)
	}
	cur = awaitDesktopTask(t, c, task.ID, 10*time.Second, func(cur model.Task) bool {
		return cur.Execution != nil && cur.Execution.Status == "failed" && cur.ColumnID == p.Columns[0].ID
	})
	if relay.creates.Load() != 0 {
		t.Fatal("rejected dispatch reached desktop")
	}
	record("injected_upstream_403_terminal_failure", cur)
	relay.reject.Store(false)

	// Retry the failed card. Drop tracking after the actual Host accepts it.
	relay.disconnect.Store(true)
	if err := c.Dispatch(task.ID, "dsh"); err != nil {
		t.Fatal(err)
	}
	status, _ := request(t, srv, "POST", "/api/tasks/"+task.ID+"/dispatch", "recovery-test", map[string]string{"runtime": "dsh"})
	if status != 409 {
		t.Fatalf("duplicate dispatch=%d, want 409", status)
	}
	cur = awaitDesktopTask(t, c, task.ID, 15*time.Second, func(cur model.Task) bool { return cur.Execution != nil && cur.Execution.Status == "disconnected" })
	if cur.ColumnID == p.Columns[2].ID {
		t.Fatal("lost connection marked success")
	}
	original := cur.Execution.SessionID
	record("accepted_then_transport_disconnected_duplicate_rejected", cur)
	status, _ = request(t, srv, "POST", "/api/tasks/"+task.ID+"/dispatch", "recovery-test", map[string]string{"runtime": "codex"})
	if status != 409 {
		t.Fatal("uncertain desktop run allowed another channel")
	}

	// Reopen the actual SQLite DB through a fresh HTTP service and dispatcher.
	restartAddr := srv.Addr
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	relay.disconnect.Store(false)
	srv, err = Start(restartAddr, db)
	if err != nil {
		t.Fatal(err)
	}
	c = client.New(&client.Discover{Addr: srv.Addr}, "recovery-acceptance")
	cur = awaitDesktopTask(t, c, task.ID, 180*time.Second, func(cur model.Task) bool {
		return cur.Execution != nil && cur.Execution.Status == "returned" && cur.ColumnID == p.Columns[2].ID
	})
	if cur.Execution.SessionID != original || relay.creates.Load() != 1 {
		t.Fatalf("duplicate session: creates=%d old=%s new=%s", relay.creates.Load(), original, cur.Execution.SessionID)
	}
	data, err := os.ReadFile(proof)
	if err != nil || string(data) != "START\nDONE\n" {
		t.Fatalf("artifact executed multiple times or incomplete: %q %v", data, err)
	}
	if !strings.Contains(fmt.Sprint(cur.Comments), "RECOVERY_FLOW_OK") {
		t.Fatal("missing final model result")
	}
	record("service_restart_resumed_original_session_artifact_exactly_once_review", cur)

	// Acknowledgement loss: the Host accepted the work, but the board cannot
	// tell. Explicit retry must query that identity rather than resubmit.
	proof2 := filepath.Join(workspace, "ack-proof.txt")
	prompt2 := fmt.Sprintf("For the file action, execute this bash command exactly once: printf 'ONCE\\n' >> '%s'. Do not repeat this command or touch other files. Write your result with ACK_FLOW_OK to the original card, move it to review, GET to confirm, then reply.", proof2)
	task2, err := c.CreateTask(p.ID, p.Columns[0].ID, "接收回执丢失验收", prompt2, "")
	if err != nil {
		t.Fatal(err)
	}
	relay.loseAck.Store(true)
	if err := c.Dispatch(task2.ID, "dsh"); err != nil {
		t.Fatal(err)
	}
	cur = awaitDesktopTask(t, c, task2.ID, 15*time.Second, func(cur model.Task) bool { return cur.Execution != nil && cur.Execution.Status == "unknown" })
	ackSession := cur.Execution.SessionID
	record("real_acceptance_acknowledgement_lost", cur)
	if err := c.Dispatch(task2.ID, "dsh"); err != nil {
		t.Fatal(err)
	}
	cur = awaitDesktopTask(t, c, task2.ID, 180*time.Second, func(cur model.Task) bool {
		return cur.Execution != nil && cur.Execution.Status == "returned" && cur.ColumnID == p.Columns[2].ID
	})
	data, err = os.ReadFile(proof2)
	if err != nil || string(data) != "ONCE\n" || relay.creates.Load() != 2 || cur.Execution.SessionID != ackSession {
		t.Fatalf("lost ack caused duplicate: %q %v creates=%d", data, err, relay.creates.Load())
	}
	record("retry_after_lost_ack_reused_session_and_executed_once", cur)

	// A normal model return can represent a blocked task. Only the Agent's
	// explicit board writes decide whether the card goes to review or backlog.
	body := "这是受阻状态回写验收：任务依赖未提供，不能实施。请按技能自行认领，随后在原卡写明‘验收故障注入：依赖缺失’，说明下一步等待补充依赖，评论含 AGENT_BLOCKED_OK；退回待办并 GET 确认。不要创建或修改文件，不要移动到评审或完成，不要创建替代卡。"
	blocked, err := c.CreateTask(p.ID, p.Columns[0].ID, "执行 Agent 受阻回写验收", body, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Dispatch(blocked.ID, "dsh"); err != nil {
		t.Fatal(err)
	}
	cur = awaitDesktopTask(t, c, blocked.ID, 180*time.Second, func(cur model.Task) bool { return cur.Execution != nil && cur.Execution.Status == "returned" })
	if cur.ColumnID != p.Columns[0].ID || cur.UpdatedBy != "DSH" || !strings.Contains(fmt.Sprint(cur.Comments), "AGENT_BLOCKED_OK") {
		t.Fatalf("blocked task was not controlled by Agent: %+v", cur)
	}
	record("agent_reported_blocker_returned_to_backlog_session_return_not_completion", cur)
}
