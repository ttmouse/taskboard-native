// Package service is the taskboard's local HTTP API and realtime hub. It is
// the only writer of the SQLite store; the GUI, the CLI and the MCP server
// are all clients.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"taskboard-native/internal/executor"
	"taskboard-native/internal/model"
	"taskboard-native/internal/store"
)

// DefaultAddr is the loopback address the service binds by default.
const DefaultAddr = "127.0.0.1:47826"

// DefaultDBPath resolves the product database location: the mygo user-data
// directory, or TASKBOARD_DB when set.
func DefaultDBPath() (string, error) {
	if p := os.Getenv("TASKBOARD_DB"); p != "" {
		return p, nil
	}
	dir, err := userDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, DefaultDBFile), nil
}

// DefaultListenAddr resolves the listen address, or TASKBOARD_ADDR when set.
func DefaultListenAddr() string {
	if a := os.Getenv("TASKBOARD_ADDR"); a != "" {
		return a
	}
	return DefaultAddr
}

// Options configure Start. Empty values fall back to the environment.
type Options struct {
	Addr   string // listen address, TASKBOARD_ADDR, then 127.0.0.1:47826
	DBPath string // database path, TASKBOARD_DB, then user data
	// Slow injects a delay into every request; TASKBOARD_SLOW overrides it.
	Slow time.Duration
}

// Server is a running service instance.
type Server struct {
	// Addr is the bound address, including the resolved port.
	Addr string
	// Port is the bound TCP port.
	Port int
	// DBPath is the database file in use.
	DBPath string
	// URL is the http base URL of the service.
	URL string

	http     *http.Server
	listener net.Listener
	store    *store.Store
	hub      *hub
	slow     time.Duration
	ping     time.Duration // SSE keep-alive interval; 0 = 15s
	dispatch *executor.Dispatcher
	closed   chan struct{}
	stopOnce sync.Once
}

// Start opens the database and serves HTTP on addr ("127.0.0.1:47826" when
// empty, ":0" for a random free port). It returns as soon as the listener is
// bound; the caller must Close the server.
func Start(addr, dbPath string) (*Server, error) {
	return StartOptions(Options{Addr: addr, DBPath: dbPath})
}

// StartOptions is Start with explicit options, including the slow hook.
func StartOptions(opts Options) (*Server, error) {
	addr := opts.Addr
	if addr == "" {
		addr = DefaultListenAddr()
	}
	dbPath := opts.DBPath
	if dbPath == "" {
		var err error
		dbPath, err = DefaultDBPath()
		if err != nil {
			return nil, err
		}
	}
	slow := opts.Slow
	if raw := os.Getenv("TASKBOARD_SLOW"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("TASKBOARD_SLOW: %w", err)
		}
		slow = d
	}

	st, err := store.Open(dbPath)
	if err != nil {
		return nil, err
	}
	h := newHub()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		st.Close()
		return nil, err
	}
	boardHost, boardPort, _ := net.SplitHostPort(ln.Addr().String())
	if ip := net.ParseIP(boardHost); ip != nil && ip.IsUnspecified() {
		boardHost = "127.0.0.1"
	}
	s := &Server{
		Addr:     ln.Addr().String(),
		Port:     ln.Addr().(*net.TCPAddr).Port,
		DBPath:   dbPath,
		store:    st,
		hub:      h,
		slow:     slow,
		dispatch: executor.NewDispatcher(st, dbPath+".runtimes.json", "http://"+net.JoinHostPort(boardHost, boardPort), dbPath+".auto-handoff.jsonl"),
		closed:   make(chan struct{}),
		listener: ln,
	}
	st.OnEvent = func(ev model.Event) {
		h.publish(ev)
		if ev.Task != nil && (ev.Type == model.EventTaskCreated || ev.Type == model.EventTaskMoved || ev.Type == model.EventTaskArchived) {
			go s.dispatch.NoteProjectRun(*ev.Task)
		}
	}
	s.URL = "http://" + s.Addr
	s.http = &http.Server{
		Handler:           s.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return context.Background() },
	}
	go func() {
		if err := s.http.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("taskboard: serve: %v", err)
		}
	}()
	return s, nil
}

// Close stops accepting requests, cancels running task executions, closes
// SSE subscribers and the database.
func (s *Server) Close() error {
	var err error
	s.stopOnce.Do(func() {
		close(s.closed)
		s.hub.closeAll()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		shutdownErr := s.http.Shutdown(ctx)
		// Cancel executions before the store closes so their write-back
		// goroutines drain against a live store.
		s.dispatch.Close()
		storeErr := s.store.Close()
		err = errors.Join(shutdownErr, storeErr)
	})
	return err
}

