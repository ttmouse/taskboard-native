package service

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"taskboard-native/internal/model"
)

func start(t *testing.T) *Server {
	t.Helper()
	srv, err := Start(":0", filepath.Join(t.TempDir(), "taskboard.db"))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// Legacy scenarios that exercise task lifecycle explicitly create their
	// own fixture board; a newly opened service now starts projectless.
	status, raw := request(t, srv, http.MethodPost, "/api/projects", "test", map[string]any{"name": "local"})
	if status != http.StatusCreated {
		t.Fatalf("fixture project: %d %s", status, raw)
	}
	project := decode[model.Project](t, raw)
	for i, title := range []string{"欢迎使用 taskboard", "接上 CLI 与 MCP", "检查实时推送"} {
		_, _ = request(t, srv, http.MethodPost, "/api/tasks", "test", map[string]any{"projectId": project.ID, "columnId": project.Columns[i].ID, "title": title, "description": title + "的说明"})
	}
	t.Cleanup(func() {
		if err := srv.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	})
	return srv
}

// request performs one API call with the given identity header.
func request(t *testing.T, srv *Server, method, path, agent string, body any) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if agent != "" {
		req.Header.Set("X-Agent", agent)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, raw
}

// fixtureProjectID resolves the fixture board's id by name: created
// projects no longer receive the literal "local" id.
func fixtureProjectID(t *testing.T, srv *Server) string {
	t.Helper()
	status, raw := request(t, srv, http.MethodGet, "/api/projects", "", nil)
	if status != http.StatusOK {
		t.Fatalf("projects = %d %s", status, raw)
	}
	projects := decode[[]model.Project](t, raw)
	for _, p := range projects {
		if p.Name == "local" {
			return p.ID
		}
	}
	t.Fatalf("fixture project missing: %+v", projects)
	return ""
}

func decode[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return v
}

func TestHealthAndProjects(t *testing.T) {
	srv := start(t)

	status, raw := request(t, srv, http.MethodGet, "/api/health", "", nil)
	if status != http.StatusOK || !strings.Contains(string(raw), `"ok":true`) {
		t.Fatalf("health = %d %s", status, raw)
	}

	status, raw = request(t, srv, http.MethodGet, "/api/projects", "", nil)
	if status != http.StatusOK {
		t.Fatalf("projects = %d %s", status, raw)
	}
	projects := decode[[]model.Project](t, raw)
	if len(projects) != 1 || projects[0].Name != "local" {
		t.Fatalf("projects = %+v", projects)
	}
	if len(projects[0].Columns) != 4 || projects[0].Columns[0].Name != "待办" {
		t.Fatalf("columns = %+v", projects[0].Columns)
	}
	if projects[0].CreatedAt.IsZero() {
		t.Fatalf("createdAt missing")
	}
}

func TestBoardIsProjectScoped(t *testing.T) {
	srv := start(t)

	projectID := fixtureProjectID(t, srv)
	status, raw := request(t, srv, http.MethodGet, "/api/projects/"+projectID+"/board", "", nil)
	if status != http.StatusOK {
		t.Fatalf("board = %d %s", status, raw)
	}
	board := decode[Board](t, raw)
	if board.Project.ID != projectID || len(board.Columns) != 4 || len(board.Tasks) != 3 {
		t.Fatalf("board = %+v", board)
	}
	if len(board.Tasks) != 3 {
		t.Fatalf("tasks = %d, want 3 seeded", len(board.Tasks))
	}

	if status, raw = request(t, srv, http.MethodGet, "/api/projects/missing/board", "", nil); status != http.StatusNotFound {
		t.Fatalf("missing board = %d %s, want 404", status, raw)
	}

	// Tasks list requires projectId and honours the scope.
	for _, path := range []string{"/api/tasks", "/api/tasks?projectId="} {
		if status, raw = request(t, srv, http.MethodGet, path, "", nil); status != http.StatusBadRequest {
			t.Fatalf("%s = %d %s, want 400", path, status, raw)
		}
	}
	status, raw = request(t, srv, http.MethodGet, "/api/tasks?projectId="+projectID, "", nil)
	tasks := decode[[]model.Task](t, raw)
	if status != http.StatusOK || len(tasks) != 3 {
		t.Fatalf("tasks = %d %s", status, raw)
	}
	for _, task := range tasks {
		if task.ProjectID != projectID {
			t.Fatalf("task leaked from another project: %+v", task)
		}
	}
	if status, raw = request(t, srv, http.MethodGet, "/api/tasks?projectId=nope", "", nil); status != http.StatusNotFound {
		t.Fatalf("unknown project tasks = %d %s, want 404", status, raw)
	}
}

