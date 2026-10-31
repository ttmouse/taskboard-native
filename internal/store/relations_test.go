package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"taskboard-native/internal/model"
)

func TestParentRelationOneParentAndCycle(t *testing.T) {
	s := openTemp(t)
	cols := mustColumns(t, s, "local")
	parent, err := s.CreateTask("local", cols[0].ID, "父", "", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.CreateTask("local", cols[0].ID, "子", "", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	grand, err := s.CreateTask("local", cols[0].ID, "孙", "", "", "test")
	if err != nil {
		t.Fatal(err)
	}

	res, err := s.AddTaskRelation(child.ID, child.Version, RelationParent, parent.ID, "test")
	if err != nil {
		t.Fatalf("add parent: %v", err)
	}
	if res.Task.Relations.Parent == nil || res.Task.Relations.Parent.ID != parent.ID {
		t.Fatalf("child parent = %+v", res.Task.Relations.Parent)
	}
	parentFresh, err := s.Task(parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(parentFresh.Relations.SubIssues) != 1 || parentFresh.Relations.SubIssues[0].ID != child.ID {
		t.Fatalf("subIssues = %+v", parentFresh.Relations.SubIssues)
	}

	// Replace parent with grand: allowed (one parent).
	child = res.Task
	res, err = s.AddTaskRelation(child.ID, child.Version, RelationParent, grand.ID, "test")
	if err != nil {
		t.Fatalf("replace parent: %v", err)
	}
	if res.Task.Relations.Parent == nil || res.Task.Relations.Parent.ID != grand.ID {
		t.Fatalf("replaced parent = %+v", res.Task.Relations.Parent)
	}

	// Cycle: make grand's parent = child (child→grand→child).
	grandFresh, err := s.Task(grand.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.AddTaskRelation(grandFresh.ID, grandFresh.Version, RelationParent, child.ID, "test")
	if err == nil || !strings.Contains(err.Error(), "环") {
		t.Fatalf("want cycle error, got %v", err)
	}
}

func TestRelationCrossProjectAndSelf(t *testing.T) {
	s := openTemp(t)
	cols := mustColumns(t, s, "local")
	a, err := s.CreateTask("local", cols[0].ID, "A", "", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateProject("other", "test")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateTask(other.ID, other.Columns[0].ID, "B", "", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.AddTaskRelation(a.ID, a.Version, RelationBlocks, b.ID, "test")
	if err == nil || !strings.Contains(err.Error(), "同一项目") {
		t.Fatalf("want cross-project error, got %v", err)
	}
	_, err = s.AddTaskRelation(a.ID, a.Version, RelationRelated, a.ID, "test")
	if err == nil || !strings.Contains(err.Error(), "自身") {
		t.Fatalf("want self error, got %v", err)
	}
}

func TestBlockedByMoveGate(t *testing.T) {
	s := openTemp(t)
	cols := mustColumns(t, s, "local")
	blocker, err := s.CreateTask("local", cols[0].ID, "前置", "", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.CreateTask("local", cols[0].ID, "被挡", "", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.AddTaskRelation(task.ID, task.Version, RelationBlockedBy, blocker.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	task = res.Task
	if len(task.Relations.BlockedBy) != 1 || task.Relations.BlockedBy[0].ID != blocker.ID {
		t.Fatalf("blockedBy = %+v", task.Relations.BlockedBy)
	}

	doing := cols[1].ID // 进行中
	_, err = s.MoveTask(task.ID, task.Version, doing, "", "test")
	if err == nil || !strings.Contains(err.Error(), "前置未完成") {
		t.Fatalf("want gate error into doing, got %v", err)
	}
	review := cols[2].ID
	task, _ = s.Task(task.ID)
	_, err = s.MoveTask(task.ID, task.Version, review, "", "test")
	if err == nil || !strings.Contains(err.Error(), "前置未完成") {
		t.Fatalf("want gate error into review, got %v", err)
	}
	// intake / done always allowed
	if _, err := s.MoveTask(task.ID, task.Version, cols[0].ID, "", "test"); err != nil {
		t.Fatalf("intake move: %v", err)
	}
	task, _ = s.Task(task.ID)
	if _, err := s.MoveTask(task.ID, task.Version, cols[3].ID, "", "test"); err != nil {
		t.Fatalf("done move: %v", err)
	}

	// Finish blocker → gate opens.
	blocker, _ = s.Task(blocker.ID)
	if _, err := s.MoveTask(blocker.ID, blocker.Version, cols[3].ID, "", "test"); err != nil {
		t.Fatalf("finish blocker: %v", err)
	}
	task, _ = s.Task(task.ID)
	moved, err := s.MoveTask(task.ID, task.Version, doing, "", "test")
	if err != nil {
		t.Fatalf("move after blocker done: %v", err)
	}
	if moved.ColumnID != doing {
		t.Fatalf("column = %s", moved.ColumnID)
	}
}



func TestReparentEmitsOldParentUpdate(t *testing.T) {
	s := openTemp(t)
	cols := mustColumns(t, s, "local")
	oldParent, _ := s.CreateTask("local", cols[0].ID, "旧父卡", "", "", "test")
	newParent, _ := s.CreateTask("local", cols[0].ID, "新父卡", "", "", "test")
	child, _ := s.CreateTask("local", cols[0].ID, "子卡", "", "", "test")
	res, err := s.AddTaskRelation(child.ID, child.Version, RelationParent, oldParent.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	var oldParentEvent *model.Task
	s.OnEvent = func(event model.Event) {
		if event.Type == model.EventTaskUpdated && event.ID == oldParent.ID && event.Task != nil {
			copy := *event.Task
			oldParentEvent = &copy
		}
	}
	if _, err := s.AddTaskRelation(child.ID, res.Task.Version, RelationParent, newParent.ID, "test"); err != nil {
		t.Fatal(err)
	}
	if oldParentEvent == nil || len(oldParentEvent.Relations.SubIssues) != 0 {
		t.Fatalf("old parent did not receive relation removal event: %+v", oldParentEvent)
	}
}

func TestAssertUnblocked(t *testing.T) {
	s := openTemp(t)
	cols := mustColumns(t, s, "local")
	blocker, _ := s.CreateTask("local", cols[0].ID, "挡", "", "", "test")
	task, _ := s.CreateTask("local", cols[0].ID, "卡", "", "", "test")
	res, err := s.AddTaskRelation(task.ID, task.Version, RelationBlockedBy, blocker.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	err = s.AssertUnblocked(res.Task.ID)
	var inv *InvalidError
	if !errors.As(err, &inv) || !strings.Contains(err.Error(), "前置未完成") {
		t.Fatalf("want InvalidError 前置未完成, got %v", err)
	}
}

func TestDependencyGateBoundaries(t *testing.T) {
	t.Run("adding blocker to active task is rejected", func(t *testing.T) {
		s := openTemp(t)
		cols := mustColumns(t, s, "local")
		blocker, _ := s.CreateTask("local", cols[0].ID, "未完成前置", "", "", "test")
		active, _ := s.CreateTask("local", cols[1].ID, "进行中", "", "", "test")
		_, err := s.AddTaskRelation(active.ID, active.Version, RelationBlockedBy, blocker.ID, "test")
		var invalidErr *InvalidError
		if !errors.As(err, &invalidErr) || !strings.Contains(err.Error(), "前置未完成") {
			t.Fatalf("add blocker error=%v", err)
		}
		readback, _ := s.Task(active.ID)
		if len(readback.Relations.BlockedBy) != 0 {
			t.Fatalf("relation persisted: %+v", readback.Relations)
		}
	})
	t.Run("restore into active column is gated", func(t *testing.T) {
		s := openTemp(t)
		cols := mustColumns(t, s, "local")
		blocker, _ := s.CreateTask("local", cols[0].ID, "未完成前置", "", "", "test")
		active, _ := s.CreateTask("local", cols[1].ID, "已归档进行中卡", "", "", "test")
		archived, err := s.PatchTask(active.ID, active.Version, TaskPatch{Archived: ptr(true)}, "test")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Execute(context.Background(), `INSERT INTO task_relations(relation_type,source_task_id,target_task_id,created_at) VALUES('blocks',?,?,?)`, blocker.ID, active.ID, timestamp(nowUTC())); err != nil {
			t.Fatal(err)
		}
		_, err = s.PatchTask(active.ID, archived.Version, TaskPatch{Archived: ptr(false)}, "test")
		var invalidErr *InvalidError
		if !errors.As(err, &invalidErr) || !strings.Contains(err.Error(), "前置未完成") {
			t.Fatalf("restore error=%v", err)
		}
	})
	t.Run("archived blockers satisfy the gate", func(t *testing.T) {
		s := openTemp(t)
		cols := mustColumns(t, s, "local")
		blocker, _ := s.CreateTask("local", cols[0].ID, "已归档前置", "", "", "test")
		task, _ := s.CreateTask("local", cols[0].ID, "待推进", "", "", "test")
		res, err := s.AddTaskRelation(task.ID, task.Version, RelationBlockedBy, blocker.ID, "test")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.PatchTask(blocker.ID, blocker.Version, TaskPatch{Archived: ptr(true)}, "test"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.PatchTask(task.ID, res.Task.Version, TaskPatch{ColumnID: &cols[1].ID}, "test"); err != nil {
			t.Fatalf("archived blocker should be treated as closed: %v", err)
		}
	})
}

func ptr[T any](value T) *T { return &value }

func mustColumns(t *testing.T, s *Store, projectID string) []model.Column {
	t.Helper()
	p, err := s.Project(projectID)
	if err != nil {
		t.Fatal(err)
	}
	return p.Columns
}