// Done reports when the server has been closed.
func (s *Server) Done() <-chan struct{} { return s.closed }

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/projects", s.handleProjects)
	mux.HandleFunc("POST /api/projects", s.handleCreateProject)
	mux.HandleFunc("PATCH /api/projects/{id}", s.handlePatchProject)
	mux.HandleFunc("DELETE /api/projects/{id}", s.handleDeleteProject)
	mux.HandleFunc("PATCH /api/projects/{id}/columns/{columnID}", s.handlePatchColumn)
	mux.HandleFunc("GET /api/projects/{id}/board", s.handleBoard)
	mux.HandleFunc("POST /api/projects/{id}/run", s.handleStartProjectRun)
	mux.HandleFunc("GET /api/projects/{id}/run", s.handleProjectRun)
	mux.HandleFunc("POST /api/projects/{id}/run/stop", s.handleStopProjectRun)
	mux.HandleFunc("GET /api/tasks", s.handleTasks)
	mux.HandleFunc("POST /api/tasks", s.handleCreateTask)
	mux.HandleFunc("GET /api/tasks/{id}", s.handleTask)
	mux.HandleFunc("POST /api/tasks/{id}/images", s.handleUploadImage)
	mux.HandleFunc("POST /api/images", s.handleUploadStandaloneImage)
	mux.HandleFunc("GET /api/images/{hash}", s.handleImage)
	mux.HandleFunc("PATCH /api/tasks/{id}", s.handlePatchTask)
	mux.HandleFunc("POST /api/tasks/{id}/archive", s.handleArchive)
	mux.HandleFunc("POST /api/tasks/{id}/dispatch", s.handleDispatch)
	mux.HandleFunc("POST /api/tasks/{id}/execution/release", s.handleReleaseExecution)
	mux.HandleFunc("GET /api/tasks/{id}/comments", s.handleComments)
	mux.HandleFunc("POST /api/tasks/{id}/comments", s.handleAddComment)
	mux.HandleFunc("PATCH /api/tasks/{id}/comments/{commentId}", s.handleUpdateComment)
	mux.HandleFunc("DELETE /api/tasks/{id}/comments/{commentId}", s.handleDeleteComment)
	mux.HandleFunc("POST /api/tasks/{id}/comments/{commentId}/continue", s.handleContinue)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	return s.middleware(mux)
}

// middleware stamps the caller identity and applies the slow hook.
func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.slow > 0 {
			select {
			case <-time.After(s.slow):
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		next.ServeHTTP(w, r)
	})
}

// agent returns the caller identity: X-Agent, else "local".
func agent(r *http.Request) string {
	if a := strings.TrimSpace(r.Header.Get("X-Agent")); a != "" {
		return a
	}
	return "local"
}

// --- responses ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		log.Printf("taskboard: encode response: %v", err)
	}
}

type errorBody struct {
	Error string      `json:"error"`
	Task  *model.Task `json:"task,omitempty"`
}

func writeError(w http.ResponseWriter, err error) {
	var invalid *store.InvalidError
	var conflict *store.VersionError
	var roleConflict *store.RoleConflictError
	switch {
	case errors.As(err, &conflict):
		cur := conflict.Current
		writeJSON(w, http.StatusConflict, errorBody{Error: "version_conflict", Task: &cur})
	case errors.As(err, &roleConflict):
		writeJSON(w, http.StatusConflict, errorBody{Error: "role_conflict"})
	case errors.As(err, &invalid):
		writeJSON(w, http.StatusBadRequest, errorBody{Error: err.Error()})
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Error: "not_found"})
	case errors.Is(err, store.ErrForbidden):
		writeJSON(w, http.StatusForbidden, errorBody{Error: "comment_author_mismatch"})
	default:
		log.Printf("taskboard: %v", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Error: "internal_error"})
	}
}

func writeBadRequest(w http.ResponseWriter, format string, args ...any) {
	writeJSON(w, http.StatusBadRequest, errorBody{Error: fmt.Sprintf(format, args...)})
}

// decodeJSON reads a request body. Unknown fields are ignored on purpose:
// clients may echo a full Task back, and only the fields the endpoint acts
// on matter. Malformed JSON and wrong types are still rejected.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(dst); err != nil {
		writeBadRequest(w, "invalid json: %v", err)
		return false
	}
	return true
}

