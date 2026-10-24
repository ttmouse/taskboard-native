package client_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"taskboard-native/internal/client"
	"taskboard-native/internal/model"
	"taskboard-native/internal/service"
)

// newService starts the real HTTP service on a random port with a fresh
// database, so the client is exercised over the wire and not against a
// stub.
func newService(t *testing.T) (*service.Server, *client.Client) {
	t.Helper()
	srv, err := service.Start("127.0.0.1:0", filepath.Join(t.TempDir(), "board.db"))
	if err != nil {
		t.Fatalf("start service: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	c := client.New(&client.Discover{Addr: srv.Addr}, "test-agent")
	if _, err := c.Health(); err != nil {
		t.Fatalf("health: %v", err)
	}
	return srv, c
}

func TestBoardLifecycle(t *testing.T) {
	_, c := newService(t)

	projects, err := c.Projects()
	if err != nil {
		t.Fatalf("projects: %v", err)
	}
	if len(projects) == 0 {
		t.Fatal("service seeded no project")
	}
	p := projects[0]
	if len(p.Columns) == 0 {
		t.Fatal("project has no columns")
	}

	board, err := c.Board(p.ID)
	if err != nil {
		t.Fatalf("board: %v", err)
	}
	if len(board.Columns) != len(p.Columns) {
		t.Fatalf("board has %d columns, project %d", len(board.Columns), len(p.Columns))
	}
	if board.Project.ID != p.ID {
		t.Fatalf("board project = %q, want %q", board.Project.ID, p.ID)
	}

	col := board.Columns[1]
	task, err := c.CreateTask(p.ID, col.ID, "写客户端测试", "覆盖 HTTP 与 SSE", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if task.ColumnID != col.ID || task.Version != 1 {
		t.Fatalf("created task = %+v", task)
	}

	// The board endpoint sees it immediately.
	board, err = c.Board(p.ID)
	if err != nil {
		t.Fatalf("board after create: %v", err)
	}
	if !hasTask(board.Tasks, task.ID) {
		t.Fatal("board does not list the new task")
	}

	title := "写客户端测试（改）"
	updated, err := c.Update(task.ID, task.Version, &title, nil, nil)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Title != title || updated.Version != 2 {
		t.Fatalf("updated = %+v", updated)
	}

	// A stale version is a conflict carrying the server's copy.
	_, err = c.Update(task.ID, 1, &title, nil, nil)
	ce, ok := err.(*client.ConflictError)
	if !ok {
		t.Fatalf("stale update error = %v (%T), want ConflictError", err, err)
	}
	if ce.Current.Version != updated.Version {
		t.Fatalf("conflict carries v%d, want v%d", ce.Current.Version, updated.Version)
	}

	if _, err := c.AddComment(task.ID, "第一条评论"); err != nil {
		t.Fatalf("comment: %v", err)
	}
	comments, err := c.Comments(task.ID)
	if err != nil {
		t.Fatalf("comments: %v", err)
	}
	if len(comments) != 1 || comments[0].Text != "第一条评论" || comments[0].Author != "test-agent" {
		t.Fatalf("comments = %+v", comments)
	}

	// Moving to the top puts the card first in the target column.
	moved, err := c.Move(task.ID, updated.Version, board.Columns[0].ID, "", true)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if moved.ColumnID != board.Columns[0].ID {
		t.Fatalf("moved into %q, want %q", moved.ColumnID, board.Columns[0].ID)
	}
	board, err = c.Board(p.ID)
	if err != nil {
		t.Fatalf("board after move: %v", err)
	}
	first := board.Tasks[0]
	for _, tk := range board.Tasks {
		if tk.ColumnID == board.Columns[0].ID && tk.Position < first.Position {
			first = tk
		}
	}
	if first.ID != task.ID {
		t.Fatalf("task is not at the top of its column: %q", first.ID)
	}

	archived, err := c.Archive(task.ID, moved.Version)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if !archived.Archived {
		t.Fatal("archived task is not marked archived")
	}
	board, err = c.Board(p.ID)
	if err != nil {
		t.Fatalf("board after archive: %v", err)
	}
	if hasTask(board.Tasks, task.ID) {
		t.Fatal("archived task still on the board")
	}
}

// TestRestoreBringsCardBack checks the undo path over the wire: archive,
// then clear the flag, and the card is live again in its own column.
func TestRestoreBringsCardBack(t *testing.T) {
	_, c := newService(t)
	projects, err := c.Projects()
	if err != nil || len(projects) == 0 {
		t.Fatalf("projects: %v", err)
	}
	p := projects[0]
	task, err := c.CreateTask(p.ID, p.Columns[1].ID, "会被归档再恢复", "", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	archived, err := c.Archive(task.ID, task.Version)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if !archived.Archived {
		t.Fatal("archive did not mark the task")
	}
	board, err := c.Board(p.ID)
	if err != nil {
		t.Fatalf("board: %v", err)
	}
	if hasTask(board.Tasks, task.ID) {
		t.Fatal("archived task still listed")
	}
	restored, err := c.Restore(task.ID, archived.Version)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.Archived {
		t.Fatal("restore left the task archived")
	}
	if restored.ColumnID != p.Columns[1].ID {
		t.Fatalf("restored into %q, want %q", restored.ColumnID, p.Columns[1].ID)
	}
	board, err = c.Board(p.ID)
	if err != nil {
		t.Fatalf("board after restore: %v", err)
	}
	if !hasTask(board.Tasks, task.ID) {
		t.Fatal("restored task is not back on the board")
	}
}

func TestResolveColumn(t *testing.T) {
	_, c := newService(t)
	projects, err := c.Projects()
	if err != nil || len(projects) == 0 {
		t.Fatalf("projects: %v", err)
	}
	p := projects[0]
	byName, err := c.ResolveColumn(p.ID, p.Columns[1].Name)
	if err != nil {
		t.Fatalf("resolve by name: %v", err)
	}
	byID, err := c.ResolveColumn(p.ID, p.Columns[1].ID)
	if err != nil {
		t.Fatalf("resolve by id: %v", err)
	}
	if byName != byID || byName != p.Columns[1].ID {
		t.Fatalf("resolve: name %q, id %q", byName, byID)
	}
	first, err := c.ResolveColumn(p.ID, "")
	if err != nil {
		t.Fatalf("resolve default: %v", err)
	}
	if first != p.Columns[0].ID {
		t.Fatalf("default column = %q, want %q", first, p.Columns[0].ID)
	}
	if _, err := c.ResolveColumn(p.ID, "没有这一列"); err == nil {
		t.Fatal("unknown column resolved")
	}
}

// TestSubscribeSeesWrites checks the SSE path end to end: a write on the
// service reaches a subscriber as an event.
func TestSubscribeSeesWrites(t *testing.T) {
	_, c := newService(t)
	projects, err := c.Projects()
	if err != nil || len(projects) == 0 {
		t.Fatalf("projects: %v", err)
	}
	p := projects[0]

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan model.Event, 16)
	states := make(chan client.StreamState, 16)
	go func() {
		_ = c.Subscribe(ctx, p.ID, func(ev model.Event) {
			select {
			case events <- ev:
			default:
			}
		}, func(st client.StreamState) {
			select {
			case states <- st:
			default:
			}
		})
	}()

	// The stream reports Online once the connection stands.
	deadline := time.After(5 * time.Second)
	for online := false; !online; {
		select {
		case st := <-states:
			online = st == client.Online
		case <-deadline:
			t.Fatal("stream never reported Online")
		}
	}

	if _, err := c.CreateTask(p.ID, p.Columns[0].ID, "来自事件流", "", ""); err != nil {
		t.Fatalf("create: %v", err)
	}

	for {
		select {
		case ev := <-events:
			if ev.Type != model.EventTaskCreated {
				continue
			}
			if ev.Task == nil || ev.Task.Title != "来自事件流" {
				t.Fatalf("event = %+v", ev)
			}
			if ev.By != "test-agent" {
				t.Fatalf("event by = %q", ev.By)
			}
			return
		case <-time.After(5 * time.Second):
			t.Fatal("no task.created event arrived")
		}
	}
}

// TestStreamReconnects checks the reader survives the service going away
// and coming back: it reports Offline, then Online again.
func TestStreamReconnects(t *testing.T) {
	_, c := newService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	states := make(chan client.StreamState, 32)
	go func() {
		_ = c.Subscribe(ctx, "", func(model.Event) {}, func(st client.StreamState) {
			select {
			case states <- st:
			default:
			}
		})
	}()

	waitState(t, states, client.Online)
	// Stop the service the client is talking to, by asking for a stream
	// on a client pointed at a closed port.
	dead := client.New(&client.Discover{Addr: "127.0.0.1:1"}, "test-agent")
	deadStates := make(chan client.StreamState, 32)
	deadCtx, deadCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer deadCancel()
	go func() {
		_ = dead.Subscribe(deadCtx, "", func(model.Event) {}, func(st client.StreamState) {
			select {
			case deadStates <- st:
			default:
			}
		})
	}()
	waitState(t, deadStates, client.Offline)
}

func waitState(t *testing.T, states <-chan client.StreamState, want client.StreamState) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case st := <-states:
			if st == want {
				return
			}
		case <-deadline:
			t.Fatalf("never saw state %v", want)
		}
	}
}

func hasTask(tasks []model.Task, id string) bool {
	for _, t := range tasks {
		if t.ID == id {
			return true
		}
	}
	return false
}