func TestTaskListFiltersPaginationAndSummary(t *testing.T) {
	srv := start(t)
	projectID := fixtureProjectID(t, srv)
	_, raw := request(t, srv, http.MethodGet, "/api/projects/"+projectID+"/board", "", nil)
	board := decode[Board](t, raw)
	todo, doing := board.Columns[0].ID, board.Columns[1].ID

	create := func(column, title, description, assignee string) model.Task {
		t.Helper()
		status, raw := request(t, srv, http.MethodPost, "/api/tasks", "task-list-test", map[string]any{
			"projectId": projectID, "columnId": column, "title": title, "description": description, "assignee": assignee,
		})
		if status != http.StatusCreated {
			t.Fatalf("create %q = %d %s", title, status, raw)
		}
		return decode[model.Task](t, raw)
	}

	first := create(todo, "Alice one", "private body marker", "alice")
	second := create(todo, "Alice two", "second body", "alice")
	_ = create(doing, "Alice other column", "other body", "alice")
	_ = create(todo, "Bob", "bob body", "bob")
	status, raw := request(t, srv, http.MethodPost, "/api/tasks/"+first.ID+"/comments", "commenter", map[string]any{"text": "private comment marker"})
	if status != http.StatusCreated {
		t.Fatalf("create comment = %d %s", status, raw)
	}

	status, raw = request(t, srv, http.MethodGet, "/api/tasks?projectId="+fixtureProjectID(t, srv)+"&columnId="+todo+"&assignee=alice&limit=1&offset=1", "", nil)
	page := decode[[]model.Task](t, raw)
	if status != http.StatusOK || len(page) != 1 || page[0].ID != second.ID {
		t.Fatalf("filtered page = %d %s", status, raw)
	}

	status, raw = request(t, srv, http.MethodGet, "/api/tasks?projectId="+fixtureProjectID(t, srv)+"&columnId="+todo+"&assignee=missing", "", nil)
	if got := decode[[]model.Task](t, raw); status != http.StatusOK || len(got) != 0 || !strings.Contains(string(raw), "[]") {
		t.Fatalf("empty filtered result = %d %s", status, raw)
	}
	status, raw = request(t, srv, http.MethodGet, "/api/tasks?projectId="+fixtureProjectID(t, srv)+"&columnId="+todo+"&assignee=alice&limit=1&offset=20", "", nil)
	if got := decode[[]model.Task](t, raw); status != http.StatusOK || len(got) != 0 {
		t.Fatalf("out-of-range page = %d %s", status, raw)
	}

	status, raw = request(t, srv, http.MethodGet, "/api/tasks?projectId="+fixtureProjectID(t, srv)+"&columnId="+todo+"&assignee=alice&summary=true", "", nil)
	var summaries []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &summaries); err != nil {
		t.Fatalf("decode summary %s: %v", raw, err)
	}
	if status != http.StatusOK || len(summaries) != 2 {
		t.Fatalf("summary list = %d %s", status, raw)
	}
	wantKeys := map[string]bool{"id": true, "number": true, "projectId": true, "columnId": true, "title": true, "version": true, "updatedAt": true, "updatedBy": true, "assignee": true}
	for _, summary := range summaries {
		if len(summary) != len(wantKeys) {
			t.Fatalf("summary contains unexpected fields: %s", raw)
		}
		for key := range summary {
			if !wantKeys[key] {
				t.Fatalf("summary contains unexpected field %q: %s", key, raw)
			}
		}
	}
	if bytes.Contains(raw, []byte("private body marker")) || bytes.Contains(raw, []byte("private comment marker")) || bytes.Contains(raw, []byte("description")) || bytes.Contains(raw, []byte("comments")) {
		t.Fatalf("summary leaked task body or comments: %s", raw)
	}

	status, raw = request(t, srv, http.MethodGet, "/api/tasks/"+first.ID, "", nil)
	detail := decode[model.Task](t, raw)
	if status != http.StatusOK || detail.Description != "private body marker" || len(detail.Comments) != 1 || detail.Comments[0].Text != "private comment marker" {
		t.Fatalf("single-task detail = %d %+v", status, detail)
	}

	// The no-filter list remains an unpaginated array of full tasks.
	status, raw = request(t, srv, http.MethodGet, "/api/tasks?projectId="+fixtureProjectID(t, srv), "", nil)
	all := decode[[]model.Task](t, raw)
	if status != http.StatusOK || len(all) != 7 {
		t.Fatalf("legacy full list = %d %s", status, raw)
	}
	if all[0].ID != board.Tasks[0].ID || all[0].Description == "" {
		t.Fatalf("legacy list order or full task shape changed: %+v", all[0])
	}
}

func TestTaskListRejectsInvalidFiltersAndForeignColumns(t *testing.T) {
	srv := start(t)
	cases := []string{
		"?projectId=" + fixtureProjectID(t, srv) + "&columnId=",
		"?projectId=" + fixtureProjectID(t, srv) + "&limit=",
		"?projectId=" + fixtureProjectID(t, srv) + "&limit=0",
		"?projectId=" + fixtureProjectID(t, srv) + "&limit=201",
		"?projectId=" + fixtureProjectID(t, srv) + "&limit=abc",
		"?projectId=" + fixtureProjectID(t, srv) + "&offset=0",
		"?projectId=" + fixtureProjectID(t, srv) + "&limit=1&offset=",
		"?projectId=" + fixtureProjectID(t, srv) + "&limit=1&offset=-1",
		"?projectId=" + fixtureProjectID(t, srv) + "&summary=TRUE",
		"?projectId=" + fixtureProjectID(t, srv) + "&summary=1",
		"?projectId=" + fixtureProjectID(t, srv) + "&columnId=x&columnId=y",
		"?projectId=" + fixtureProjectID(t, srv) + "&assignee=a&assignee=b",
		"?projectId=" + fixtureProjectID(t, srv) + "&limit=1&limit=2",
		"?projectId=" + fixtureProjectID(t, srv) + "&summary=true&summary=false",
	}
	for _, path := range cases {
		if status, raw := request(t, srv, http.MethodGet, "/api/tasks"+path, "", nil); status != http.StatusBadRequest {
			t.Errorf("%s = %d %s, want 400", path, status, raw)
		}
	}

	status, raw := request(t, srv, http.MethodPost, "/api/projects", "test", map[string]any{"name": "Other project"})
	if status != http.StatusCreated {
		t.Fatalf("create other project = %d %s", status, raw)
	}
	other := decode[model.Project](t, raw)
	foreignColumn := other.Columns[0].ID
	for _, columnID := range []string{"unknown-column", foreignColumn} {
		path := "/api/tasks?projectId=" + fixtureProjectID(t, srv) + "&columnId=" + columnID
		if status, raw := request(t, srv, http.MethodGet, path, "", nil); status != http.StatusBadRequest {
			t.Fatalf("foreign/unknown column %q = %d %s, want 400", columnID, status, raw)
		}
	}
}