// --- handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleProjects(w http.ResponseWriter, _ *http.Request) {
	projects, err := s.store.Projects()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, projects)
}

type newProject struct {
	Name          string `json:"name"`
	WorkspacePath string `json:"workspacePath"`
	Color         string `json:"color,omitempty"`
}

// checkColor validates an optional project accent color: empty means "the
// window assigns a stable slot"; otherwise it must be a "#rrggbb" hex string
// so the GUI can paint with it without parsing surprises.
func checkColor(w http.ResponseWriter, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", true
	}
	if len(raw) != 7 || raw[0] != '#' {
		writeBadRequest(w, "color must be a #rrggbb hex string or empty")
		return "", false
	}
	for i := 1; i < len(raw); i++ {
		c := raw[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			continue
		}
		writeBadRequest(w, "color must be a #rrggbb hex string or empty")
		return "", false
	}
	return raw, true
}

// expandHome expands a leading "~" or "~/" to the user's home directory,
// so paths like ~/Projects/demo work when typed by hand. "~user" is not
// supported — only the current user's home.
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, p[2:])
}

// checkWorkspacePath validates an optional workspace path: empty means
// "none"; otherwise it must be an existing absolute directory (~ expands
// to the home directory first). Returns the cleaned path. Shared by
// project create and project edit.
func checkWorkspacePath(w http.ResponseWriter, raw string) (string, bool) {
	if raw == "" {
		return "", true
	}
	expanded := expandHome(strings.TrimSpace(raw))
	if !filepath.IsAbs(expanded) {
		writeBadRequest(w, "workspacePath must be an absolute directory")
		return "", false
	}
	cleaned := filepath.Clean(expanded)
	info, err := os.Stat(cleaned)
	if err != nil || !info.IsDir() {
		writeBadRequest(w, "workspacePath must be an existing directory")
		return "", false
	}
	return cleaned, true
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req newProject
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeBadRequest(w, "name is required")
		return
	}
	workspacePath, ok := checkWorkspacePath(w, req.WorkspacePath)
	if !ok {
		return
	}
	color, ok := checkColor(w, req.Color)
	if !ok {
		return
	}
	project, err := s.store.CreateProjectWithColor(req.Name, workspacePath, color, agent(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, project)
}

type projectPatch struct {
	Name          *string `json:"name"`
	WorkspacePath *string `json:"workspacePath"`
	Color         *string `json:"color"`
}

// handlePatchProject edits a project's name and/or workspace path. Absent
// fields stay unchanged; an empty workspacePath clears the path. Path
// validation matches project creation.
func (s *Server) handlePatchProject(w http.ResponseWriter, r *http.Request) {
	var req projectPatch
	if !decodeJSON(w, r, &req) {
		return
	}
	patch := store.ProjectPatch{Name: req.Name, WorkspacePath: req.WorkspacePath, Color: req.Color}
	if req.WorkspacePath != nil && *req.WorkspacePath != "" {
		cleaned, ok := checkWorkspacePath(w, *req.WorkspacePath)
		if !ok {
			return
		}
		patch.WorkspacePath = &cleaned
	}
	if req.Color != nil {
		cleaned, ok := checkColor(w, *req.Color)
		if !ok {
			return
		}
		patch.Color = &cleaned
	}
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		writeBadRequest(w, "name is required")
		return
	}
	project, err := s.store.UpdateProject(r.PathValue("id"), patch, agent(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, project)
}

