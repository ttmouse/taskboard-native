package executor

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"taskboard-native/internal/model"
)

// shortenHerdrGrace makes the convergence window test-sized and restores the
// production values when the test ends.
func shortenHerdrGrace(t *testing.T, probe, grace time.Duration) {
	t.Helper()
	herdrProbeInterval, herdrUnreachableGrace = probe, grace
	t.Cleanup(func() {
		herdrProbeInterval, herdrUnreachableGrace = 2*time.Second, 2*time.Minute
	})
}

// A Herdr terminal that disappears for good must not keep the card's write
// lock. Before convergence existed the record stayed "disconnected" forever:
// Active() was true, Stalled() was false, so dispatch and comment follow-up
// both refused the card while the UI kept showing 连接异常.
func TestHerdrUnreachableConvergesToDetached(t *testing.T) {
	shortenHerdrGrace(t, 20*time.Millisecond, 120*time.Millisecond)
	st := newTestStore(t)
	p, task := projectWithWorkspace(t, st)
	installFakeHerdr(t, p.WorkspacePath, "unreachable")
	d := NewDispatcher(st)
	defer d.Close()
	if err := d.Dispatch(task.ID, RuntimePi, "herdr-convergence"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Herdr record converged to released", func() (bool, string) {
		run, err := st.Execution(task.ID)
		return err == nil && run != nil && run.Status == "released", fmt.Sprint(run, err)
	})
	run, err := st.Execution(task.ID)
	if err != nil || run == nil {
		t.Fatalf("execution = %+v, err=%v", run, err)
	}
	if run.Active() {
		t.Fatalf("released record still holds the write lock: %+v", run)
	}
	if run.SessionID != "herdr:test-terminal-1" {
		t.Fatalf("convergence dropped the session identity: %+v", run)
	}
	if active, err := st.ActiveExecutions(); err != nil || len(active) != 0 {
		t.Fatalf("active executions after convergence = %+v, err=%v", active, err)
	}
	cur, _ := st.Task(task.ID)
	if cur.ColumnID != p.Columns[0].ID || cur.Version != task.Version {
		t.Fatalf("dispatcher changed the card's workflow: %+v", cur)
	}
	// The lock is released, so a person may dispatch the card again.
	if err := d.Dispatch(task.ID, RuntimeClaude, "herdr-convergence-retry"); err != nil {
		t.Fatalf("redispatch after convergence: %v", err)
	}
}

// A terminal that answers again inside the grace window keeps its live record:
// convergence must not fire on a transient hiccup.
func TestHerdrReachableNeverConverges(t *testing.T) {
	shortenHerdrGrace(t, 20*time.Millisecond, 80*time.Millisecond)
	st := newTestStore(t)
	p, task := projectWithWorkspace(t, st)
	installFakeHerdr(t, p.WorkspacePath, "working")
	d := NewDispatcher(st)
	defer d.Close()
	if err := d.Dispatch(task.ID, RuntimePi, "herdr-grace"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Herdr session tracking", func() (bool, string) {
		run, err := st.Execution(task.ID)
		return err == nil && run != nil && run.Status == "running", fmt.Sprint(run, err)
	})
	// Well past the grace window: a reachable terminal stays tracked.
	time.Sleep(200 * time.Millisecond)
	run, err := st.Execution(task.ID)
	if err != nil || run == nil || run.Status != "running" {
		t.Fatalf("reachable Herdr session converged unexpectedly: %+v, err=%v", run, err)
	}
	if !run.Active() {
		t.Fatalf("running session lost the write lock: %+v", run)
	}
}

