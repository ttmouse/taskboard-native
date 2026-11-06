package executor

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"taskboard-native/internal/model"
)

// writeFakeT3 writes an executable t3 shim so every test drives the real
// command-line contract instead of a mocked function.
func writeFakeT3(t *testing.T, body string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "t3")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write fake t3: %v", err)
	}
	return bin
}

func writeT3Profile(t *testing.T, bin, baseDir, extra string) string {
	t.Helper()
	profile := filepath.Join(t.TempDir(), "runtimes.json")
	body := `{"t3":{"bin":` + strconv.Quote(bin) + `,"baseDir":` + strconv.Quote(baseDir) + extra + `}}`
	if err := os.WriteFile(profile, []byte(body), 0o600); err != nil {
		t.Fatalf("write profile: %v", err)
	}
	return profile
}

func testT3Plan(t *testing.T, bin, baseDir string) *t3RuntimePlan {
	t.Helper()
	return &t3RuntimePlan{bin: bin, baseDir: baseDir, provider: "codex", model: "gpt-test"}
}

func TestT3CreateStartsThread(t *testing.T) {
	calls := filepath.Join(t.TempDir(), "calls")
	t.Setenv("T3_CALLS", calls)
	bin := writeFakeT3(t, `
printf '%s\n' "$*" >> "$T3_CALLS"
case "$1 $2" in
  "thread list") printf '[]'; exit 0 ;;
  "project add") printf 'Added project p1 (probe) at /work.\n'; exit 0 ;;
  "thread start") printf '{"threadId":"mcp:abc-1","projectId":"p1","runId":"run:1"}'; exit 0 ;;
esac
exit 1
`)
	task := model.Task{ID: "task-1", Title: "修复\n  卡片 标题"}
	got, err := t3Create(context.Background(), task, "/work/probe", RuntimeCodex, "Codex", "任务正文", testT3Plan(t, bin, t.TempDir()))
	if err != nil || got != "t3:mcp:abc-1" {
		t.Fatalf("t3Create() = %q, %v", got, err)
	}
	logged, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"thread list",
		"project add --base-dir",
		"--provider codex",
		"--model gpt-test",
		"--title 修复 卡片 标题",
		"TASKBOARD_DISPATCH_TASK_ID=task-1",
		"执行身份：Codex；写看板时使用 --agent Codex。",
		"任务正文",
	} {
		if !strings.Contains(string(logged), want) {
			t.Errorf("t3 calls missing %q:\n%s", want, logged)
		}
	}
}

func TestT3CreateTreatsExistingProjectAsSuccess(t *testing.T) {
	calls := filepath.Join(t.TempDir(), "calls")
	t.Setenv("T3_CALLS", calls)
	bin := writeFakeT3(t, `
printf '%s\n' "$*" >> "$T3_CALLS"
case "$1 $2" in
  "thread list") printf '[]'; exit 0 ;;
  "project add") printf 'ProjectAlreadyExistsError: An active project already exists for /work.\n' >&2; exit 1 ;;
  "thread start") printf '{"threadId":"mcp:abc-2"}'; exit 0 ;;
esac
exit 1
`)
	got, err := t3Create(context.Background(), model.Task{ID: "task-2", Title: "t"}, "/work", RuntimeClaude, "Claude Code", "prompt", testT3Plan(t, bin, t.TempDir()))
	if err != nil || got != "t3:mcp:abc-2" {
		t.Fatalf("t3Create() = %q, %v", got, err)
	}
}

func TestT3CreateStopsOnProjectFailure(t *testing.T) {
	calls := filepath.Join(t.TempDir(), "calls")
	t.Setenv("T3_CALLS", calls)
	bin := writeFakeT3(t, `
printf '%s\n' "$*" >> "$T3_CALLS"
case "$1 $2" in
  "thread list") printf '[]'; exit 0 ;;
  "project add") printf 'permission denied\n' >&2; exit 2 ;;
esac
exit 0
`)
	_, err := t3Create(context.Background(), model.Task{ID: "task-3", Title: "t"}, "/work", RuntimeCodex, "Codex", "prompt", testT3Plan(t, bin, t.TempDir()))
	if err == nil || isT3Unavailable(err) {
		t.Fatalf("project failure = %v, want a config error", err)
	}
	if !strings.Contains(err.Error(), "T3 无法登记项目目录") {
		t.Fatalf("error = %v", err)
	}
	logged, _ := os.ReadFile(calls)
	if strings.Contains(string(logged), "thread start") {
		t.Fatalf("thread started after the project failed:\n%s", logged)
	}
}

