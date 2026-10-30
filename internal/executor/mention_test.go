package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A comment may address a channel other than the one that owns the card. That
// is the human's explicit instruction, so the follow-up opens a session there;
// without an address the card keeps the session it already has.
func TestMentionSwitchesFollowupChannel(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls")
	t.Setenv("MENTION_CALLS", log)
	script := `printf '%s\n' "$*" >> "$MENTION_CALLS"
printf '{"type":"thread.started","thread_id":"00000000-0000-4000-8000-000000000009"}\n'
printf 'returned\n'`
	installFakeRuntime(t, RuntimeCodex, script)
	installFakeRuntime(t, RuntimeClaude, script)

	st := newTestStore(t)
	p, task := projectWithWorkspace(t, st)
	argsLog, sendsLog := installFakeHerdr(t, p.WorkspacePath, "done")
	d := NewDispatcher(st)
	defer func() { d.Close() }()
	if err := d.Dispatch(task.ID, RuntimeCodex, "test"); err != nil {
		t.Fatal(err)
	}
	awaitNoExecution(t, d)
	original, err := st.Execution(task.ID)
	if err != nil || original == nil || original.SessionID == "" {
		t.Fatalf("no original session: %+v %v", original, err)
	}

	cm, err := st.AddCommentTo(task.ID, "改由 Claude 复核", "human", RuntimeClaude)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Continue(task.ID, cm.ID, "human"); err != nil {
		t.Fatal(err)
	}
	awaitNoExecution(t, d)

	switched, err := st.Execution(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if switched.Channel() != RuntimeClaude {
		t.Fatalf("channel = %q, want %q", switched.Channel(), RuntimeClaude)
	}
	if switched.SessionID != "herdr:test-terminal-2" || switched.SessionID == original.SessionID {
		t.Fatalf("new session = %q (old %q)", switched.SessionID, original.SessionID)
	}
	if status, _ := st.FollowupStatus(task.ID, cm.ID); status != "sent" {
		t.Fatalf("followup = %q", status)
	}
	calls, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(calls), "claude") {
		t.Fatalf("the addressed channel did not get a new Herdr tab: %s", calls)
	}
	if strings.Contains(string(calls), "--resume") {
		t.Fatalf("the addressed channel was resumed as a CLI process: %s", calls)
	}

	// The switch becomes the card's session, so the next comment with no
	// address resumes it instead of going back to the first channel.
	plain, err := st.AddComment(task.ID, "继续这一点", "human")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Continue(task.ID, plain.ID, "human"); err != nil {
		t.Fatal(err)
	}
	awaitNoExecution(t, d)
	calls, err = os.ReadFile(sendsLog)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
	last := lines[len(lines)-1]
	if !strings.Contains(last, "test-terminal-2") {
		t.Fatalf("an unaddressed comment left the switched session: %s", last)
	}
	if kept, _ := st.Execution(task.ID); kept.SessionID != switched.SessionID || kept.Channel() != RuntimeClaude {
		t.Fatalf("execution drifted: %+v", kept)
	}
}

// Addressing the channel that already owns the card keeps the conversation.
func TestMentionOnOwningChannelResumes(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls")
	t.Setenv("MENTION_CALLS", log)
	installFakeRuntime(t, RuntimeClaude, `printf '%s\n' "$*" >> "$MENTION_CALLS"
printf 'returned\n'`)

	st := newTestStore(t)
	p, task := projectWithWorkspace(t, st)
	argsLog, sendsLog := installFakeHerdr(t, p.WorkspacePath, "done")
	d := NewDispatcher(st)
	defer func() { d.Close() }()
	if err := d.Dispatch(task.ID, RuntimeClaude, "test"); err != nil {
		t.Fatal(err)
	}
	awaitNoExecution(t, d)
	first, _ := st.Execution(task.ID)

	cm, err := st.AddCommentTo(task.ID, "再补一句", "human", RuntimeClaude)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Continue(task.ID, cm.ID, "human"); err != nil {
		t.Fatal(err)
	}
	awaitNoExecution(t, d)
	calls, err := os.ReadFile(sendsLog)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
	if last := lines[len(lines)-1]; !strings.Contains(last, "send test-terminal-1") {
		t.Fatalf("same channel did not resume: %s", last)
	}
	args, err := os.ReadFile(argsLog)
	if err != nil || strings.Count(string(args), "tab.new") != 1 {
		t.Fatalf("same-channel follow-up unexpectedly opened another Herdr tab: %s (%v)", args, err)
	}
	if kept, _ := st.Execution(task.ID); kept.SessionID != first.SessionID {
		t.Fatalf("session changed: %+v", kept)
	}
}

// A comment with no address on a card that never ran stays refused: only an
// explicit human address may start a session.
func TestUnaddressedCommentWithoutSessionIsRefused(t *testing.T) {
	st := newTestStore(t)
	d := NewDispatcher(st)
	defer func() { d.Close() }()
	_, task := projectWithWorkspace(t, st)
	cm, err := st.AddComment(task.ID, "顺手看一下", "human")
	if err != nil {
		t.Fatal(err)
	}
	err = d.Continue(task.ID, cm.ID, "human")
	if err == nil || !strings.Contains(err.Error(), "没有可接续的原会话") {
		t.Fatalf("continue = %v", err)
	}
}