// Closing the service must keep the original terminal record for the next
// start instead of converging it to released.
func TestHerdrCloseKeepsRecord(t *testing.T) {
	shortenHerdrGrace(t, 20*time.Millisecond, 80*time.Millisecond)
	st := newTestStore(t)
	p, task := projectWithWorkspace(t, st)
	installFakeHerdr(t, p.WorkspacePath, "unreachable")
	d := NewDispatcher(st)
	if err := d.Dispatch(task.ID, RuntimePi, "herdr-close"); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	waitFor(t, "Herdr probing started", func() (bool, string) {
		run, err := st.Execution(task.ID)
		return err == nil && run != nil, fmt.Sprint(run, err)
	})
	d.Close()
	run, err := st.Execution(task.ID)
	if err != nil || run == nil {
		t.Fatalf("execution = %+v, err=%v", run, err)
	}
	if run.Status == "released" {
		t.Fatalf("shutdown converged the record instead of keeping it: %+v", run)
	}
}

// A service restart re-enters the same grace probe, so records that were frozen
// at "disconnected" by an earlier version converge on their own. No manual
// database surgery is needed to unlock an already-saved card.
func TestHerdrDisconnectedRecordConvergesAfterRestart(t *testing.T) {
	shortenHerdrGrace(t, 20*time.Millisecond, 80*time.Millisecond)
	st := newTestStore(t)
	p, task := projectWithWorkspace(t, st)
	installFakeHerdr(t, p.WorkspacePath, "unreachable")
	if err := st.SaveExecution(model.Execution{
		TaskID:    task.ID,
		SessionID: "herdr:test-terminal-1",
		Status:    "disconnected",
		Detail:    "Herdr terminal 暂不可达；保留原 terminal，不能自动重派",
	}); err != nil {
		t.Fatalf("seed disconnected execution: %v", err)
	}
	d := NewDispatcher(st)
	defer d.Close()
	waitFor(t, "restart converged the disconnected record", func() (bool, string) {
		run, err := st.Execution(task.ID)
		return err == nil && run != nil && run.Status == "released", fmt.Sprint(run, err)
	})
	if active, err := st.ActiveExecutions(); err != nil || len(active) != 0 {
		t.Fatalf("active executions after restart convergence = %+v, err=%v", active, err)
	}
}

// One or two missed probes are a routing blip, not evidence the session is
// gone: the tracker keeps following the same terminal and the record returns to
// running instead of freezing at 连接异常.
func TestHerdrFlapBelowThresholdKeepsTracking(t *testing.T) {
	shortenHerdrGrace(t, 20*time.Millisecond, 2*time.Second)
	st := newTestStore(t)
	p, task := projectWithWorkspace(t, st)
	installFakeHerdr(t, p.WorkspacePath, "working")
	t.Setenv("TASKBOARD_TEST_HERDR_FAIL_FIRST", "2") // below herdrFlapProbes
	d := NewDispatcher(st)
	defer d.Close()
	if err := d.Dispatch(task.ID, RuntimePi, "herdr-flap"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Herdr session recovered from the flap", func() (bool, string) {
		run, err := st.Execution(task.ID)
		return err == nil && run != nil && run.Status == "running", fmt.Sprint(run, err)
	})
	if run, _ := st.Execution(task.ID); run.Status != "running" || run.Detail != "" {
		t.Fatalf("recovered record = %+v", run)
	}
}

// Enough consecutive misses do reach 连接异常 before the grace window closes, so
// a long outage is honest instead of silently reported as 运行中.
func TestHerdrConsecutiveMissesShowDisconnected(t *testing.T) {
	shortenHerdrGrace(t, 20*time.Millisecond, 500*time.Millisecond)
	st := newTestStore(t)
	p, task := projectWithWorkspace(t, st)
	installFakeHerdr(t, p.WorkspacePath, "unreachable")
	d := NewDispatcher(st)
	defer d.Close()
	if err := d.Dispatch(task.ID, RuntimePi, "herdr-conn-lost"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "连接异常 before convergence", func() (bool, string) {
		run, err := st.Execution(task.ID)
		return err == nil && run != nil && run.Status == "disconnected", fmt.Sprint(run, err)
	})
	if run, _ := st.Execution(task.ID); !strings.Contains(run.Detail, "连续") {
		t.Fatalf("disconnect detail = %q", run.Detail)
	}
}