func TestT3CreateUnavailableWhenServerMissing(t *testing.T) {
	bin := writeFakeT3(t, `
case "$1 $2" in
  "thread list") printf 'No running T3 Code server uses /work.\n' >&2; exit 1 ;;
esac
exit 0
`)
	_, err := t3Create(context.Background(), model.Task{ID: "task-4", Title: "t"}, "/work", RuntimeCodex, "Codex", "prompt", testT3Plan(t, bin, t.TempDir()))
	if !isT3Unavailable(err) {
		t.Fatalf("missing server = %v, want unavailable", err)
	}
}

func TestT3CreateUnavailableWhenPinnedPortIsClosed(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	calls := filepath.Join(t.TempDir(), "calls")
	t.Setenv("T3_CALLS", calls)
	bin := writeFakeT3(t, `printf 'called\n' >> "$T3_CALLS"`)
	plan := testT3Plan(t, bin, t.TempDir())
	plan.port = port
	_, err = t3Create(context.Background(), model.Task{ID: "task-5", Title: "t"}, "/work", RuntimeCodex, "Codex", "prompt", plan)
	if !isT3Unavailable(err) {
		t.Fatalf("closed port = %v, want unavailable", err)
	}
	if _, statErr := os.Stat(calls); !os.IsNotExist(statErr) {
		t.Fatal("probed the CLI after the pinned port refused the connection")
	}
}

func TestT3CreateClassifiesStartFailures(t *testing.T) {
	bin := writeFakeT3(t, `
case "$1 $2" in
  "thread list") printf '[]'; exit 0 ;;
  "project add") printf 'Added.\n'; exit 0 ;;
  "thread start") printf 'the project has no default model\n' >&2; exit 1 ;;
esac
exit 0
`)
	_, err := t3Create(context.Background(), model.Task{ID: "task-6", Title: "t"}, "/work", RuntimeCodex, "Codex", "prompt", testT3Plan(t, bin, t.TempDir()))
	if err == nil || isT3Unavailable(err) {
		t.Fatalf("start failure = %v, want a config error", err)
	}
	if !strings.Contains(err.Error(), "T3 无法启动线程") {
		t.Fatalf("error = %v", err)
	}
}

func TestT3CreateUncertainWithoutThreadID(t *testing.T) {
	bin := writeFakeT3(t, `
case "$1 $2" in
  "thread list") printf '[]'; exit 0 ;;
  "project add") printf 'Added.\n'; exit 0 ;;
  "thread start") printf '{"projectId":"p1"}'; exit 0 ;;
esac
exit 0
`)
	_, err := t3Create(context.Background(), model.Task{ID: "task-7", Title: "t"}, "/work", RuntimeCodex, "Codex", "prompt", testT3Plan(t, bin, t.TempDir()))
	var failure *t3Error
	if !errors.As(err, &failure) || !failure.uncertain {
		t.Fatalf("missing thread id = %v, want uncertain", err)
	}
}

func shortT3Timers(t *testing.T) {
	t.Helper()
	previousInterval, previousIdle, previousGrace, previousFlap := t3ProbeInterval, t3IdleGrace, t3UnreachableGrace, t3FlapProbes
	t3ProbeInterval = 10 * time.Millisecond
	t3IdleGrace = 50 * time.Millisecond
	t3UnreachableGrace = 400 * time.Millisecond
	t3FlapProbes = 2
	t.Cleanup(func() {
		t3ProbeInterval, t3IdleGrace, t3UnreachableGrace, t3FlapProbes = previousInterval, previousIdle, previousGrace, previousFlap
	})
}

