package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"taskboard-native/internal/model"
	"testing"
)

func awaitNoExecution(t *testing.T, d *Dispatcher) {
	t.Helper()
	waitFor(t, "execution returned", func() (bool, string) {
		d.mu.Lock()
		defer d.mu.Unlock()
		return len(d.inflight) == 0, fmt.Sprint(len(d.inflight))
	})
}

func TestHerdrFollowupDoesNotAddRedundantSuccessComment(t *testing.T) {
	st := newTestStore(t)
	p, task := projectWithWorkspace(t, st)
	_, _ = installFakeHerdr(t, p.WorkspacePath, "idle")
	if err := st.SaveExecution(model.Execution{TaskID: task.ID, Runtime: RuntimeCodex, SessionID: "herdr:test-terminal-1", Status: "returned"}); err != nil {
		t.Fatal(err)
	}
	cm, err := st.AddComment(task.ID, "继续处理这条要求", "豆爸")
	if err != nil {
		t.Fatal(err)
	}
	d := NewDispatcher(st)
	defer d.Close()
	if err := d.Continue(task.ID, cm.ID, "豆爸"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Herdr follow-up delivery", func() (bool, string) {
		status, err := st.FollowupStatus(task.ID, cm.ID)
		return err == nil && status == "sent", status
	})
	d.Close()

	comments, err := st.Comments(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 || comments[0].ID != cm.ID {
		t.Fatalf("successful Herdr follow-up added a redundant comment: %#v", comments)
	}
	if status, _ := st.FollowupStatus(task.ID, cm.ID); status != "sent" {
		t.Fatalf("follow-up status = %q, want sent", status)
	}
}

func TestCLICommentResumesExactSessionAndDeduplicatesAfterRestart(t *testing.T) {
	for _, runtime := range []string{RuntimeCodex, RuntimePi, RuntimeClaude} {
		t.Run(runtime, func(t *testing.T) {
			log := filepath.Join(t.TempDir(), "calls")
			t.Setenv("FOLLOWUP_CALLS", log)
			installFakeRuntime(t, runtime, `printf '%s\n' "$*" >> "$FOLLOWUP_CALLS"
while [ "$#" -gt 0 ]; do
 if [ "$1" = "--session" ]; then shift; printf '{"type":"session"}\n' >> "$1"; fi
 shift
done
printf '{"type":"thread.started","thread_id":"00000000-0000-4000-8000-000000000001"}\n'
printf 'returned\n'`)
			st := newTestStore(t)
			p, task := projectWithWorkspace(t, st)
			_, sendsLog := installFakeHerdr(t, p.WorkspacePath, "done")
			d := NewDispatcher(st)
			defer func() { d.Close() }()
			if err := d.Dispatch(task.ID, runtime, "test"); err != nil {
				t.Fatal(err)
			}
			awaitNoExecution(t, d)
			original, _ := st.Execution(task.ID)
			if original == nil || original.SessionID != "herdr:test-terminal-1" || original.Channel() != runtime {
				t.Fatalf("lost identity: %+v", original)
			}
			awaiting, _ := st.Task(task.ID)
			review, ok := p.ColumnByRole(model.ColumnRoleReview)
			if !ok {
				t.Fatal("review column missing")
			}
			if _, err := st.MoveTask(awaiting.ID, awaiting.Version, review.ID, "", "user"); err != nil {
				t.Fatal(err)
			}
			cm, err := st.AddComment(task.ID, "只跟进本条新要求", "user")
			if err != nil {
				t.Fatal(err)
			}
			if err := d.Continue(task.ID, cm.ID, "user"); err != nil {
				t.Fatal(err)
			}
			doing, _ := p.ColumnByRole(model.ColumnRoleDoing)
			cur, _ := st.Task(task.ID)
			if cur.ColumnID != doing.ID {
				t.Fatalf("accepted follow-up did not move card immediately: column=%q want=%q", cur.ColumnID, doing.ID)
			}
			if err := d.Continue(task.ID, cm.ID, "user"); err != nil {
				t.Fatal(err)
			}
			awaitNoExecution(t, d)
			d.Close()
			d = NewDispatcher(st)
			if err := d.Continue(task.ID, cm.ID, "user"); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(sendsLog)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(got)), "\n")
			// The follow-up prompt must ask for the comment's work, not for
			// another follow-up: an agent that calls tb followup only confirms
			// its own delivery and reports success while nothing happened.
			if len(lines) != 1 || !strings.Contains(lines[0], "send test-terminal-1") || !strings.Contains(lines[0], cm.ID) || !strings.Contains(lines[0], "不要调用 tb followup") {
				t.Fatalf("not exact/idempotent: %s", got)
			}
			if status, _ := st.FollowupStatus(task.ID, cm.ID); status != "sent" {
				t.Fatalf("followup status = %q", status)
			}
			cur, _ = st.Task(task.ID)
			if cur.ColumnID != doing.ID {
				t.Fatalf("active follow-up column = %q, want doing %q", cur.ColumnID, doing.ID)
			}
		})
	}
}