func TestTaskLifecycle(t *testing.T) {
	srv := start(t)
	_, raw := request(t, srv, http.MethodGet, "/api/projects/"+fixtureProjectID(t, srv)+"/board", "", nil)
	board := decode[Board](t, raw)
	todo, doing := board.Columns[0].ID, board.Columns[1].ID

	// Create.
	status, raw := request(t, srv, http.MethodPost, "/api/tasks", "cli-test", map[string]any{
		"projectId": fixtureProjectID(t, srv), "columnId": todo, "title": "新卡片", "description": "正文",
	})
	if status != http.StatusCreated {
		t.Fatalf("create = %d %s", status, raw)
	}
	task := decode[model.Task](t, raw)
	if task.Title != "新卡片" || task.ColumnID != todo || task.Version != 1 || task.UpdatedBy != "cli-test" {
		t.Fatalf("created = %+v", task)
	}

	// Create without a column lands in the first column.
	status, raw = request(t, srv, http.MethodPost, "/api/tasks", "local", map[string]any{
		"projectId": fixtureProjectID(t, srv), "title": "默认列",
	})
	if status != http.StatusCreated || decode[model.Task](t, raw).ColumnID != todo {
		t.Fatalf("default column create = %d %s", status, raw)
	}

	// Fetch.
	status, raw = request(t, srv, http.MethodGet, "/api/tasks/"+task.ID, "", nil)
	if status != http.StatusOK || decode[model.Task](t, raw).ID != task.ID {
		t.Fatalf("get = %d %s", status, raw)
	}
	if status, raw = request(t, srv, http.MethodGet, "/api/tasks/nope", "", nil); status != http.StatusNotFound {
		t.Fatalf("missing task = %d %s", status, raw)
	}

	// Patch title.
	status, raw = request(t, srv, http.MethodPatch, "/api/tasks/"+task.ID, "mcp", map[string]any{
		"version": task.Version, "title": "改名", "assignee": "linus",
	})
	if status != http.StatusOK {
		t.Fatalf("patch = %d %s", status, raw)
	}
	patched := decode[model.Task](t, raw)
	if patched.Title != "改名" || patched.Assignee != "linus" || patched.Version != 2 || patched.UpdatedBy != "mcp" {
		t.Fatalf("patched = %+v", patched)
	}

	// Move into another column.
	status, raw = request(t, srv, http.MethodPatch, "/api/tasks/"+task.ID, "mcp", map[string]any{
		"version": patched.Version, "columnId": doing,
	})
	if status != http.StatusOK {
		t.Fatalf("move = %d %s", status, raw)
	}
	moved := decode[model.Task](t, raw)
	if moved.ColumnID != doing || moved.Version != 3 {
		t.Fatalf("moved = %+v", moved)
	}

	// Comments.
	status, raw = request(t, srv, http.MethodPost, "/api/tasks/"+task.ID+"/comments", "mcp", map[string]any{"text": "第一条评论"})
	if status != http.StatusCreated {
		t.Fatalf("comment = %d %s", status, raw)
	}
	comment := decode[model.Comment](t, raw)
	if comment.Author != "mcp" || comment.Text != "第一条评论" {
		t.Fatalf("comment = %+v", comment)
	}
	status, raw = request(t, srv, http.MethodGet, "/api/tasks/"+task.ID+"/comments", "", nil)
	comments := decode[[]model.Comment](t, raw)
	if status != http.StatusOK || len(comments) != 1 {
		t.Fatalf("comments = %d %s", status, raw)
	}

	// Archive hides the card from the board but keeps it addressable.
	status, raw = request(t, srv, http.MethodPost, "/api/tasks/"+task.ID+"/archive", "mcp", map[string]any{"version": moved.Version})
	if status != http.StatusOK || !decode[model.Task](t, raw).Archived {
		t.Fatalf("archive = %d %s", status, raw)
	}
	_, raw = request(t, srv, http.MethodGet, "/api/projects/"+fixtureProjectID(t, srv)+"/board", "", nil)
	for _, listed := range decode[Board](t, raw).Tasks {
		if listed.ID == task.ID {
			t.Fatalf("archived card still on board")
		}
	}
	status, raw = request(t, srv, http.MethodGet, "/api/tasks?projectId="+fixtureProjectID(t, srv)+"&includeArchived=true", "", nil)
	found := false
	for _, listed := range decode[[]model.Task](t, raw) {
		if listed.ID == task.ID && listed.Archived {
			found = true
		}
	}
	if status != http.StatusOK || !found {
		t.Fatalf("archived listing = %d %s", status, raw)
	}
}

