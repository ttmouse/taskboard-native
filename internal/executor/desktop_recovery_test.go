package executor

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"taskboard-native/internal/model"
)

func TestDesktopRestartResumesWithoutCreatingAgain(t *testing.T) {
	var creates atomic.Int32
	fakeDesktop(t, func(method string, params map[string]any) any {
		switch method {
		case "session.createAndStart":
			creates.Add(1)
			return map[string]any{"accepted": true}
		case "session.getState":
			return map[string]any{"status": "idle"}
		case "session.getSnapshot":
			return map[string]any{"snapshotComplete": true, "items": []any{
				map[string]any{"type": "message", "status": "done", "role": "assistant", "turnId": "t1", "content": map[string]any{"text": "RESUMED_ONCE"}},
				map[string]any{"type": "turn.end", "status": "done", "turnId": "t1"},
			}}
		}
		return nil
	})
	st := newTestStore(t)
	p, task := projectWithWorkspace(t, st)
	_, err := st.MoveTask(task.ID, task.Version, p.Columns[1].ID, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveExecution(model.Execution{TaskID: task.ID, SessionID: "original-session", Status: "disconnected"}); err != nil {
		t.Fatal(err)
	}
	d := NewDispatcher(st, desktopTestConfig(t))
	defer d.Close()
	waitFor(t, "recovered result", func() (bool, string) {
		cur, _ := st.Task(task.ID)
		e, _ := st.Execution(task.ID)
		return e != nil && e.Status == "returned" && cur.ColumnID == p.Columns[1].ID, fmt.Sprint(cur.Comments)
	})
	if creates.Load() != 0 {
		t.Fatal("recovery created another session")
	}
	e, err := st.Execution(task.ID)
	if err != nil || e == nil || e.Status != "returned" || e.SessionID != "original-session" {
		t.Fatalf("%+v %v", e, err)
	}
	cs, _ := st.Comments(task.ID)
	if strings.Contains(fmt.Sprint(cs), "RESUMED_ONCE") {
		t.Fatal(cs)
	}
}

func TestDesktopLegacyReceiptRecoversActualSession(t *testing.T) {
	const original = "aa_ebf99dac28d7d70e_taskboard-b5bc2c2f62af492e51edcdf429bfbce4"
	var calls atomic.Int32
	fakeDesktop(t, func(method string, params map[string]any) any {
		if params["externalSessionId"] != original {
			t.Errorf("lost original desktop identity: %v", params)
		}
		switch method {
		case "session.createAndStart":
			t.Error("legacy upgrade resubmitted task")
			return nil
		case "session.getState":
			return map[string]any{"status": "idle"}
		case "session.getSnapshot":
			if calls.Add(1) == 1 {
				return map[string]any{"sessionId": "taskboard-b5bc2c2f62af492e51edcdf429bfbce4", "nextCursor": "page:1", "snapshotComplete": true, "items": []any{map[string]any{"type": "message", "status": "done", "role": "assistant", "turnId": "t1", "content": map[string]any{"text": "LEGACY_OK"}}}}
			}
			if params["sessionId"] != "taskboard-b5bc2c2f62af492e51edcdf429bfbce4" {
				t.Error("pagination lost platform identity")
			}
			return map[string]any{"snapshotComplete": true, "items": []any{map[string]any{"type": "turn.end", "status": "done", "turnId": "t1"}}}
		}
		return nil
	})
	st := newTestStore(t)
	p, task := projectWithWorkspace(t, st)
	if _, err := st.MoveTask(task.ID, task.Version, p.Columns[1].ID, "", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddComment(task.ID, "已交给 DSH 桌面端。会话 ID："+original, "DSH"); err != nil {
		t.Fatal(err)
	}
	d := NewDispatcher(st, desktopTestConfig(t))
	defer d.Close()
	waitFor(t, "legacy receipt recovery", func() (bool, string) {
		cur, _ := st.Task(task.ID)
		e, _ := st.Execution(task.ID)
		return e != nil && e.Status == "returned" && cur.ColumnID == p.Columns[1].ID, fmt.Sprint(cur)
	})
	e, err := st.Execution(task.ID)
	if err != nil || e == nil || e.SessionID != original || e.Status != "returned" {
		t.Fatalf("%+v %v", e, err)
	}
}
