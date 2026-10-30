package store

import (
	"testing"

	"taskboard-native/internal/model"
)

func TestTaskListsAndBoardSnapshotsIncludeExecution(t *testing.T) {
	s := openTemp(t)
	var events []model.Event
	s.OnEvent = func(ev model.Event) { events = append(events, ev) }
	tasks, err := s.TasksByProject("local", false)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	want := model.Execution{
		TaskID: tasks[0].ID, SessionID: "herdr:t4s", Status: "returned",
		Runtime: "codex", Detail: "turn ended", FollowupID: "comment-1",
		PreviousTurnID: "turn-1",
	}
	if err := s.SaveExecution(want); err != nil {
		t.Fatalf("save execution: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("execution save emitted %d events, want one", len(events))
	}
	ev := events[0]
	if ev.Type != model.EventTaskUpdated || ev.ID != want.TaskID || ev.Version != tasks[0].Version || ev.Task == nil || ev.Task.Execution == nil || *ev.Task.Execution != want {
		t.Fatalf("execution event = %+v", ev)
	}
	if err := s.SaveExecution(want); err != nil {
		t.Fatalf("save unchanged execution: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("unchanged execution emitted another event: %d events", len(events))
	}

	assertExecution := func(where string, task model.Task) {
		t.Helper()
		if task.Execution == nil || *task.Execution != want {
			t.Fatalf("%s execution = %+v, want %+v", where, task.Execution, want)
		}
	}

	listed, err := s.TasksByProject("local", false)
	if err != nil {
		t.Fatalf("list tasks with execution: %v", err)
	}
	assertExecution("task list", listed[0])

	board, err := s.Board("local")
	if err != nil {
		t.Fatalf("board: %v", err)
	}
	assertExecution("board", board.Tasks[0])

	snapshot, err := s.Snapshot("local")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	assertExecution("SSE snapshot", snapshot.Boards[0].Tasks[0])
}