func TestValidationErrors(t *testing.T) {
	srv := start(t)
	cases := []struct {
		name string
		path string
		body any
		want int
	}{
		{"missing projectId", "/api/tasks", map[string]any{"title": "x"}, http.StatusBadRequest},
		{"missing title", "/api/tasks", map[string]any{"projectId": fixtureProjectID(t, srv)}, http.StatusBadRequest},
		{"unknown project", "/api/tasks", map[string]any{"projectId": "nope", "title": "x"}, http.StatusNotFound},
		{"unknown column", "/api/tasks", map[string]any{"projectId": fixtureProjectID(t, srv), "title": "x", "columnId": "nope"}, http.StatusBadRequest},
		{"malformed json", "/api/tasks", json.RawMessage(`{"projectId":`), http.StatusBadRequest},
		{"wrong type", "/api/tasks", map[string]any{"projectId": fixtureProjectID(t, srv), "title": 42}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		body := tc.body
		if raw, ok := body.(json.RawMessage); ok {
			status, _ := rawRequest(t, srv, http.MethodPost, tc.path, []byte(raw))
			if status != tc.want {
				t.Fatalf("%s = %d, want %d", tc.name, status, tc.want)
			}
			continue
		}
		if status, _ := request(t, srv, http.MethodPost, tc.path, "", body); status != tc.want {
			t.Fatalf("%s = %d, want %d", tc.name, status, tc.want)
		}
	}

	// Unknown fields are ignored so a client may echo a whole Task back.
	if status, raw := request(t, srv, http.MethodPost, "/api/tasks", "", map[string]any{
		"projectId": fixtureProjectID(t, srv), "title": "带多余字段", "position": 5, "version": 9, "archived": true,
	}); status != http.StatusCreated {
		t.Fatalf("create with extra fields = %d %s", status, raw)
	} else if task := decode[model.Task](t, raw); task.Position <= 0 || task.Version != 1 || task.Archived {
		t.Fatalf("extra fields leaked into the task: %+v", task)
	}

	// version is required on patch and archive.
	status, raw := request(t, srv, http.MethodPost, "/api/tasks", "", map[string]any{"projectId": fixtureProjectID(t, srv), "title": "v"})
	task := decode[model.Task](t, raw)
	if status != http.StatusCreated {
		t.Fatalf("create = %d %s", status, raw)
	}
	for _, path := range []string{"/api/tasks/" + task.ID} {
		if status, raw := request(t, srv, http.MethodPatch, path, "", map[string]any{"title": "x"}); status != http.StatusBadRequest {
			t.Fatalf("patch without version = %d %s", status, raw)
		}
	}
	if status, raw := request(t, srv, http.MethodPost, "/api/tasks/"+task.ID+"/archive", "", map[string]any{}); status != http.StatusBadRequest {
		t.Fatalf("archive without version = %d %s", status, raw)
	}
	if status, raw := request(t, srv, http.MethodPost, "/api/tasks/"+task.ID+"/comments", "", map[string]any{}); status != http.StatusBadRequest {
		t.Fatalf("empty comment = %d %s", status, raw)
	}
	if status, raw := request(t, srv, http.MethodPost, "/api/tasks/nope/comments", "", map[string]any{"text": "x"}); status != http.StatusNotFound {
		t.Fatalf("comment on missing task = %d %s", status, raw)
	}
}

func rawRequest(t *testing.T, srv *Server, method, path string, body []byte) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func TestVersionConflictReturnsCurrentTask(t *testing.T) {
	srv := start(t)
	status, raw := request(t, srv, http.MethodPost, "/api/tasks", "a", map[string]any{"projectId": fixtureProjectID(t, srv), "title": "冲突"})
	task := decode[model.Task](t, raw)
	if status != http.StatusCreated {
		t.Fatalf("create = %d %s", status, raw)
	}

	status, raw = request(t, srv, http.MethodPatch, "/api/tasks/"+task.ID, "a", map[string]any{"version": task.Version, "title": "第一次"})
	if status != http.StatusOK {
		t.Fatalf("first patch = %d %s", status, raw)
	}

	status, raw = request(t, srv, http.MethodPatch, "/api/tasks/"+task.ID, "b", map[string]any{"version": task.Version, "title": "过期"})
	if status != http.StatusConflict {
		t.Fatalf("stale patch = %d %s, want 409", status, raw)
	}
	var conflict struct {
		Error string     `json:"error"`
		Task  model.Task `json:"task"`
	}
	if err := json.Unmarshal(raw, &conflict); err != nil {
		t.Fatalf("decode conflict: %v", err)
	}
	if conflict.Error != "version_conflict" || conflict.Task.Version != 2 || conflict.Task.Title != "第一次" {
		t.Fatalf("conflict body = %+v", conflict)
	}

	if status, raw := request(t, srv, http.MethodPost, "/api/tasks/"+task.ID+"/archive", "b", map[string]any{"version": 1}); status != http.StatusConflict {
		t.Fatalf("stale archive = %d %s, want 409", status, raw)
	}
}

// sseReader subscribes to /api/events and exposes parsed messages.
type sseReader struct {
	resp      *http.Response
	events    chan model.Event
	snapshots chan model.BoardSnapshot
	raw       chan string
}

func subscribe(t *testing.T, srv *Server, query string) *sseReader {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/events"+query, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("subscribe status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}
	s := &sseReader{resp: resp, events: make(chan model.Event, 16), snapshots: make(chan model.BoardSnapshot, 1), raw: make(chan string, 32)}
	go func() {
		defer close(s.events)
		scanner := bufio.NewScanner(resp.Body)
		var eventType, data string
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case line == "":
				if eventType != "" && data != "" {
					var ev model.Event
					if err := json.Unmarshal([]byte(data), &ev); err == nil {
						ev.Type = eventType
						if ev.Type == model.EventBoardSnapshot && ev.Snapshot != nil {
							s.snapshots <- *ev.Snapshot
						} else {
							s.events <- ev
						}
					}
				}
				eventType, data = "", ""
			case strings.HasPrefix(line, "event: "):
				eventType = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = strings.TrimPrefix(line, "data: ")
			default:
				select {
				case s.raw <- line:
				default:
				}
			}
		}
	}()
	t.Cleanup(func() { resp.Body.Close() })
	return s
}

func nextEvent(t *testing.T, s *sseReader) model.Event {
	t.Helper()
	select {
	case ev, ok := <-s.events:
		if !ok {
			t.Fatalf("event stream closed")
		}
		return ev
	case <-time.After(5 * time.Second):
		t.Fatalf("no event within 5s")
	}
	return model.Event{}
}

