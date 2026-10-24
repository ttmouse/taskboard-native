// Package client is the GUI's only path to the taskboard service: JSON
// calls over the local HTTP API carrying the caller's agent identity, and
// the SSE stream that pushes every mutation, so no view ever polls.
//
// It speaks the contract in docs/contracts.md and tolerates the older
// shapes (a flat 409 body, a separate move endpoint) so the window keeps
// working against either build of the service.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"taskboard-native/internal/model"
)

// DefaultAddr is where the service listens when TASKBOARD_ADDR says nothing.
const DefaultAddr = "127.0.0.1:47826"

// ConfigDir is the directory holding server.json and the store, as the
// service's own discovery file and TASKBOARD_DIR agree.
func ConfigDir() string {
	if d := os.Getenv("TASKBOARD_DIR"); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "taskboard-native")
}

// Discover is the service's address and token, as server.json holds them.
type Discover struct {
	Addr  string `json:"addr,omitempty"`
	Port  int    `json:"port"`
	Token string `json:"token,omitempty"`
	PID   int    `json:"pid,omitempty"`
}

// Addr resolves the listen address from either field.
func (d *Discover) HostPort() string {
	if d == nil {
		return ""
	}
	if d.Addr != "" {
		return d.Addr
	}
	if d.Port > 0 {
		return "127.0.0.1:" + strconv.Itoa(d.Port)
	}
	return ""
}

// ErrNotRunning reports that no local service answered.
var ErrNotRunning = errors.New("任务看板服务未运行")

// LoadDiscover reads the service's address: server.json (written by a
// service that announces itself), then TASKBOARD_ADDR, then the default
// loopback address. Dial checks that something answers there.
func LoadDiscover() (*Discover, error) {
	if raw, err := os.ReadFile(filepath.Join(ConfigDir(), "server.json")); err == nil {
		var d Discover
		if json.Unmarshal(raw, &d) == nil && d.HostPort() != "" {
			return &d, nil
		}
	}
	if addr := strings.TrimSpace(os.Getenv("TASKBOARD_ADDR")); addr != "" {
		return &Discover{Addr: addr, Token: os.Getenv("TASKBOARD_TOKEN")}, nil
	}
	return &Discover{Addr: DefaultAddr, Token: os.Getenv("TASKBOARD_TOKEN")}, nil
}

// ConflictError reports a version mismatch; Current is the server's task.
type ConflictError struct{ Current model.Task }

func (e *ConflictError) Error() string {
	return fmt.Sprintf("版本冲突：服务端已是 v%d", e.Current.Version)
}

// HTTPError is a non-2xx answer other than 409.
type HTTPError struct {
	Status int
	Msg    string
}

func (e *HTTPError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	return "HTTP " + strconv.Itoa(e.Status)
}

// Client calls the API. Agent travels as X-Agent on every request.
type Client struct {
	Base  string
	Token string
	Agent string
	HTTP  *http.Client
}

// New builds a client for d, with agent "local" when empty.
func New(d *Discover, agent string) *Client {
	if agent == "" {
		if env := os.Getenv("TASKBOARD_AGENT"); env != "" {
			agent = env
		} else {
			agent = "local"
		}
	}
	return &Client{
		Base:  "http://" + d.HostPort(),
		Token: d.Token,
		Agent: agent,
		HTTP:  &http.Client{Timeout: 10 * time.Second},
	}
}

// Dial finds the service and checks that it answers.
func Dial(agent string) (*Client, error) {
	d, err := LoadDiscover()
	if err != nil {
		return nil, err
	}
	c := New(d, agent)
	if _, err := c.Health(); err != nil {
		return nil, ErrNotRunning
	}
	return c, nil
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("X-Agent", c.Agent)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrNotRunning
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil || len(bytes.TrimSpace(raw)) == 0 {
			return nil
		}
		return json.Unmarshal(raw, out)
	}
	if resp.StatusCode == http.StatusConflict {
		if t, ok := decodeConflict(raw); ok {
			return &ConflictError{Current: t}
		}
		return &HTTPError{Status: resp.StatusCode, Msg: "版本冲突"}
	}
	var e struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &e)
	return &HTTPError{Status: resp.StatusCode, Msg: e.Error}
}

func (c *Client) get(path string, out any) error {
	return c.do(context.Background(), "GET", path, nil, out)
}

// decodeConflict accepts the {error, task} envelope and the older flat
// task body the service used to write.
func decodeConflict(raw []byte) (model.Task, bool) {
	var wrap struct {
		Task *model.Task `json:"task"`
	}
	if json.Unmarshal(raw, &wrap) == nil && wrap.Task != nil && wrap.Task.ID != "" {
		return *wrap.Task, true
	}
	var t model.Task
	if json.Unmarshal(raw, &t) == nil && t.ID != "" {
		return t, true
	}
	return model.Task{}, false
}

// --- wire types ---