func trackT3Show(t *testing.T, showBody string, wantStatus, wantDetail string) {
	t.Helper()
	shortT3Timers(t)
	st := newTestStore(t)
	_, task := projectWithWorkspace(t, st)
	d := NewDispatcher(st)
	defer d.Close()
	bin := writeFakeT3(t, showBody)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	run := &model.Execution{TaskID: task.ID, Runtime: RuntimeCodex, SessionID: "t3:mcp:t-1", Status: "running"}
	go func() {
		defer close(done)
		d.trackT3(ctx, run, testT3Plan(t, bin, t.TempDir()))
	}()
	waitFor(t, "execution "+wantStatus, func() (bool, string) {
		execution, err := st.Execution(task.ID)
		if err != nil || execution == nil {
			return false, "no execution"
		}
		ok := execution.Status == wantStatus && (wantDetail == "" || execution.Detail == wantDetail)
		return ok, execution.Status + " / " + execution.Detail
	})
	cancel()
	<-done
}

func TestT3StatusMapping(t *testing.T) {
	const show = `
case "$1 $2" in
  "thread show") printf '{"thread":{"threadId":"mcp:t-1","status":"%s"},"messages":[%s]}' "$T3_STATE" "$T3_MESSAGES"; exit 0 ;;
esac
exit 1
`
	for _, tc := range []struct{ name, state, messages, wantStatus, wantDetail string }{
		{"preparing", "preparing", ``, "running", ""},
		{"starting", "starting", ``, "running", ""},
		{"running", "running", ``, "running", ""},
		{"waiting", "waiting", ``, "waiting_approval", "T3 agent 等待人工处理"},
		{"idle-before-turn", "idle", ``, "running", t3InitialTurnPendingDetail},
		{"idle-after-turn", "idle", `{"role":"assistant","runId":"r1","text":"ok"}`, "returned", "T3 agent turn 已结束；任务状态由执行 Agent 回写"},
		{"completed", "completed", `{"role":"assistant","runId":"r1","text":"ok"}`, "returned", "T3 agent turn 已结束；任务状态由执行 Agent 回写"},
		{"failed", "failed", ``, "failed", "T3 线程状态：failed"},
		{"cancelled", "cancelled", ``, "failed", "T3 线程状态：cancelled"},
		{"interrupted", "interrupted", ``, "failed", "T3 线程状态：interrupted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("T3_STATE", tc.state)
			t.Setenv("T3_MESSAGES", tc.messages)
			trackT3Show(t, show, tc.wantStatus, tc.wantDetail)
		})
	}
	t.Run("unreachable", func(t *testing.T) {
		shortT3Timers(t)
		st := newTestStore(t)
		_, task := projectWithWorkspace(t, st)
		d := NewDispatcher(st)
		defer d.Close()
		bin := writeFakeT3(t, `printf 'connection refused\n' >&2; exit 1`)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		run := &model.Execution{TaskID: task.ID, Runtime: RuntimeCodex, SessionID: "t3:mcp:t-1", Status: "running"}
		go func() {
			defer close(done)
			d.trackT3(ctx, run, testT3Plan(t, bin, t.TempDir()))
		}()
		sawDisconnected := false
		waitFor(t, "unreachable grace", func() (bool, string) {
			execution, err := st.Execution(task.ID)
			if err != nil || execution == nil {
				return false, "no execution"
			}
			if execution.Status == "disconnected" {
				sawDisconnected = true
			}
			return execution.Status == "released", execution.Status + " / " + execution.Detail
		})
		cancel()
		<-done
		if !sawDisconnected {
			t.Fatal("never reported disconnected before releasing the write lock")
		}
	})
}