func TestSSEEvents(t *testing.T) {
	srv := start(t)
	stream := subscribe(t, srv, "")
	// Wait for the post-snapshot comment so the subscription is ready before
	// the write happens. The preceding snapshot also carries an SSE id line.
	deadline := time.After(5 * time.Second)
	for {
		select {
		case line := <-stream.raw:
			if line == ": connected" {
				goto connected
			}
		case <-deadline:
			t.Fatalf("stream did not open")
		}
	}
connected:

	status, raw := request(t, srv, http.MethodPost, "/api/tasks", "cli", map[string]any{"projectId": fixtureProjectID(t, srv), "title": "事件卡"})
	task := decode[model.Task](t, raw)
	if status != http.StatusCreated {
		t.Fatalf("create = %d %s", status, raw)
	}
	created := nextEvent(t, stream)
	if created.Type != model.EventTaskCreated || created.ID != task.ID || created.By != "cli" || created.Task == nil {
		t.Fatalf("created event = %+v", created)
	}
	if created.Seq <= 0 || created.ProjectID == "" {
		t.Fatalf("created event metadata = %+v", created)
	}

	_, raw = request(t, srv, http.MethodPatch, "/api/tasks/"+task.ID, "mcp", map[string]any{"version": task.Version, "title": "改"})
	updated := decode[model.Task](t, raw)
	ev := nextEvent(t, stream)
	if ev.Type != model.EventTaskUpdated || ev.Version != updated.Version {
		t.Fatalf("updated event = %+v", ev)
	}

	execution := model.Execution{TaskID: task.ID, SessionID: "herdr:terminal-1", Runtime: "codex", Status: "running"}
	if err := srv.store.SaveExecution(execution); err != nil {
		t.Fatalf("save execution: %v", err)
	}
	executionEvent := nextEvent(t, stream)
	if executionEvent.Type != model.EventTaskUpdated || executionEvent.Version != updated.Version || executionEvent.Task == nil || executionEvent.Task.Execution == nil || *executionEvent.Task.Execution != execution {
		t.Fatalf("execution event = %+v", executionEvent)
	}

	// A rejected write (unknown column) must not push an event.
	status, raw = request(t, srv, http.MethodPatch, "/api/tasks/"+task.ID, "mcp", map[string]any{"version": updated.Version, "columnId": "nonexistent"})
	if status != http.StatusBadRequest {
		t.Fatalf("unknown column patch = %d %s", status, raw)
	}
	select {
	case stray := <-stream.events:
		t.Fatalf("rejected write emitted %+v", stray)
	case <-time.After(200 * time.Millisecond):
	}

	_, raw = request(t, srv, http.MethodGet, "/api/projects/"+fixtureProjectID(t, srv)+"/board", "", nil)
	board := decode[Board](t, raw)
	target := board.Columns[1].ID
	_, raw = request(t, srv, http.MethodPatch, "/api/tasks/"+task.ID, "gui", map[string]any{"version": updated.Version, "columnId": target})
	movedTask := decode[model.Task](t, raw)
	moved := nextEvent(t, stream)
	if moved.Type != model.EventTaskMoved || moved.Column == nil {
		t.Fatalf("move event = %+v", moved)
	}
	if moved.Column.FromID != board.Columns[0].ID || moved.Column.ToID != target || moved.Version != movedTask.Version {
		t.Fatalf("move event column = %+v", moved.Column)
	}
	if moved.By != "gui" {
		t.Fatalf("move event by = %q", moved.By)
	}

	_, _ = request(t, srv, http.MethodPost, "/api/tasks/"+task.ID+"/comments", "mcp", map[string]any{"text": "评论"})
	commented := nextEvent(t, stream)
	if commented.Type != model.EventCommentAdded || commented.Comment == nil || commented.Comment.Text != "评论" {
		t.Fatalf("comment event = %+v", commented)
	}

	_, _ = request(t, srv, http.MethodPost, "/api/tasks/"+task.ID+"/archive", "mcp", map[string]any{"version": movedTask.Version})
	archived := nextEvent(t, stream)
	if archived.Type != model.EventTaskArchived {
		t.Fatalf("archive event = %+v", archived)
	}
}