// handleDeleteProject removes a project with its columns and every card
// under it — archived ones included. The GUI confirms with card counts
// before calling; the server keeps no undo.
func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	if err := s.dispatch.DeleteProject(r.PathValue("id"), agent(r)); err != nil {
		var busy *executor.ErrProjectBusy
		if errors.As(err, &busy) || errors.Is(err, store.ErrActiveExecution) {
			writeJSON(w, http.StatusConflict, errorBody{Error: "project_busy"})
			return
		}
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type columnPatch struct {
	Role *string `json:"role"`
}

// handlePatchColumn sets a column's workflow role (empty clears it). The
// store enforces the role enum, project ownership and role uniqueness; this
// boundary only adds the "field must be present" rule.
func (s *Server) handlePatchColumn(w http.ResponseWriter, r *http.Request) {
	var req columnPatch
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Role == nil {
		writeBadRequest(w, "role is required")
		return
	}
	column, err := s.store.SetColumnRole(r.PathValue("id"), r.PathValue("columnID"), *req.Role, agent(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, column)
}

func (s *Server) handleBoard(w http.ResponseWriter, r *http.Request) {
	board, err := s.store.Board(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, board)
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("projectId")
	if projectID == "" {
		writeBadRequest(w, "projectId is required")
		return
	}

	columnID, hasColumn, err := optionalQueryValue(r, "columnId")
	if err != nil {
		writeBadRequest(w, "%v", err)
		return
	}
	if hasColumn && columnID == "" {
		writeBadRequest(w, "columnId must not be empty")
		return
	}
	assignee, hasAssignee, err := optionalQueryValue(r, "assignee")
	if err != nil {
		writeBadRequest(w, "%v", err)
		return
	}
	limitValue, hasLimit, err := optionalQueryValue(r, "limit")
	if err != nil {
		writeBadRequest(w, "%v", err)
		return
	}
	limit := 0
	if hasLimit {
		limit, err = strconv.Atoi(limitValue)
		if err != nil || limit < 1 || limit > 200 {
			writeBadRequest(w, "limit must be an integer from 1 to 200")
			return
		}
	}
	offsetValue, hasOffset, err := optionalQueryValue(r, "offset")
	if err != nil {
		writeBadRequest(w, "%v", err)
		return
	}
	if hasOffset && !hasLimit {
		writeBadRequest(w, "offset requires limit")
		return
	}
	offset := 0
	if hasOffset {
		offset, err = strconv.Atoi(offsetValue)
		if err != nil || offset < 0 {
			writeBadRequest(w, "offset must be a non-negative integer")
			return
		}
	}
	summaryValue, hasSummary, err := optionalQueryValue(r, "summary")
	if err != nil {
		writeBadRequest(w, "%v", err)
		return
	}
	summary := false
	if hasSummary {
		switch summaryValue {
		case "true":
			summary = true
		case "false":
		default:
			writeBadRequest(w, "summary must be true or false")
			return
		}
	}

	includeArchived, _ := strconv.ParseBool(r.URL.Query().Get("includeArchived"))
	if hasColumn {
		project, err := s.store.Project(projectID)
		if err != nil {
			writeError(w, err)
			return
		}
		if !project.HasColumn(columnID) {
			writeBadRequest(w, "columnId does not belong to project")
			return
		}
	}
	tasks, err := s.store.TasksByProject(projectID, includeArchived)
	if err != nil {
		writeError(w, err)
		return
	}

	if hasColumn || hasAssignee {
		filtered := make([]model.Task, 0, len(tasks))
		for _, task := range tasks {
			if hasColumn && task.ColumnID != columnID {
				continue
			}
			if hasAssignee && task.Assignee != assignee {
				continue
			}
			filtered = append(filtered, task)
		}
		tasks = filtered
	}
	if hasLimit {
		if offset >= len(tasks) {
			tasks = tasks[len(tasks):]
		} else {
			end := len(tasks)
			if limit < end-offset {
				end = offset + limit
			}
			tasks = tasks[offset:end]
		}
	}
	if summary {
		out := make([]taskSummary, 0, len(tasks))
		for _, task := range tasks {
			out = append(out, taskSummary{
				ID:        task.ID,
				Number:    task.Number,
				ProjectID: task.ProjectID,
				ColumnID:  task.ColumnID,
				Title:     task.Title,
				Version:   task.Version,
				UpdatedAt: task.UpdatedAt,
				UpdatedBy: task.UpdatedBy,
				Assignee:  task.Assignee,
			})
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

func optionalQueryValue(r *http.Request, key string) (string, bool, error) {
	values, ok := r.URL.Query()[key]
	if !ok {
		return "", false, nil
	}
	if len(values) != 1 {
		return "", false, fmt.Errorf("%s must be provided once", key)
	}
	return values[0], true, nil
}

type taskSummary struct {
	ID        string    `json:"id"`
	Number    int       `json:"number"`
	ProjectID string    `json:"projectId"`
	ColumnID  string    `json:"columnId"`
	Title     string    `json:"title"`
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updatedAt"`
	UpdatedBy string    `json:"updatedBy"`
	Assignee  string    `json:"assignee,omitempty"`
}

func (s *Server) handleTask(w http.ResponseWriter, r *http.Request) {
	task, err := s.store.Task(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

type newTask struct {
	ProjectID   string `json:"projectId"`
	ColumnID    string `json:"columnId"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Assignee    string `json:"assignee"`
	// DispatchRuntime turns the create into the one-call "create and
	// dispatch" mode; empty keeps the plain create-only mode.
	DispatchRuntime string `json:"dispatchRuntime"`
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req newTask
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.ProjectID == "" {
		writeBadRequest(w, "projectId is required")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeBadRequest(w, "title is required")
		return
	}
	runtime := strings.TrimSpace(req.DispatchRuntime)
	if runtime != "" {
		// Reject a bad handoff before the card exists, so a 400 leaves
		// nothing behind to clean up.
		if !executor.KnownRuntime(runtime) {
			writeBadRequest(w, "unknown runtime %q", runtime)
			return
		}
		project, err := s.store.Project(req.ProjectID)
		if err != nil {
			writeError(w, err)
			return
		}
		dir := strings.TrimSpace(project.WorkspacePath)
		if dir == "" {
			writeBadRequest(w, "project %q has no workspacePath; set one before dispatching", project.Name)
			return
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			writeBadRequest(w, "workspacePath %q is not a directory on this machine", dir)
			return
		}
	}
	task, err := s.store.CreateTask(req.ProjectID, req.ColumnID, req.Title, req.Description, req.Assignee, agent(r))
	if err != nil {
		writeError(w, err)
		return
	}
	if runtime != "" {
		// A fresh card cannot be in flight, so a failure here is rare
		// (store hiccup, workspace deleted in between). The card stays;
		// the failed handoff is recorded on it instead of 500-ing away
		// the successful creation.
		if err := s.dispatch.Dispatch(task.ID, runtime, agent(r)); err != nil {
			_, _ = s.store.AddComment(task.ID, "派发失败："+err.Error(), agent(r))
		}
	}
	writeJSON(w, http.StatusCreated, task)
}

type taskPatch struct {
	Version     *int    `json:"version"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Assignee    *string `json:"assignee"`
	ColumnID    *string `json:"columnId"`
	AfterID     string  `json:"afterId"`
	AtTop       bool    `json:"atTop"`
	Archived    *bool   `json:"archived"`
}

func (s *Server) handlePatchTask(w http.ResponseWriter, r *http.Request) {
	var req taskPatch
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Version == nil {
		writeBadRequest(w, "version is required")
		return
	}
	patch := store.TaskPatch{
		Title:       req.Title,
		Description: req.Description,
		Assignee:    req.Assignee,
		AfterID:     req.AfterID,
		AtTop:       req.AtTop,
		Archived:    req.Archived,
	}
	// An empty columnId means "leave the column alone", which lets clients
	// send a uniform patch body.
	if req.ColumnID != nil && strings.TrimSpace(*req.ColumnID) != "" {
		patch.ColumnID = req.ColumnID
	}
	task, err := s.store.PatchTask(r.PathValue("id"), *req.Version, patch, agent(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

type archiveRequest struct {
	Version *int `json:"version"`
}

func (s *Server) handleArchive(w http.ResponseWriter, r *http.Request) {
	var req archiveRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Version == nil {
		writeBadRequest(w, "version is required")
		return
	}
	task, err := s.store.ArchiveTask(r.PathValue("id"), *req.Version, agent(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

type dispatchRequest struct {
	Runtime string `json:"runtime"`
}

// handleReleaseExecution converges a card's execution record to detached so it
// stops holding the write lock. People use it when the original session is gone
// and the automatic Herdr grace window is not enough (or never applied). A run
// that may still be writing is refused: releasing it would allow a second
// writer on the same card.
func (s *Server) handleReleaseExecution(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	run, err := s.store.Execution(taskID)
	if err != nil {
		writeError(w, err)
		return
	}
	if run == nil {
		writeJSON(w, http.StatusNotFound, errorBody{Error: "no_execution"})
		return
	}
	if !run.Releasable() {
		writeJSON(w, http.StatusConflict, errorBody{Error: "execution_active"})
		return
	}
	detail := fmt.Sprintf("人工释放执行记录（%s）：原会话不可用，写锁已释放，可重新派发或评论跟进", agent(r))
	if _, err := s.store.ReleaseExecution(taskID, detail); err != nil {
		writeError(w, err)
		return
	}
	task, err := s.store.Task(taskID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

// handleDispatch starts an async execution of the task on an execution
// runtime. The body is optional: with no runtime named, dsh remains the
// default. The run's progress lands as normal task
// comments and column moves, so clients need no new event types.
func (s *Server) handleDispatch(w http.ResponseWriter, r *http.Request) {
	var req dispatchRequest
	if r.Body != nil {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			writeBadRequest(w, "read body: %v", err)
			return
		}
		if strings.TrimSpace(string(body)) != "" {
			if err := json.Unmarshal(body, &req); err != nil {
				writeBadRequest(w, "invalid json: %v", err)
				return
			}
		}
	}
	runtime := strings.TrimSpace(req.Runtime)
	if runtime == "" {
		runtime = executor.RuntimeDSH
	}
	taskID := r.PathValue("id")
	if err := s.dispatch.Dispatch(taskID, runtime, agent(r)); err != nil {
		if errors.Is(err, executor.ErrInProgress) {
			writeJSON(w, http.StatusConflict, errorBody{Error: "dispatch_in_progress"})
			return
		}
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"taskId": taskID, "runtime": runtime})
}

func (s *Server) handleStartProjectRun(w http.ResponseWriter, r *http.Request) {
	var req dispatchRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	run, err := s.dispatch.StartProjectRun(r.PathValue("id"), strings.TrimSpace(req.Runtime), agent(r))
	if err != nil {
		var busy *executor.ErrProjectBusy
		if errors.As(err, &busy) {
			writeJSON(w, http.StatusConflict, errorBody{Error: busy.Error()})
			return
		}
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) handleProjectRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.ProjectRun(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleStopProjectRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.dispatch.StopProjectRun(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleComments(w http.ResponseWriter, r *http.Request) {
	comments, err := s.store.Comments(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, comments)
}

type newComment struct {
	Text string `json:"text"`
	// Mention addresses the comment to one execution channel (@ in the
	// composer). Empty keeps the current behaviour: the card's session answers.
	Mention         string `json:"mention,omitempty"`
	ParentCommentID string `json:"parentCommentId,omitempty"`
}

func (s *Server) handleContinue(w http.ResponseWriter, r *http.Request) {
	if err := s.dispatch.Continue(r.PathValue("id"), r.PathValue("commentId"), agent(r)); err != nil {
		if errors.Is(err, executor.ErrInProgress) {
			writeJSON(w, http.StatusConflict, errorBody{Error: "原会话仍在执行或接收状态不确定，请先核对原会话"})
			return
		}
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"taskId": r.PathValue("id"), "commentId": r.PathValue("commentId")})
}

func (s *Server) handleAddComment(w http.ResponseWriter, r *http.Request) {
	var req newComment
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeBadRequest(w, "text is required")
		return
	}
	mention := strings.TrimSpace(req.Mention)
	if mention != "" && !executor.KnownRuntime(mention) {
		writeBadRequest(w, "%s", "unknown mention channel: "+mention)
		return
	}
	// An addressed comment on a card in review is actionable feedback. Return
	// that card to the project's intake column before the caller starts the
	// mentioned execution. Keep ordinary discussion in review.
	var returnToIntake *struct {
		task     model.Task
		columnID string
	}
	if mention != "" {
		task, err := s.store.Task(r.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		project, err := s.store.Project(task.ProjectID)
		if err != nil {
			writeError(w, err)
			return
		}
		if review, ok := project.ColumnByRole(model.ColumnRoleReview); ok && task.ColumnID == review.ID {
			if intake, ok := project.ColumnByRole(model.ColumnRoleIntake); ok {
				returnToIntake = &struct {
					task     model.Task
					columnID string
				}{task: task, columnID: intake.ID}
			}
		}
	}
	c, err := s.store.AddCommentWithParent(r.PathValue("id"), req.Text, agent(r), mention, strings.TrimSpace(req.ParentCommentID))
	if err != nil {
		writeError(w, err)
		return
	}
	if returnToIntake != nil {
		if _, err := s.store.MoveTask(returnToIntake.task.ID, returnToIntake.task.Version, returnToIntake.columnID, "", agent(r)); err != nil {
			writeError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleUpdateComment(w http.ResponseWriter, r *http.Request) {
	var req newComment
	if !decodeJSON(w, r, &req) {
		return
	}
	comment, err := s.store.UpdateComment(r.PathValue("id"), r.PathValue("commentId"), req.Text, agent(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, comment)
}

func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteComment(r.PathValue("id"), r.PathValue("commentId"), agent(r)); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
