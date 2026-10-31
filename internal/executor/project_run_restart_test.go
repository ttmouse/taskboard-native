package executor

import (
	"strings"
	"testing"

	"taskboard-native/internal/model"
)

func TestServiceCloseKeepsProjectRunEnabled(t *testing.T) {
	st := newTestStore(t)
	project, task := projectWithWorkspace(t, st)
	doing, ok := project.ColumnByRole(model.ColumnRoleDoing)
	if !ok {
		t.Fatal("project has no doing column")
	}
	if _, err := st.MoveTask(task.ID, task.Version, doing.ID, "", "test"); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProjectRun(model.ProjectRun{
		ProjectID: project.ID, Runtime: RuntimeCodex, Status: "running",
		TaskIDs: []string{task.ID}, Cursor: 1, CurrentTaskID: task.ID,
	}); err != nil {
		t.Fatal(err)
	}

	d := NewDispatcher(st)
	d.Close()
	// The in-flight tracker calls this as it unwinds after Close cancels it.
	d.finishProjectCard(task.ID)

	run, err := st.ProjectRun(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !run.Running() {
		t.Fatalf("ProjectRun() = %#v; service shutdown must preserve the enabled switch", run)
	}
}

func TestReturnedSessionMovesCardToReviewAndContinues(t *testing.T) {
	st := newTestStore(t)
	project, task := projectWithWorkspace(t, st)
	doing, ok := project.ColumnByRole(model.ColumnRoleDoing)
	if !ok {
		t.Fatal("project has no doing column")
	}
	if _, err := st.MoveTask(task.ID, task.Version, doing.ID, "", "test"); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveExecution(model.Execution{TaskID: task.ID, Runtime: RuntimeCodex, SessionID: "herdr:t-returned", Status: "returned"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProjectRun(model.ProjectRun{
		ProjectID: project.ID, Runtime: RuntimeCodex, Status: "running",
		TaskIDs: []string{task.ID}, Cursor: 1, CurrentTaskID: task.ID,
	}); err != nil {
		t.Fatal(err)
	}

	d := &Dispatcher{store: st}
	d.finishProjectCard(task.ID)

	moved, err := st.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	review, ok := project.ColumnByRole(model.ColumnRoleReview)
	if !ok || moved.ColumnID != review.ID {
		t.Fatalf("finished card stayed in %q, want review %q", moved.ColumnID, review.ID)
	}
	comments, err := st.Comments(task.ID)
	if err != nil || len(comments) == 0 {
		t.Fatalf("comments = %v, %v; want a service note about the automatic move", comments, err)
	}
	if comments[0].Author != "自动承接" || !strings.Contains(comments[0].Text, "自动移入「等你」") {
		t.Fatalf("comment = %#v; want the 自动承接 move note", comments[0])
	}
	run, err := st.ProjectRun(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !run.Running() || run.CurrentTaskID != "" || !strings.Contains(run.Detail, "等待下一张") {
		t.Fatalf("ProjectRun() = %#v; want the switch enabled and waiting for the next card", run)
	}
}

func TestFailedExecutionMovesCardToReviewAndContinues(t *testing.T) {
	st := newTestStore(t)
	project, task := projectWithWorkspace(t, st)
	doing, ok := project.ColumnByRole(model.ColumnRoleDoing)
	if !ok {
		t.Fatal("project has no doing column")
	}
	if _, err := st.MoveTask(task.ID, task.Version, doing.ID, "", "test"); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveExecution(model.Execution{TaskID: task.ID, Runtime: RuntimeCodex, SessionID: "herdr:t-failed", Status: "failed"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProjectRun(model.ProjectRun{
		ProjectID: project.ID, Runtime: RuntimeCodex, Status: "running",
		TaskIDs: []string{task.ID}, Cursor: 1, CurrentTaskID: task.ID,
	}); err != nil {
		t.Fatal(err)
	}

	d := &Dispatcher{store: st}
	d.finishProjectCard(task.ID)

	moved, err := st.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	review, ok := project.ColumnByRole(model.ColumnRoleReview)
	if !ok || moved.ColumnID != review.ID {
		t.Fatalf("failed card stayed in %q, want review %q", moved.ColumnID, review.ID)
	}
	run, err := st.ProjectRun(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !run.Running() {
		t.Fatalf("ProjectRun() = %#v; a failed execution must not disable the switch anymore", run)
	}
}

func TestApprovalBlockedCardMovesToReviewAndUnblocksProjectRun(t *testing.T) {
	st := newTestStore(t)
	project, task := projectWithWorkspace(t, st)
	doing, ok := project.ColumnByRole(model.ColumnRoleDoing)
	if !ok {
		t.Fatal("project has no doing column")
	}
	if _, err := st.MoveTask(task.ID, task.Version, doing.ID, "", "test"); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveExecution(model.Execution{TaskID: task.ID, Runtime: RuntimeCodex, SessionID: "herdr:t-approval", Status: "waiting_approval"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProjectRun(model.ProjectRun{
		ProjectID: project.ID, Runtime: RuntimeCodex, Status: "running",
		TaskIDs: []string{task.ID}, Cursor: 1, CurrentTaskID: task.ID,
	}); err != nil {
		t.Fatal(err)
	}

	d := &Dispatcher{store: st, inflight: make(map[string]*execution)}
	d.parkProjectCardForApproval(task.ID)

	moved, err := st.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	review, ok := project.ColumnByRole(model.ColumnRoleReview)
	if !ok || moved.ColumnID != review.ID {
		t.Fatalf("approval-blocked card stayed in %q, want review %q", moved.ColumnID, review.ID)
	}
	comments, err := st.Comments(task.ID)
	if err != nil || len(comments) == 0 || !strings.Contains(comments[0].Text, "等待人工审批") {
		t.Fatalf("comments = %#v, %v; want an approval note", comments, err)
	}
	run, err := st.ProjectRun(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !run.Running() || run.CurrentTaskID != "" || !strings.Contains(run.Detail, "等待下一张") {
		t.Fatalf("ProjectRun() = %#v; want approval task unblocked and switch still enabled", run)
	}
	execution, err := st.Execution(task.ID)
	if err != nil || execution == nil || execution.Status != "waiting_approval" || !execution.Active() {
		t.Fatalf("Execution() = %#v, %v; approval wait must keep the per-card writer lock", execution, err)
	}
}

func TestArchivedCurrentCardContinuesProjectRun(t *testing.T) {
	st := newTestStore(t)
	project, task := projectWithWorkspace(t, st)
	doing, ok := project.ColumnByRole(model.ColumnRoleDoing)
	if !ok {
		t.Fatal("project has no doing column")
	}
	moved, err := st.MoveTask(task.ID, task.Version, doing.ID, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	archived, err := st.ArchiveTask(task.ID, moved.Version, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProjectRun(model.ProjectRun{
		ProjectID: project.ID, Runtime: RuntimeCodex, Status: "running",
		TaskIDs: []string{task.ID}, Cursor: 1, CurrentTaskID: task.ID,
	}); err != nil {
		t.Fatal(err)
	}

	d := &Dispatcher{store: st}
	d.NoteProjectRun(archived)

	run, err := st.ProjectRun(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !run.Running() || run.CurrentTaskID != "" || !strings.Contains(run.Detail, "等待下一张") {
		t.Fatalf("ProjectRun() = %#v; archiving the current card must keep the switch on and move on", run)
	}
}

func TestRecoverProjectRunPushesReturnedDoingCardToReview(t *testing.T) {
	st := newTestStore(t)
	project, task := projectWithWorkspace(t, st)
	doing, ok := project.ColumnByRole(model.ColumnRoleDoing)
	if !ok {
		t.Fatal("project has no doing column")
	}
	if _, err := st.MoveTask(task.ID, task.Version, doing.ID, "", "test"); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveExecution(model.Execution{
		TaskID: task.ID, Runtime: RuntimeCodex, SessionID: "herdr:t-returned",
		Status: "returned",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProjectRun(model.ProjectRun{
		ProjectID: project.ID, Runtime: RuntimeCodex, Status: "running",
		TaskIDs: []string{task.ID}, Cursor: 1, CurrentTaskID: task.ID,
	}); err != nil {
		t.Fatal(err)
	}

	d := &Dispatcher{store: st}
	if err := d.recoverProjectRuns(); err != nil {
		t.Fatal(err)
	}

	moved, err := st.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	review, ok := project.ColumnByRole(model.ColumnRoleReview)
	if !ok || moved.ColumnID != review.ID {
		t.Fatalf("recovered card stayed in %q, want review %q", moved.ColumnID, review.ID)
	}
	run, err := st.ProjectRun(project.ID)
	if err != nil || !run.Running() || run.CurrentTaskID != "" {
		t.Fatalf("recovered project run = %#v, %v; want the switch on and the queue moving", run, err)
	}
}

func TestRecoverProjectRunRedispatchesEndedIntakeCard(t *testing.T) {
	st := newTestStore(t)
	project, task := projectWithWorkspace(t, st)
	installFakeHerdr(t, project.WorkspacePath, "working")
	if err := st.SaveExecution(model.Execution{
		TaskID: task.ID, Runtime: RuntimeCodex, SessionID: "herdr:t-returned",
		Status: "returned",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProjectRun(model.ProjectRun{
		ProjectID: project.ID, Runtime: RuntimeCodex, Status: "running",
		TaskIDs: []string{task.ID}, Cursor: 1, CurrentTaskID: task.ID,
	}); err != nil {
		t.Fatal(err)
	}

	d := &Dispatcher{store: st, inflight: make(map[string]*execution)}
	defer d.Close()
	if err := d.recoverProjectRuns(); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "redispatch creates a new session", func() (bool, string) {
		execution, err := st.Execution(task.ID)
		if err != nil {
			return false, err.Error()
		}
		if execution.SessionID == "herdr:t-returned" {
			return false, "the returned session was not replaced by a fresh dispatch"
		}
		return true, ""
	})
	claimed, err := st.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	doing, ok := project.ColumnByRole(model.ColumnRoleDoing)
	if !ok || claimed.ColumnID != doing.ID {
		t.Fatalf("redispatched card stayed in %q, want doing %q", claimed.ColumnID, doing.ID)
	}
	run, err := st.ProjectRun(project.ID)
	if err != nil || !run.Running() || run.CurrentTaskID != task.ID {
		t.Fatalf("recovered project run = %#v, %v; want the run tracking the fresh dispatch", run, err)
	}
}

func TestProjectRunPushesEndedDoingCardInsteadOfRerunning(t *testing.T) {
	st := newTestStore(t)
	project, done := projectWithWorkspace(t, st)
	installFakeHerdr(t, project.WorkspacePath, "done")
	doing, ok := project.ColumnByRole(model.ColumnRoleDoing)
	if !ok {
		t.Fatal("project has no doing column")
	}
	intake, ok := project.ColumnByRole(model.ColumnRoleIntake)
	if !ok {
		t.Fatal("project has no intake column")
	}
	if _, err := st.MoveTask(done.ID, done.Version, doing.ID, "", "test"); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveExecution(model.Execution{TaskID: done.ID, Runtime: RuntimeCodex, SessionID: "herdr:t-old", Status: "returned"}); err != nil {
		t.Fatal(err)
	}
	next, err := st.CreateTask(project.ID, intake.ID, "后续卡片", "", "", "test")
	if err != nil {
		t.Fatal(err)
	}

	d := NewDispatcher(st)
	t.Cleanup(d.Close)
	if _, err := d.StartProjectRun(project.ID, RuntimeCodex, "test"); err != nil {
		t.Fatal(err)
	}

	// The finished doing card is pushed to 等你 without a new session; the
	// fresh intake card is dispatched and then auto-pushed when it returns.
	moved, err := st.Task(done.ID)
	if err != nil {
		t.Fatal(err)
	}
	review, ok := project.ColumnByRole(model.ColumnRoleReview)
	if !ok || moved.ColumnID != review.ID {
		t.Fatalf("finished doing card stayed in %q, want review %q", moved.ColumnID, review.ID)
	}
	execution, err := st.Execution(done.ID)
	if err != nil || execution.SessionID != "herdr:t-old" || execution.Status != "returned" {
		t.Fatalf("finished card was re-dispatched: %#v, %v", execution, err)
	}
	waitFor(t, "fresh card dispatched and cleared", func() (bool, string) {
		current, err := st.Task(next.ID)
		if err != nil {
			return false, err.Error()
		}
		return current.ColumnID == review.ID, "column " + current.ColumnID
	})
	run, err := st.ProjectRun(project.ID)
	if err != nil || !run.Running() || run.CurrentTaskID != "" {
		t.Fatalf("run = %#v, %v; want the switch on with both queues cleared", run, err)
	}
}
