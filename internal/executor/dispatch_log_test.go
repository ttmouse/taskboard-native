package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"taskboard-native/internal/model"
)

// The executions row keeps only the latest state. Every transition has to reach
// the log, otherwise a card that dipped through disconnected and recovered
// leaves no trace of it.
func TestRecordTrackedStatusLogsEachTransitionOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dispatch.jsonl")
	d := &Dispatcher{handoffLog: newAutoHandoffLog(path)}
	run := &model.Execution{TaskID: "t-1", Runtime: RuntimeClaude, Status: "running"}
	last := run.Status

	run.Status = "running"
	d.recordTrackedStatus(run, "t3", &last)
	run.Status = "disconnected"
	run.Detail = "T3 线程连续 3 次不可达"
	d.recordTrackedStatus(run, "t3", &last)
	d.recordTrackedStatus(run, "t3", &last)
	run.Status = "returned"
	run.Detail = "T3 agent turn 已结束；任务状态由执行 Agent 回写"
	d.recordTrackedStatus(run, "t3", &last)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read dispatch log: %v", err)
	}
	events, err := ReadTaskHandoffEvents(path, "t-1", 100)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2 transitions (%s)", len(events), raw)
	}
	if events[0].ExecutionState != "disconnected" || events[1].ExecutionState != "returned" {
		t.Fatalf("states = %q, %q; want disconnected then returned", events[0].ExecutionState, events[1].ExecutionState)
	}
	if events[1].Message == "" {
		t.Fatal("transition message missing")
	}
}

// A diagnostic must stay one bounded line: the log is for scanning, and a
// multi-line CLI dump would break the one-record-per-line contract.
func TestClipMessageKeepsOneBoundedLine(t *testing.T) {
	if got := clipMessage("a\nb   c"); got != "a b c" {
		t.Fatalf("clip = %q, want %q", got, "a b c")
	}
	long := strings.Repeat("诊", dispatchLogMaxMessage+50)
	got := clipMessage(long)
	if n := len([]rune(got)); n != dispatchLogMaxMessage+1 {
		t.Fatalf("clipped length = %d runes, want %d plus ellipsis", n, dispatchLogMaxMessage)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("clipped message = %q, want a trailing ellipsis", got)
	}
	if strings.ContainsRune(got, '�') {
		t.Fatal("clipping split a multi-byte character")
	}
}

// CLI fallback is the last transport, so its failure text used to be the card's
// only evidence. It belongs in the log like every other channel's: the person
// reading the card needs the outcome, not a stderr dump.
func TestCLIFailureDiagnosticsGoToLogNotComments(t *testing.T) {
	previous := herdrAvailable
	herdrAvailable = func() bool { return false }
	t.Cleanup(func() { herdrAvailable = previous })

	installFakeRuntime(t, RuntimeCodex, `printf 'boom-marker\n' >&2
exit 3`)
	profile := writeT3Profile(t, filepath.Join(t.TempDir(), "absent"), t.TempDir(), "")
	st := newTestStore(t)
	_, task := projectWithWorkspace(t, st)
	logPath := filepath.Join(t.TempDir(), "dispatch.jsonl")
	d := NewDispatcher(st, profile, "", logPath)
	defer d.Close()

	if err := d.Dispatch(task.ID, RuntimeCodex, "test"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "CLI failure recorded", func() (bool, string) {
		run, err := st.Execution(task.ID)
		if err != nil || run == nil {
			return false, "no execution"
		}
		return run.Status == "failed", run.Status
	})

	comments, _ := st.Comments(task.ID)
	for _, c := range comments {
		if strings.Contains(c.Text, "boom-marker") || strings.Contains(c.Text, "诊断：") {
			t.Fatalf("CLI diagnostic leaked into the user timeline: %q", c.Text)
		}
	}
	if !hasComment(comments, "执行失败（exit 3") {
		t.Fatalf("outcome comment missing: %#v", comments)
	}

	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read dispatch log: %v", err)
	}
	logText := string(logged)
	if !strings.Contains(logText, "boom-marker") {
		t.Fatalf("CLI diagnostic missing from the log: %s", logText)
	}
	if !strings.Contains(logText, `"channel":"cli"`) || !strings.Contains(logText, `"event":"dispatch.status"`) {
		t.Fatalf("CLI outcome missing from the log: %s", logText)
	}
}
