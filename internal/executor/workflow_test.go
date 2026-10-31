package executor

import (
	"testing"

	"taskboard-native/internal/model"
)

func TestAcceptedFollowupMovesCardIntoDoing(t *testing.T) {
	st := newTestStore(t)
	d := NewDispatcher(st)
	defer d.Close()
	project, task := projectWithWorkspace(t, st)
	review, ok := project.ColumnByRole(model.ColumnRoleReview)
	if !ok {
		t.Fatal("review column missing")
	}
	doing, ok := project.ColumnByRole(model.ColumnRoleDoing)
	if !ok {
		t.Fatal("doing column missing")
	}
	awaiting, err := st.MoveTask(task.ID, task.Version, review.ID, "", "user")
	if err != nil {
		t.Fatal(err)
	}
	d.syncRunningTaskColumn(task.ID, "Codex")

	got, err := st.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ColumnID != doing.ID || got.Version != awaiting.Version+1 {
		t.Fatalf("running card column/version = %q/%d, want doing %q/version %d", got.ColumnID, got.Version, doing.ID, awaiting.Version+1)
	}
}

func TestAcceptedFollowupLeavesCardAlreadyInDoing(t *testing.T) {
	st := newTestStore(t)
	d := NewDispatcher(st)
	defer d.Close()
	project, task := projectWithWorkspace(t, st)
	doing, ok := project.ColumnByRole(model.ColumnRoleDoing)
	if !ok {
		t.Fatal("doing column missing")
	}
	claimed, err := st.MoveTask(task.ID, task.Version, doing.ID, "", "user")
	if err != nil {
		t.Fatal(err)
	}
	d.syncRunningTaskColumn(task.ID, "Codex")

	got, err := st.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ColumnID != doing.ID || got.Version != claimed.Version {
		t.Fatalf("already-running card changed: column/version = %q/%d", got.ColumnID, got.Version)
	}
}