func TestContinueRejectsMissingSessionCommentAndBusy(t *testing.T) {
	st := newTestStore(t)
	d := NewDispatcher(st)
	defer func() { d.Close() }()
	p, task := projectWithWorkspace(t, st)
	review, _ := p.ColumnByRole(model.ColumnRoleReview)
	awaiting, err := st.MoveTask(task.ID, task.Version, review.ID, "", "user")
	if err != nil {
		t.Fatal(err)
	}
	cm, _ := st.AddComment(task.ID, "follow", "user")
	if err := d.Continue(task.ID, cm.ID, "user"); err == nil {
		t.Fatal("created replacement session")
	}
	cur, _ := st.Task(task.ID)
	if cur.ColumnID != review.ID || cur.Version != awaiting.Version {
		t.Fatalf("rejected follow-up moved card: %+v", cur)
	}
	if err := d.Continue(task.ID, "other-task-comment", "user"); err == nil {
		t.Fatal("accepted foreign comment")
	}
	if err := st.SaveExecution(model.Execution{TaskID: task.ID, Runtime: RuntimeCodex, SessionID: "original", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if err := d.Continue(task.ID, cm.ID, "user"); !errors.Is(err, ErrInProgress) {
		t.Fatal(err)
	}
	if err := d.Dispatch(task.ID, RuntimeDSH, "user"); !errors.Is(err, ErrInProgress) {
		t.Fatalf("cross-channel bypass: %v", err)
	}
	d.Close()
	d = NewDispatcher(st)
	run, _ := st.Execution(task.ID)
	if run.Status != "disconnected" || run.Channel() != RuntimeCodex {
		t.Fatal(run)
	}
	// That process died with the service: the record can never move again, so
	// it must not block its own follow-up forever.
	installFakeRuntime(t, RuntimeCodex, `printf '{"type":"thread.started","thread_id":"original"}\n'`)
	if err := d.Continue(task.ID, cm.ID, "user"); err != nil {
		t.Fatalf("stalled run blocked its own follow-up: %v", err)
	}
	awaitNoExecution(t, d)
	if after, _ := st.Execution(task.ID); after.Status == "disconnected" || after.SessionID != "original" {
		t.Fatalf("follow-up did not resume the recorded session: %+v", after)
	}
}

// A codex run cut off by a service restart used to answer every later dispatch
// with ErrInProgress: the card could neither be run again nor followed up.
func TestStalledCLIRunDoesNotBlockFreshDispatch(t *testing.T) {
	installFakeRuntime(t, RuntimeCodex, `printf '{"type":"thread.started","thread_id":"fresh"}\n'`)
	st := newTestStore(t)
	p, task := projectWithWorkspace(t, st)
	installFakeHerdr(t, p.WorkspacePath, "done")
	if err := st.SaveExecution(model.Execution{TaskID: task.ID, Runtime: RuntimeCodex, SessionID: "dead", Status: "disconnected"}); err != nil {
		t.Fatal(err)
	}
	d := NewDispatcher(st)
	defer d.Close()
	if err := d.Dispatch(task.ID, RuntimeCodex, "test"); err != nil {
		t.Fatalf("stalled run blocked a fresh dispatch: %v", err)
	}
	awaitNoExecution(t, d)
	run, _ := st.Execution(task.ID)
	if run == nil || run.SessionID != "herdr:test-terminal-1" || run.Status != "returned" {
		t.Fatalf("dispatch did not start a new session: %+v", run)
	}
}

func TestClaudeSessionIsUUIDAndResumesOnlyThatID(t *testing.T) {
	calls := filepath.Join(t.TempDir(), "claude")
	t.Setenv("CLAUDE_CALLS", calls)
	installFakeRuntime(t, RuntimeClaude, `printf '%s\n' "$*" >> "$CLAUDE_CALLS"`)
	st := newTestStore(t)
	d := NewDispatcher(st)
	defer func() { d.Close() }()
	_, task := projectWithWorkspace(t, st)
	const sessionID = "00000000-0000-4000-8000-000000000001"
	if err := st.SaveExecution(model.Execution{TaskID: task.ID, Runtime: RuntimeClaude, SessionID: sessionID, Status: "returned"}); err != nil {
		t.Fatal(err)
	}
	cm, _ := st.AddComment(task.ID, "follow", "user")
	if err := d.Continue(task.ID, cm.ID, "user"); err != nil {
		t.Fatal(err)
	}
	awaitNoExecution(t, d)
	out, _ := os.ReadFile(calls)
	if !strings.Contains(string(out), "--resume "+sessionID) {
		t.Fatalf("legacy session id was not resumed: %s", out)
	}

	// A recorded id the CLI cannot resume must fail loudly instead of quietly
	// becoming a second writer in a brand new conversation.
	if err := st.SaveExecution(model.Execution{TaskID: task.ID, Runtime: RuntimeClaude, SessionID: "legacy-session", Status: "disconnected"}); err != nil {
		t.Fatal(err)
	}
	cm, _ = st.AddComment(task.ID, "follow legacy", "user")
	if err := d.Continue(task.ID, cm.ID, "user"); err == nil {
		t.Fatal("resumed a non-UUID claude session")
	}
	if after, _ := os.ReadFile(calls); strings.Count(string(after), "\n") != 1 {
		t.Fatalf("claude started a replacement run: %s", after)
	}
}

func TestPiMissingSessionNeverCreatesReplacement(t *testing.T) {
	installFakeRuntime(t, RuntimePi, "echo should-not-run")
	st := newTestStore(t)
	d := NewDispatcher(st)
	defer d.Close()
	_, task := projectWithWorkspace(t, st)
	cm, _ := st.AddComment(task.ID, "follow", "user")
	st.SaveExecution(model.Execution{TaskID: task.ID, Runtime: RuntimePi, SessionID: filepath.Join(t.TempDir(), "absent.jsonl"), Status: "returned"})
	if err := d.Continue(task.ID, cm.ID, "user"); err == nil {
		t.Fatal("created new Pi session")
	}
}

func TestDesktopFollowupWaitsForNewTurn(t *testing.T) {
	var mu sync.Mutex
	polls, starts := 0, 0
	var received map[string]any
	fakeDesktop(t, func(method string, params map[string]any) any {
		mu.Lock()
		defer mu.Unlock()
		switch method {
		case "session.getState":
			return map[string]any{"status": "idle"}
		case "session.startTurn":
			starts++
			received = params
			return map[string]any{"accepted": true}
		case "session.getSnapshot":
			id := "old"
			if starts > 0 {
				polls++
				if polls >= 2 {
					id = "new"
				}
			}
			return map[string]any{"snapshotComplete": true, "items": []any{map[string]any{"type": "turn.end", "turnId": id, "status": "done"}}}
		}
		t.Errorf("unexpected %s", method)
		return nil
	})
	st := newTestStore(t)
	d := NewDispatcher(st)
	defer d.Close()
	_, task := projectWithWorkspace(t, st)
	cm, _ := st.AddComment(task.ID, "follow", "user")
	st.SaveExecution(model.Execution{TaskID: task.ID, SessionID: "original", Status: "returned"})
	if err := d.Continue(task.ID, cm.ID, "user"); err != nil {
		t.Fatal(err)
	}
	awaitNoExecution(t, d)
	mu.Lock()
	defer mu.Unlock()
	if starts != 1 || polls < 2 || received["sessionId"] != "original" || received["clientMessageId"] != "taskboard-comment-"+cm.ID {
		t.Fatalf("starts=%d polls=%d params=%v", starts, polls, received)
	}
	run, _ := st.Execution(task.ID)
	if run.Status != "returned" || run.PreviousTurnID != "old" {
		t.Fatal(run)
	}
}

func TestDesktopFollowupUnknownReceiptDoesNotReplay(t *testing.T) {
	starts := 0
	fakeDesktop(t, func(method string, _ map[string]any) any {
		switch method {
		case "session.getState":
			return map[string]any{"status": "idle"}
		case "session.getSnapshot":
			id := "old"
			if starts > 0 {
				id = "new"
			}
			return map[string]any{"snapshotComplete": true, "items": []any{map[string]any{"type": "turn.end", "turnId": id, "status": "done"}}}
		case "session.startTurn":
			starts++
			return nil
		}
		return nil
	})
	st := newTestStore(t)
	d := NewDispatcher(st)
	defer func() { d.Close() }()
	_, task := projectWithWorkspace(t, st)
	cm, _ := st.AddComment(task.ID, "follow", "user")
	st.SaveExecution(model.Execution{TaskID: task.ID, SessionID: "original", Status: "returned"})
	if err := d.Continue(task.ID, cm.ID, "user"); err != nil {
		t.Fatal(err)
	}
	awaitNoExecution(t, d)
	if err := d.Continue(task.ID, cm.ID, "user"); !errors.Is(err, ErrInProgress) {
		t.Fatalf("unknown receipt: %v", err)
	}
	d.Close()
	d = NewDispatcher(st)
	awaitNoExecution(t, d)
	if err := d.Continue(task.ID, cm.ID, "user"); !errors.Is(err, ErrInProgress) {
		t.Fatal(err)
	}
	if starts != 1 {
		t.Fatalf("replayed %d times", starts)
	}
}

func TestCodexResumeKeepsPinnedConnection(t *testing.T) {
	installFakeRuntime(t, RuntimeCodex, "echo harmless")
	profile := filepath.Join(t.TempDir(), "runtime.json")
	os.WriteFile(profile, []byte(`{"codex":{"baseUrl":"http://127.0.0.1:3425/v1","model":"codex/gpt-6-luna"}}`), 0600)
	st := newTestStore(t)
	d := NewDispatcher(st, profile)
	defer d.Close()
	cmd, err := execCommand(context.Background(), t.TempDir(), "prompt", RuntimeCodex, profile)
	if err != nil {
		t.Fatal(err)
	}
	err = d.prepareCLI(cmd, &model.Execution{Runtime: RuntimeCodex, SessionID: "original"}, true)
	if err != nil || !strings.Contains(strings.Join(cmd.Args, " "), `model="codex/gpt-6-luna"`) || cmd.Dir == "" {
		t.Fatalf("%q %v", cmd.Args, err)
	}
}

func TestCLIForwardsBoardAddressAndAgent(t *testing.T) {
	log := filepath.Join(t.TempDir(), "routing")
	t.Setenv("FOLLOWUP_ROUTING", log)
	installFakeRuntime(t, RuntimeCodex, `printf '%s %s' "$TASKBOARD_ADDR" "$TB_AGENT" > "$FOLLOWUP_ROUTING"`)
	st := newTestStore(t)
	p, task := projectWithWorkspace(t, st)
	argsLog, _ := installFakeHerdr(t, p.WorkspacePath, "done")
	d := NewDispatcher(st, "", "http://127.0.0.1:54321")
	defer d.Close()
	if err := d.Dispatch(task.ID, RuntimeCodex, "user"); err != nil {
		t.Fatal(err)
	}
	awaitNoExecution(t, d)
	got, err := os.ReadFile(argsLog)
	if err != nil || !strings.Contains(string(got), "TASKBOARD_ADDR=127.0.0.1:54321") || !strings.Contains(string(got), "TB_AGENT=Codex") {
		t.Fatalf("Herdr routing args=%q err=%v", got, err)
	}
}