func TestSSEReconnectSnapshotClosesEventGap(t *testing.T) {
	srv := start(t)
	status, raw := request(t, srv, http.MethodPost, "/api/tasks", "cli", map[string]any{"projectId": fixtureProjectID(t, srv), "title": "断线期间创建"})
	if status != http.StatusCreated {
		t.Fatalf("create during disconnect = %d %s", status, raw)
	}
	want := decode[model.Task](t, raw)
	stream := subscribe(t, srv, "")
	select {
	case snapshot := <-stream.snapshots:
		found := false
		for _, board := range snapshot.Boards {
			for _, task := range board.Tasks {
				if task.ID == want.ID {
					found = true
				}
			}
		}
		if !found {
			t.Fatalf("reconnect snapshot seq=%d did not contain task %s", snapshot.Seq, want.ID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reconnect did not send a board snapshot")
	}

	status, raw = request(t, srv, http.MethodPost, "/api/tasks", "cli", map[string]any{"projectId": fixtureProjectID(t, srv), "title": "连接后创建"})
	if status != http.StatusCreated {
		t.Fatalf("create after reconnect = %d %s", status, raw)
	}
	created := decode[model.Task](t, raw)
	if ev := nextEvent(t, stream); ev.Type != model.EventTaskCreated || ev.ID != created.ID || ev.Seq <= 0 {
		t.Fatalf("incremental event after snapshot = %+v", ev)
	}
}

func TestSSEProjectFilter(t *testing.T) {
	srv := start(t)
	_, raw := request(t, srv, http.MethodGet, "/api/tasks?projectId="+fixtureProjectID(t, srv), "", nil)
	_ = decode[[]model.Task](t, raw)

	fixtureID := fixtureProjectID(t, srv)
	scoped := subscribe(t, srv, "?projectId="+fixtureID)
	other := subscribe(t, srv, "?projectId=other")
	for _, s := range []*sseReader{scoped, other} {
		select {
		case <-s.raw:
		case <-time.After(5 * time.Second):
			t.Fatalf("stream did not open")
		}
	}

	request(t, srv, http.MethodPost, "/api/tasks", "cli", map[string]any{"projectId": fixtureProjectID(t, srv), "title": "过滤"})

	select {
	case ev := <-scoped.events:
		if ev.ProjectID != fixtureID || ev.Type != model.EventTaskCreated {
			t.Fatalf("scoped event = %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("project-scoped subscriber got no event")
	}
	select {
	case ev := <-other.events:
		t.Fatalf("other-project subscriber received %+v", ev)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestSlowHookAndGracefulStop(t *testing.T) {
	t.Setenv("TASKBOARD_SLOW", "150ms")
	srv, err := StartOptions(Options{Addr: ":0", DBPath: filepath.Join(t.TempDir(), "taskboard.db")})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer srv.Close()

	begin := time.Now()
	if status, _ := request(t, srv, http.MethodGet, "/api/health", "", nil); status != http.StatusOK {
		t.Fatalf("health status = %d", status)
	}
	if elapsed := time.Since(begin); elapsed < 150*time.Millisecond {
		t.Fatalf("slow hook not applied: %v", elapsed)
	}

	// An open SSE stream must not block Close.
	stream := subscribe(t, srv, "")
	select {
	case <-stream.raw:
	case <-time.After(5 * time.Second):
		t.Fatalf("stream did not open")
	}
	done := make(chan error, 1)
	go func() { done <- srv.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Close blocked on an open SSE stream")
	}
	select {
	case <-srv.Done():
	case <-time.After(time.Second):
		t.Fatalf("Done not closed")
	}
	if _, err := http.Get(srv.URL + "/api/health"); err == nil {
		t.Fatalf("service still answering after Close")
	}
}

func TestPortAndDefaults(t *testing.T) {
	t.Setenv("TASKBOARD_ADDR", "")
	t.Setenv("TASKBOARD_DB", filepath.Join(t.TempDir(), "override.db"))
	t.Setenv("TASKBOARD_SLOW", "")
	srv, err := Start(":0", "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer srv.Close()
	if srv.Port == 0 || srv.Addr == "" || srv.URL != "http://"+srv.Addr {
		t.Fatalf("server identity = %+v", srv)
	}
	if !strings.HasSuffix(srv.DBPath, "override.db") {
		t.Fatalf("db path = %q, want TASKBOARD_DB override", srv.DBPath)
	}
	if got := DefaultListenAddr(); got != "127.0.0.1:47826" {
		t.Fatalf("DefaultListenAddr = %q", got)
	}
	t.Setenv("TASKBOARD_ADDR", "127.0.0.1:12345")
	if got := DefaultListenAddr(); got != "127.0.0.1:12345" {
		t.Fatalf("DefaultListenAddr with override = %q", got)
	}

	// Without TASKBOARD_DB the database lives under a fixed product folder,
	// not under the test binary's name.
	t.Setenv("TASKBOARD_DB", "")
	path, err := DefaultDBPath()
	if err != nil {
		t.Fatalf("DefaultDBPath: %v", err)
	}
	if filepath.Base(path) != DefaultDBFile {
		t.Fatalf("db file = %q, want %q", filepath.Base(path), DefaultDBFile)
	}
	if dir := filepath.Base(filepath.Dir(path)); dir != AppName {
		t.Fatalf("db dir = %q, want %q", dir, AppName)
	}
}

func TestSSEPing(t *testing.T) {
	srv := start(t)
	srv.ping = 100 * time.Millisecond
	stream := subscribe(t, srv, "")
	deadline := time.After(5 * time.Second)
	pings := 0
	for pings == 0 {
		select {
		case line := <-stream.raw:
			if line == ": ping" {
				pings++
			}
		case <-deadline:
			t.Fatalf("no ping within 5s")
		}
	}
	// A message plus a ping arrives on the wire.
	request(t, srv, http.MethodPost, "/api/tasks", "cli", map[string]any{"projectId": fixtureProjectID(t, srv), "title": "ping 卡"})
	select {
	case ev, ok := <-stream.events:
		if !ok {
			t.Fatalf("stream closed after ping")
		}
		if ev.Type != model.EventTaskCreated {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no event after ping")
	}
}

func TestCreateProject(t *testing.T) {
	srv := start(t)
	stream := subscribe(t, srv, "")
	select {
	case <-stream.raw:
	case <-time.After(5 * time.Second):
		t.Fatalf("stream did not open")
	}

	status, raw := request(t, srv, http.MethodPost, "/api/projects", "gui", map[string]any{"name": "工作台"})
	if status != http.StatusCreated {
		t.Fatalf("create project = %d %s", status, raw)
	}
	project := decode[model.Project](t, raw)
	if project.Name != "工作台" || len(project.Columns) != 4 || project.ID == "local" || project.WorkspacePath != "" {
		t.Fatalf("project = %+v", project)
	}

	ev := nextEvent(t, stream)
	if ev.Type != model.EventProjectChange || ev.ID != project.ID || ev.ProjectID != project.ID || ev.By != "gui" {
		t.Fatalf("project event = %+v", ev)
	}

	status, raw = request(t, srv, http.MethodGet, "/api/projects", "", nil)
	projects := decode[[]model.Project](t, raw)
	if status != http.StatusOK || len(projects) != 2 || projects[1].ID != project.ID {
		t.Fatalf("projects = %d %s", status, raw)
	}
	// The new project has an empty board and scoped task list.
	status, raw = request(t, srv, http.MethodGet, "/api/projects/"+project.ID+"/board", "", nil)
	if board := decode[Board](t, raw); status != http.StatusOK || len(board.Tasks) != 0 || len(board.Columns) != 4 {
		t.Fatalf("new board = %d %s", status, raw)
	}
	status, raw = request(t, srv, http.MethodGet, "/api/tasks?projectId="+project.ID, "", nil)
	if tasks := decode[[]model.Task](t, raw); status != http.StatusOK || len(tasks) != 0 {
		t.Fatalf("new project tasks = %d %s", status, raw)
	}

	if status, raw := request(t, srv, http.MethodPost, "/api/projects", "", map[string]any{}); status != http.StatusBadRequest {
		t.Fatalf("empty project name = %d %s, want 400", status, raw)
	}
	if status, raw := request(t, srv, http.MethodPost, "/api/projects", "", map[string]any{"name": "  "}); status != http.StatusBadRequest {
		t.Fatalf("blank project name = %d %s, want 400", status, raw)
	}
}

// TestProjectColorCreatePatch covers the project accent color across the
// HTTP surface: creation carries it, the patch changes and clears it, and
// malformed values are rejected at the boundary (store trusts HTTP).
func TestProjectColorCreatePatch(t *testing.T) {
	srv := start(t)

	status, raw := request(t, srv, http.MethodPost, "/api/projects", "gui", map[string]any{
		"name": "彩色项目", "color": "#2563eb",
	})
	if status != http.StatusCreated {
		t.Fatalf("create with color = %d %s", status, raw)
	}
	project := decode[model.Project](t, raw)
	if project.Color != "#2563eb" {
		t.Fatalf("created color = %q", project.Color)
	}

	// Invalid hex is rejected without creating anything.
	if status, raw := request(t, srv, http.MethodPost, "/api/projects", "gui", map[string]any{
		"name": "坏颜色", "color": "blue",
	}); status != http.StatusBadRequest {
		t.Fatalf("bad color create = %d %s, want 400", status, raw)
	}
	if status, raw := request(t, srv, http.MethodPost, "/api/projects", "gui", map[string]any{
		"name": "坏颜色2", "color": "#12345",
	}); status != http.StatusBadRequest {
		t.Fatalf("short color create = %d %s, want 400", status, raw)
	}

	// PATCH changes the color; empty string clears it back to automatic.
	status, raw = request(t, srv, http.MethodPatch, "/api/projects/"+project.ID, "gui",
		map[string]any{"color": "#10b981"})
	if status != http.StatusOK {
		t.Fatalf("patch color = %d %s", status, raw)
	}
	patched := decode[model.Project](t, raw)
	if patched.Color != "#10b981" {
		t.Fatalf("patched color = %q", patched.Color)
	}
	status, raw = request(t, srv, http.MethodPatch, "/api/projects/"+project.ID, "gui",
		map[string]any{"color": ""})
	if status != http.StatusOK {
		t.Fatalf("clear color = %d %s", status, raw)
	}
	cleared := decode[model.Project](t, raw)
	if cleared.Color != "" {
		t.Fatalf("cleared color = %q", cleared.Color)
	}
	if status, raw := request(t, srv, http.MethodPatch, "/api/projects/"+project.ID, "gui",
		map[string]any{"color": "nope"}); status != http.StatusBadRequest {
		t.Fatalf("bad color patch = %d %s, want 400", status, raw)
	}
}

func TestCreateProjectWorkspacePathPersistence(t *testing.T) {
	srv := start(t)
	workspace := t.TempDir()
	requestedPath := workspace + string(filepath.Separator) + "."
	wantPath := filepath.Clean(requestedPath)

	status, raw := request(t, srv, http.MethodPost, "/api/projects", "cli", map[string]any{
		"name": "Workspace project", "workspacePath": requestedPath,
	})
	if status != http.StatusCreated {
		t.Fatalf("create project with path = %d %s", status, raw)
	}
	created := decode[model.Project](t, raw)
	if created.WorkspacePath != wantPath {
		t.Fatalf("created workspacePath = %q, want %q", created.WorkspacePath, wantPath)
	}

	status, raw = request(t, srv, http.MethodGet, "/api/projects", "", nil)
	projects := decode[[]model.Project](t, raw)
	if status != http.StatusOK {
		t.Fatalf("list projects = %d %s", status, raw)
	}
	found := false
	for _, project := range projects {
		if project.ID == created.ID {
			found = true
			if project.WorkspacePath != wantPath {
				t.Fatalf("listed workspacePath = %q, want %q", project.WorkspacePath, wantPath)
			}
		}
	}
	if !found {
		t.Fatalf("created project %q missing from list", created.ID)
	}

	status, raw = request(t, srv, http.MethodGet, "/api/projects/"+created.ID+"/board", "", nil)
	var board struct {
		Project model.Project `json:"project"`
	}
	if err := json.Unmarshal(raw, &board); err != nil {
		t.Fatalf("decode board %s: %v", raw, err)
	}
	if status != http.StatusOK || board.Project.WorkspacePath != wantPath {
		t.Fatalf("board project workspacePath = %d %q, want %q", status, board.Project.WorkspacePath, wantPath)
	}
}

// TestPatchProjectExpandsTildePath covers the typed-path flow: a leading ~
// resolves to the home directory, which exists, so the patch succeeds and
// the stored path is the expanded absolute one.
func TestPatchProjectExpandsTildePath(t *testing.T) {
	srv := start(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}

	status, raw := request(t, srv, http.MethodPost, "/api/projects", "cli", map[string]any{"name": "tilde"})
	if status != http.StatusCreated {
		t.Fatalf("create = %d %s", status, raw)
	}
	created := decode[model.Project](t, raw)

	status, raw = request(t, srv, http.MethodPatch, "/api/projects/"+created.ID, "cli", map[string]any{"workspacePath": "~"})
	if status != http.StatusOK {
		t.Fatalf("patch ~ = %d %s", status, raw)
	}
	got := decode[model.Project](t, raw)
	if got.WorkspacePath != home {
		t.Fatalf("~ expanded to %q, want %q", got.WorkspacePath, home)
	}

	status, raw = request(t, srv, http.MethodPatch, "/api/projects/"+created.ID, "cli", map[string]any{"workspacePath": "~/."})
	if status != http.StatusOK {
		t.Fatalf("patch ~/. = %d %s", status, raw)
	}
	got = decode[model.Project](t, raw)
	if got.WorkspacePath != filepath.Clean(home) {
		t.Fatalf("~/. expanded to %q, want %q", got.WorkspacePath, filepath.Clean(home))
	}
}

func TestCreateProjectRejectsInvalidWorkspacePath(t *testing.T) {
	srv := start(t)
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("file"), 0o600); err != nil {
		t.Fatalf("create test file: %v", err)
	}
	cases := []string{
		"relative/path",
		filepath.Join(t.TempDir(), "does-not-exist"),
		file,
	}
	for _, path := range cases {
		status, raw := request(t, srv, http.MethodPost, "/api/projects", "cli", map[string]any{
			"name": "Invalid workspace", "workspacePath": path,
		})
		if status != http.StatusBadRequest {
			t.Errorf("workspacePath %q = %d %s, want 400", path, status, raw)
		}
	}
}

func TestCommentEditDeleteRequireAuthor(t *testing.T) {
	srv := start(t)
	status, raw := request(t, srv, http.MethodGet, "/api/tasks?projectId="+fixtureProjectID(t, srv), "", nil)
	if status != http.StatusOK {
		t.Fatalf("tasks = %d %s", status, raw)
	}
	task := decode[[]model.Task](t, raw)[0]
	status, raw = request(t, srv, http.MethodPost, "/api/tasks/"+task.ID+"/comments", "alice", map[string]any{"text": "原文"})
	if status != http.StatusCreated {
		t.Fatalf("create comment = %d %s", status, raw)
	}
	comment := decode[model.Comment](t, raw)
	path := "/api/tasks/" + task.ID + "/comments/" + comment.ID
	if status, raw = request(t, srv, http.MethodPatch, path, "bob", map[string]any{"text": "越权编辑"}); status != http.StatusForbidden {
		t.Fatalf("foreign edit = %d %s", status, raw)
	}
	if status, raw = request(t, srv, http.MethodPatch, path, "alice", map[string]any{"text": ""}); status != http.StatusBadRequest {
		t.Fatalf("blank edit = %d %s", status, raw)
	}
	if status, raw = request(t, srv, http.MethodPatch, path, "alice", map[string]any{"text": "已编辑"}); status != http.StatusOK || decode[model.Comment](t, raw).Text != "已编辑" {
		t.Fatalf("edit = %d %s", status, raw)
	}
	if status, raw = request(t, srv, http.MethodDelete, path, "bob", nil); status != http.StatusForbidden {
		t.Fatalf("foreign delete = %d %s", status, raw)
	}
	if status, raw = request(t, srv, http.MethodDelete, path, "alice", nil); status != http.StatusNoContent || len(raw) != 0 {
		t.Fatalf("delete = %d %s", status, raw)
	}
	status, raw = request(t, srv, http.MethodGet, "/api/tasks/"+task.ID+"/comments", "", nil)
	if status != http.StatusOK || len(decode[[]model.Comment](t, raw)) != 0 {
		t.Fatalf("remaining comments = %d %s", status, raw)
	}
}

func TestCommentRepliesAndParentDeletion(t *testing.T) {
	srv := start(t)
	projectID := fixtureProjectID(t, srv)
	status, raw := request(t, srv, http.MethodGet, "/api/tasks?projectId="+projectID, "", nil)
	if status != http.StatusOK {
		t.Fatalf("tasks = %d %s", status, raw)
	}
	task := decode[[]model.Task](t, raw)[0]
	base := "/api/tasks/" + task.ID + "/comments"
	status, raw = request(t, srv, http.MethodPost, base, "alice", map[string]any{"text": "主评论"})
	if status != http.StatusCreated {
		t.Fatalf("create root = %d %s", status, raw)
	}
	root := decode[model.Comment](t, raw)
	status, raw = request(t, srv, http.MethodPost, base, "bob", map[string]any{"text": "回复", "parentCommentId": root.ID})
	if status != http.StatusCreated {
		t.Fatalf("create reply = %d %s", status, raw)
	}
	reply := decode[model.Comment](t, raw)
	if reply.ParentCommentID != root.ID {
		t.Fatalf("reply parent = %q, want %q", reply.ParentCommentID, root.ID)
	}
	if status, raw = request(t, srv, http.MethodPost, base, "bob", map[string]any{"text": "wrong task parent", "parentCommentId": "missing"}); status != http.StatusBadRequest {
		t.Fatalf("missing parent = %d %s", status, raw)
	}
	if status, raw = request(t, srv, http.MethodDelete, base+"/"+root.ID, "alice", nil); status != http.StatusNoContent {
		t.Fatalf("delete root = %d %s", status, raw)
	}
	status, raw = request(t, srv, http.MethodGet, base, "", nil)
	comments := decode[[]model.Comment](t, raw)
	if status != http.StatusOK || len(comments) != 1 || comments[0].ID != reply.ID || comments[0].ParentCommentID != "" {
		t.Fatalf("comments after root deletion = %d %+v", status, comments)
	}
}

type relationAPIResponse struct {
	Task        model.Task `json:"task"`
	RelatedTask model.Task `json:"relatedTask"`
}

func TestTaskRelationsAndDependencyGate(t *testing.T) {
	srv := start(t)
	projectID := fixtureProjectID(t, srv)
	boardStatus, raw := request(t, srv, http.MethodGet, "/api/projects/"+projectID+"/board", "", nil)
	if boardStatus != http.StatusOK {
		t.Fatalf("board=%d %s", boardStatus, raw)
	}
	board := decode[Board](t, raw)
	create := func(title string) model.Task {
		status, body := request(t, srv, http.MethodPost, "/api/tasks", "test", map[string]any{"projectId": projectID, "columnId": board.Columns[0].ID, "title": title})
		if status != http.StatusCreated {
			t.Fatalf("create=%d %s", status, body)
		}
		return decode[model.Task](t, body)
	}
	blocked := create("dependent")
	blocker := create("blocker")
	path := "/api/tasks/" + blocked.ID + "/relations/blocked_by/" + blocker.ID
	status, body := request(t, srv, http.MethodPost, path, "test", map[string]int{"version": blocked.Version})
	if status != http.StatusOK {
		t.Fatalf("add relation=%d %s", status, body)
	}
	result := decode[relationAPIResponse](t, body)
	if len(result.Task.Relations.BlockedBy) != 1 || result.Task.Relations.BlockedBy[0].ID != blocker.ID {
		t.Fatalf("relation result=%+v", result)
	}
	status, body = request(t, srv, http.MethodPatch, "/api/tasks/"+blocked.ID, "test", map[string]any{"version": result.Task.Version, "columnId": board.Columns[1].ID})
	if status != http.StatusBadRequest || !strings.Contains(string(body), "前置未完成") {
		t.Fatalf("blocked move=%d %s", status, body)
	}
	status, body = request(t, srv, http.MethodPatch, "/api/tasks/"+blocker.ID, "test", map[string]any{"version": blocker.Version, "columnId": board.Columns[3].ID})
	if status != http.StatusOK {
		t.Fatalf("complete blocker=%d %s", status, body)
	}
	status, body = request(t, srv, http.MethodPatch, "/api/tasks/"+blocked.ID, "test", map[string]any{"version": result.Task.Version, "columnId": board.Columns[1].ID})
	if status != http.StatusOK {
		t.Fatalf("move unblocked=%d %s", status, body)
	}
}


func createTaskForTest(t *testing.T, srv *Server, projectID, columnID, title string) model.Task {
	t.Helper()
	status, raw := request(t, srv, http.MethodPost, "/api/tasks", "test", map[string]any{"projectId": projectID, "columnId": columnID, "title": title})
	if status != http.StatusCreated {
		t.Fatalf("create task=%d %s", status, raw)
	}
	return decode[model.Task](t, raw)
}