// Board is one project's columns and live tasks.
type Board struct {
	Project model.Project  `json:"project"`
	Columns []model.Column `json:"columns"`
	Tasks   []model.Task   `json:"tasks"`
}

// NewTask is the body of POST /api/tasks.
type NewTask struct {
	ProjectID   string `json:"projectId"`
	ColumnID    string `json:"columnId,omitempty"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Assignee    string `json:"assignee,omitempty"`
}

// TaskPatch is the body of PATCH /api/tasks/{id}.
type TaskPatch struct {
	Version  int     `json:"version"`
	Title    *string `json:"title,omitempty"`
	Desc     *string `json:"description,omitempty"`
	Assignee *string `json:"assignee,omitempty"`
	ColumnID *string `json:"columnId,omitempty"`
	AfterID  string  `json:"afterId,omitempty"`
	AtTop    bool    `json:"atTop,omitempty"`
	Archived *bool   `json:"archived,omitempty"`
}

// --- API ---

// Health reports whether the service answers.
func (c *Client) Health() (map[string]any, error) {
	var m map[string]any
	err := c.get("/api/health", &m)
	return m, err
}

// Projects lists the projects the agent may see.
func (c *Client) Projects() ([]model.Project, error) {
	var ps []model.Project
	err := c.get("/api/projects", &ps)
	return ps, err
}

// CreateProject adds a project with the service's default columns.
func (c *Client) CreateProject(name string) (model.Project, error) {
	var p model.Project
	err := c.do(context.Background(), "POST", "/api/projects", map[string]string{"name": name}, &p)
	return p, err
}

// Board reads a project's columns and live tasks in one call, falling back
// to the per-project task list when the board endpoint is absent.
func (c *Client) Board(projectID string) (Board, error) {
	var b Board
	if err := c.get("/api/projects/"+url.PathEscape(projectID)+"/board", &b); err == nil {
		if b.Project.ID == "" {
			b.Project.ID = projectID
		}
		if len(b.Columns) == 0 {
			b.Columns = b.Project.Columns
		}
		return b, nil
	}
	p, err := c.project(projectID)
	if err != nil {
		return Board{}, err
	}
	ts, err := c.Tasks(projectID, false)
	if err != nil {
		return Board{}, err
	}
	return Board{Project: p, Columns: p.Columns, Tasks: ts}, nil
}

// Tasks lists one project's tasks.
func (c *Client) Tasks(projectID string, includeArchived bool) ([]model.Task, error) {
	var ts []model.Task
	q := "/api/tasks?projectId=" + url.QueryEscape(projectID)
	if includeArchived {
		q += "&includeArchived=true"
	}
	err := c.get(q, &ts)
	return ts, err
}

// ResolveColumn turns a column ID or name into an ID, the project's first
// column when ref is empty.
func (c *Client) ResolveColumn(projectID, ref string) (string, error) {
	p, err := c.project(projectID)
	if err != nil {
		return "", err
	}
	if ref == "" {
		if len(p.Columns) == 0 {
			return "", fmt.Errorf("项目没有列")
		}
		return p.Columns[0].ID, nil
	}
	for _, col := range p.Columns {
		if col.ID == ref || col.Name == ref {
			return col.ID, nil
		}
	}
	return "", fmt.Errorf("未知列 %q", ref)
}

func (c *Client) project(id string) (model.Project, error) {
	ps, err := c.Projects()
	if err != nil {
		return model.Project{}, err
	}
	for _, p := range ps {
		if p.ID == id {
			return p, nil
		}
	}
	return model.Project{}, &HTTPError{Status: http.StatusNotFound, Msg: "项目不存在"}
}

// Task reads one task with its comments.
func (c *Client) Task(id string) (model.Task, error) {
	var t model.Task
	err := c.get("/api/tasks/"+url.PathEscape(id), &t)
	return t, err
}

// CreateTask adds a task to a column (the first one when columnID is "").
func (c *Client) CreateTask(projectID, columnID, title, description, assignee string) (model.Task, error) {
	in := NewTask{ProjectID: projectID, ColumnID: columnID, Title: title, Description: description, Assignee: assignee}
	var t model.Task
	err := c.do(context.Background(), "POST", "/api/tasks", in, &t)
	return t, err
}

// Update patches a task's text; version 0 reads the current version first.
func (c *Client) Update(id string, version int, title, description, assignee *string) (model.Task, error) {
	if version == 0 {
		t, err := c.Task(id)
		if err != nil {
			return model.Task{}, err
		}
		version = t.Version
	}
	p := TaskPatch{Version: version, Title: title, Desc: description, Assignee: assignee}
	var t model.Task
	err := c.do(context.Background(), "PATCH", "/api/tasks/"+url.PathEscape(id), p, &t)
	return t, err
}

// Move moves a task into toColumn, after afterID, or to the head of that
// column when atTop.
func (c *Client) Move(id string, version int, toColumn, afterID string, atTop bool) (model.Task, error) {
	if version == 0 {
		t, err := c.Task(id)
		if err != nil {
			return model.Task{}, err
		}
		version = t.Version
	}
	p := TaskPatch{Version: version, ColumnID: &toColumn, AfterID: afterID, AtTop: atTop}
	var t model.Task
	err := c.do(context.Background(), "PATCH", "/api/tasks/"+url.PathEscape(id), p, &t)
	return t, err
}

// Archive soft-deletes a task.
func (c *Client) Archive(id string, version int) (model.Task, error) {
	if version == 0 {
		t, err := c.Task(id)
		if err != nil {
			return model.Task{}, err
		}
		version = t.Version
	}
	var t model.Task
	err := c.do(context.Background(), "POST", "/api/tasks/"+url.PathEscape(id)+"/archive",
		map[string]int{"version": version}, &t)
	return t, err
}

// Restore puts an archived card back: the service clears the flag, bumps
// the version and places the card at the bottom of its own column. version
// 0 reads the current one first.
func (c *Client) Restore(id string, version int) (model.Task, error) {
	if version == 0 {
		t, err := c.Task(id)
		if err != nil {
			return model.Task{}, err
		}
		version = t.Version
	}
	live := false
	var t model.Task
	err := c.do(context.Background(), "PATCH", "/api/tasks/"+url.PathEscape(id),
		TaskPatch{Version: version, Archived: &live}, &t)
	return t, err
}

// Comments lists a task's comments.
func (c *Client) Comments(taskID string) ([]model.Comment, error) {
	var cs []model.Comment
	err := c.get("/api/tasks/"+url.PathEscape(taskID)+"/comments", &cs)
	return cs, err
}

// AddComment appends a comment.
func (c *Client) AddComment(taskID, text string) (model.Comment, error) {
	var cm model.Comment
	err := c.do(context.Background(), "POST", "/api/tasks/"+url.PathEscape(taskID)+"/comments",
		map[string]string{"text": text}, &cm)
	return cm, err
}

// --- SSE ---

// StreamState is how the event stream stands.
type StreamState int

const (
	// Connecting is the first attempt, or a retry after a drop.
	Connecting StreamState = iota
	// Online means events are flowing.
	Online
	// Offline means the service cannot be reached; retries continue.
	Offline
)

func (s StreamState) String() string {
	switch s {
	case Online:
		return "已连接"
	case Offline:
		return "未连接"
	default:
		return "连接中"
	}
}

// Subscribe reads the project's events, calling onEvent for each and
// onState whenever the connection changes. It reconnects with exponential
// backoff — reset by every connection that stood — resuming from
// Last-Event-ID so a drop loses nothing, and blocks until ctx ends.
func (c *Client) Subscribe(ctx context.Context, projectID string, onEvent func(model.Event), onState func(StreamState)) error {
	var lastID atomic.Int64
	backoff := 250 * time.Millisecond
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		reached := false
		c.consume(ctx, projectID, &lastID, func(ev model.Event) {
			reached = true
			if onEvent != nil {
				onEvent(ev)
			}
		}, func(st StreamState) {
			if st == Online {
				reached = true
			}
			if onState != nil {
				onState(st)
			}
		})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if reached {
			backoff = 250 * time.Millisecond
		} else if onState != nil {
			onState(Offline)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 5*time.Second {
			backoff *= 2
		}
	}
}

// consume runs one connection, calling onEvent per event and onState on
// every state change, until the stream ends or ctx ends.
func (c *Client) consume(ctx context.Context, projectID string, lastID *atomic.Int64, onEvent func(model.Event), onState func(StreamState)) error {
	u := c.Base + "/api/events"
	if projectID != "" {
		u += "?projectId=" + url.QueryEscape(projectID)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Agent", c.Agent)
	req.Header.Set("Accept", "text/event-stream")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if id := lastID.Load(); id > 0 {
		req.Header.Set("Last-Event-ID", strconv.FormatInt(id, 10))
	}
	hc := &http.Client{} // no timeout: the stream stays open
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &HTTPError{Status: resp.StatusCode, Msg: "事件流不可用"}
	}
	if onState != nil {
		onState(Online)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var evType string
	var data []byte
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if len(data) > 0 {
				var ev model.Event
				if json.Unmarshal(data, &ev) == nil {
					if ev.Type == "" {
						ev.Type = evType
					}
					if ev.Seq > 0 {
						lastID.Store(ev.Seq)
					}
					if onEvent != nil {
						onEvent(ev)
					}
				}
			}
			evType, data = "", nil
		case strings.HasPrefix(line, ":"):
			// comment, including the 15s ping that keeps the link alive
		case strings.HasPrefix(line, "event:"):
			evType = strings.TrimSpace(line[len("event:"):])
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimSpace(line[len("data:"):])...)
		case strings.HasPrefix(line, "id:"):
			if id, err := strconv.ParseInt(strings.TrimSpace(line[len("id:"):]), 10, 64); err == nil {
				lastID.Store(id)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return io.EOF
}
