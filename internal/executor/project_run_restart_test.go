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

func TestReturnedSessionKeepsProjectRunWaitingForCardMove(t *testing.T) {
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

	run, err := st.ProjectRun(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !run.Running() || run.CurrentTaskID != task.ID || !strings.Contains(run.Detail, "等待卡片") {
		t.Fatalf("ProjectRun() = %#v; expected enabled state waiting on the same card", run)
	}
}