func TestT3FollowupSendsToOriginalThread(t *testing.T) {
	calls := filepath.Join(t.TempDir(), "calls")
	t.Setenv("T3_CALLS", calls)
	bin := writeFakeT3(t, `
printf '%s\n' "$*" >> "$T3_CALLS"
case "$1 $2" in
  "thread show") printf '{"thread":{"threadId":"mcp:t-1","status":"idle"},"messages":[]}' ;;
  "thread send") printf 'Queued.' ;;
esac
exit 0
`)
	profile := writeT3Profile(t, bin, t.TempDir(), `,"model":{"codex":"gpt-test"}`)
	st := newTestStore(t)
	_, task := projectWithWorkspace(t, st)
	if err := st.SaveExecution(model.Execution{TaskID: task.ID, Runtime: RuntimeCodex, SessionID: "t3:mcp:t-1", Status: "returned"}); err != nil {
		t.Fatal(err)
	}
	comment, err := st.AddComment(task.ID, "继续处理这条要求", "豆爸")
	if err != nil {
		t.Fatal(err)
	}
	d := NewDispatcher(st, profile)
	defer d.Close()
	if err := d.Continue(task.ID, comment.ID, "豆爸"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "follow-up delivery", func() (bool, string) {
		status, err := st.FollowupStatus(task.ID, comment.ID)
		return err == nil && status == "sent", status
	})
	d.Close()
	logged, _ := os.ReadFile(calls)
	if !strings.Contains(string(logged), "thread send") || !strings.Contains(string(logged), comment.ID) {
		t.Fatalf("follow-up did not reach the original thread:\n%s", logged)
	}
}

func TestDispatchFallsBackToCLIWhenT3Unreachable(t *testing.T) {
	previous := herdrAvailable
	herdrAvailable = func() bool { return false }
	t.Cleanup(func() { herdrAvailable = previous })

	bin := writeFakeT3(t, `
case "$1 $2" in
  "thread list") printf 'No running T3 Code server uses /work.\n' >&2; exit 1 ;;
esac
exit 0
`)
	profile := writeT3Profile(t, bin, t.TempDir(), `,"model":{"codex":"gpt-test"}`)
	marker := filepath.Join(t.TempDir(), "cli-ran")
	t.Setenv("CLI_MARKER", marker)
	installFakeRuntime(t, RuntimeCodex, `printf 'ran' > "$CLI_MARKER"
printf '{"type":"thread.started","thread_id":"00000000-0000-4000-8000-000000000002"}\n'
printf 'returned\n'`)
	st := newTestStore(t)
	_, task := projectWithWorkspace(t, st)
	d := NewDispatcher(st, profile)
	defer d.Close()
	if err := d.Dispatch(task.ID, RuntimeCodex, "test"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "CLI fallback", func() (bool, string) {
		execution, err := st.Execution(task.ID)
		if err != nil || execution == nil {
			return false, "no execution"
		}
		return execution.Status == "returned", execution.Status + " / " + execution.Detail
	})
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("CLI fallback did not run")
	}
	comments, _ := st.Comments(task.ID)
	if !hasComment(comments, "T3 派发 codex 失败") || !hasComment(comments, "回退到下一可用执行方式") {
		t.Fatalf("missing T3 fallback comment: %#v", comments)
	}
}

func TestDispatchUsesT3WhenHerdrUnavailable(t *testing.T) {
	previous := herdrAvailable
	herdrAvailable = func() bool { return false }
	t.Cleanup(func() { herdrAvailable = previous })

	bin := writeFakeT3(t, `
case "$1 $2" in
  "thread list") printf '[]'; exit 0 ;;
  "project add") printf 'Added project p1.\n'; exit 0 ;;
  "thread start") printf '{"threadId":"mcp:live"}'; exit 0 ;;
  "thread show") printf '{"thread":{"threadId":"mcp:live","status":"completed"},"messages":[{"role":"assistant","runId":"r1"}]}'; exit 0 ;;
esac
exit 1
`)
	profile := writeT3Profile(t, bin, t.TempDir(), `,"model":{"codex":"gpt-test"}`)
	marker := filepath.Join(t.TempDir(), "cli-ran")
	t.Setenv("CLI_MARKER", marker)
	installFakeRuntime(t, RuntimeCodex, `printf 'ran' > "$CLI_MARKER"`)
	st := newTestStore(t)
	_, task := projectWithWorkspace(t, st)
	d := NewDispatcher(st, profile)
	defer d.Close()
	if err := d.Dispatch(task.ID, RuntimeCodex, "test"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "T3 dispatch", func() (bool, string) {
		execution, err := st.Execution(task.ID)
		if err != nil || execution == nil {
			return false, "no execution"
		}
		return execution.SessionID == "t3:mcp:live" && execution.Status == "returned", execution.SessionID + " / " + execution.Status
	})
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("CLI ran even though T3 accepted the thread")
	}
	comments, _ := st.Comments(task.ID)
	if !hasComment(comments, "已交给 T3 codex") {
		t.Fatalf("missing T3 dispatch comment: %#v", comments)
	}
}

