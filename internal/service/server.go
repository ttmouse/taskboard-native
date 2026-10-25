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
	st.OnEvent = h.publish

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		st.Close()
		return nil, err
	}
	s := &Server{
		Addr:     ln.Addr().String(),
		Port:     ln.Addr().(*net.TCPAddr).Port,
		DBPath:   dbPath,
		store:    st,
		hub:      h,
		slow:     slow,
		dispatch: executor.NewDispatcher(st),
		closed:   make(chan struct{}),
		listener: ln,
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
	mux.HandleFunc("GET /api/projects/{id}/board", s.handleBoard)
	mux.HandleFunc("GET /api/tasks", s.handleTasks)
	mux.HandleFunc("POST /api/tasks", s.handleCreateTask)
	mux.HandleFunc("GET /api/tasks/{id}", s.handleTask)
	mux.HandleFunc("PATCH /api/tasks/{id}", s.handlePatchTask)
	mux.HandleFunc("POST /api/tasks/{id}/archive", s.handleArchive)
	mux.HandleFunc("POST /api/tasks/{id}/dispatch", s.handleDispatch)
	mux.HandleFunc("GET /api/tasks/{id}/comments", s.handleComments)
	mux.HandleFunc("POST /api/tasks/{id}/comments", s.handleAddComment)
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
	switch {
	case errors.As(err, &conflict):
		cur := conflict.Current
		writeJSON(w, http.StatusConflict, errorBody{Error: "version_conflict", Task: &cur})
	case errors.As(err, &invalid):
		writeJSON(w, http.StatusBadRequest, errorBody{Error: err.Error()})
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Error: "not_found"})
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
}

// checkWorkspacePath validates an optional workspace path: empty means
// "none"; otherwise it must be an existing absolute directory. Returns the
// cleaned path. Shared by project create and project edit.
func checkWorkspacePath(w http.ResponseWriter, raw string) (string, bool) {
	if raw == "" {
		return "", true
	}
	if !filepath.IsAbs(raw) {
		writeBadRequest(w, "workspacePath must be an absolute directory")
		return "", false
	}
	cleaned := filepath.Clean(raw)
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
	project, err := s.store.CreateProjectWithPath(req.Name, workspacePath, agent(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, project)
}

type projectPatch struct {
	Name          *string `json:"name"`
	WorkspacePath *string `json:"workspacePath"`
}

// handlePatchProject edits a project's name and/or workspace path. Absent
// fields stay unchanged; an empty workspacePath clears the path. Path
// validation matches project creation.
func (s *Server) handlePatchProject(w http.ResponseWriter, r *http.Request) {
	var req projectPatch
	if !decodeJSON(w, r, &req) {
		return
	}
	patch := store.ProjectPatch{Name: req.Name, WorkspacePath: req.WorkspacePath}
	if req.WorkspacePath != nil && *req.WorkspacePath != "" {
		cleaned, ok := checkWorkspacePath(w, *req.WorkspacePath)
		if !ok {
			return
		}
		patch.WorkspacePath = &cleaned
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
	task, err := s.store.CreateTask(req.ProjectID, req.ColumnID, req.Title, req.Description, req.Assignee, agent(r))
	if err != nil {
		writeError(w, err)
		return
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

// handleDispatch starts an async execution of the task on an execution
// runtime. The body is optional: with no runtime named, the only runtime
// the service ships (dsh) is used. The run's progress lands as normal task
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
	c, err := s.store.AddComment(r.PathValue("id"), req.Text, agent(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}