func TestT3PlanResolution(t *testing.T) {
	bin := writeFakeT3(t, "exit 0")
	baseDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(baseDir, "userdata"), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"manifest":{"providers":{"claudeAgent":{"defaults":{"chat":"claude-default"}}}}}`
	if err := os.WriteFile(filepath.Join(baseDir, "userdata", "model-manifest.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	profile := writeT3Profile(t, bin, baseDir, "")
	plan, err := t3Plan(profile, RuntimeClaude)
	if err != nil || plan == nil {
		t.Fatalf("t3Plan(claude) = %v, %v", plan, err)
	}
	if plan.provider != "claudeAgent" || plan.model != "claude-default" || plan.preferT3 {
		t.Fatalf("defaults resolved to %#v", plan)
	}
	if plan, err := t3Plan(profile, RuntimeCodex); err != nil || plan == nil || plan.canStart() {
		t.Fatalf("codex without a model = %v, %v; want resolvable but not startable", plan, err)
	}
	if plan, err := t3Plan(profile, RuntimePi); err != nil || plan != nil {
		t.Fatalf("pi = %v, %v; want no T3", plan, err)
	}

	disabled := writeT3Profile(t, bin, baseDir, `,"enabled":false`)
	if plan, err := t3Plan(disabled, RuntimeClaude); err != nil || plan != nil {
		t.Fatalf("disabled = %v, %v", plan, err)
	}
	missing := writeT3Profile(t, filepath.Join(t.TempDir(), "absent"), baseDir, "")
	if plan, err := t3Plan(missing, RuntimeClaude); err != nil || plan != nil {
		t.Fatalf("missing bin = %v, %v", plan, err)
	}
	preferred := writeT3Profile(t, bin, baseDir, `,"prefer":"t3","model":{"claude":"claude-override"},"provider":{"claude":"claudeAgent"}`)
	plan, err = t3Plan(preferred, RuntimeClaude)
	if err != nil || plan == nil || !plan.preferT3 || plan.model != "claude-override" {
		t.Fatalf("overrides = %#v, %v", plan, err)
	}
	invalid := writeT3Profile(t, bin, baseDir, `,"prefer":"elsewhere"`)
	if _, err := t3Plan(invalid, RuntimeClaude); err == nil {
		t.Fatal("accepted an unknown prefer value")
	}
}

// The T3 section lives in the same sidecar as the per-runtime connection
// profiles; its provider/model maps must not break the codex entry.
func TestT3SectionDoesNotBreakCodexConnection(t *testing.T) {
	bin := writeFakeT3(t, "exit 0")
	profile := writeT3Profile(t, bin, t.TempDir(), `,"provider":{"codex":"codex"},"model":{"codex":"gpt-test"}`)
	args, err := runtimeArgs("dir", "task", RuntimeCodex, profile)
	if err != nil {
		t.Fatalf("runtimeArgs: %v", err)
	}
	if len(args) == 0 || args[len(args)-1] != "task" {
		t.Fatalf("unexpected args: %q", args)
	}
}

func TestT3SessionAddress(t *testing.T) {
	if threadID, ok := t3SessionAddress("t3:mcp:abc"); !ok || threadID != "mcp:abc" {
		t.Fatalf("t3SessionAddress() = %q, %v", threadID, ok)
	}
	if _, ok := t3SessionAddress("herdr:t7"); ok {
		t.Fatal("Herdr session classified as T3")
	}
}
