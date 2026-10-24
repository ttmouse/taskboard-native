// Command tb is the taskboard CLI: a thin client of the local HTTP service.
// It starts the service in the background when it is not already running.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"taskboard-native/internal/model"
	"taskboard-native/internal/service"
)

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return exitUsage
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "help", "-h", "--help":
		usage()
		return exitOK
	case "projects":
		return cmdProjects(rest)
	case "list":
		return cmdList(rest)
	case "add":
		return cmdAdd(rest)
	case "move":
		return cmdMove(rest)
	case "comment":
		return cmdComment(rest)
	case "archive":
		return cmdArchive(rest)
	case "serve":
		return cmdServe(rest)
	default:
		fmt.Fprintf(os.Stderr, "tb: unknown command %q\n\n", cmd)
		usage()
		return exitUsage
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `taskboard CLI

  tb projects
  tb list    [--project ID]
  tb add     <title> [--project ID] [--column NAME|ID] [--body TEXT] [--assignee WHO]
  tb move    <taskID> <column NAME|ID> [--top] [--after CARD_ID]
  tb comment <taskID> <body>
  tb archive <taskID>
  tb serve   [--addr ADDR] [--db PATH] [--detach]

Environment: TASKBOARD_ADDR, TASKBOARD_DB, TB_AGENT.
Every command accepts --agent WHO; the default identity is "local".
`)
}

func cmdProjects(args []string) int {
	fs := flag.NewFlagSet("projects", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("addr", "", "service address")
	agent := fs.String("agent", "", "agent identity")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "tb projects: %v\n", err)
		return exitUsage
	}
	c, err := ensureClient(*addr, *agent)
	if err != nil {
		return fail(err)
	}
	projects, err := c.projects()
	if err != nil {
		return fail(err)
	}
	for _, p := range projects {
		fmt.Printf("%s\t%s\n", p.ID, p.Name)
	}
	return exitOK
}

func cmdList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	project := fs.String("project", "", "project id")
	addr := fs.String("addr", "", "service address")
	agent := fs.String("agent", "", "agent identity")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "tb list: %v\n", err)
		return exitUsage
	}
	c, err := ensureClient(*addr, *agent)
	if err != nil {
		return fail(err)
	}
	projectID := *project
	if projectID == "" {
		projects, err := c.projects()
		if err != nil {
			return fail(err)
		}
		if len(projects) == 0 {
			fmt.Fprintln(os.Stderr, "tb list: no projects")
			return exitError
		}
		projectID = projects[0].ID
	}
	tasks, err := c.tasks(projectID)
	if err != nil {
		return fail(err)
	}
	board, err := c.board(projectID)
	if err != nil {
		return fail(err)
	}
	names := make(map[string]string, len(board.Columns))
	for _, col := range board.Columns {
		names[col.ID] = col.Name
	}
	for _, t := range tasks {
		column := names[t.ColumnID]
		if column == "" {
			column = t.ColumnID
		}
		fmt.Printf("%s\t%s\t%s\tv%d\t%s\n", t.ID, column, t.Title, t.Version, t.UpdatedBy)
	}
	return exitOK
}

func cmdAdd(args []string) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	project := fs.String("project", "", "project id")
	column := fs.String("column", "", "column name or id")
	body := fs.String("body", "", "task description")
	assignee := fs.String("assignee", "", "assignee")
	addr := fs.String("addr", "", "service address")
	agent := fs.String("agent", "", "agent identity")
	// The title is positional; flags may appear before or after it.
	if err := fs.Parse(reorderArgs(args)); err != nil {
		fmt.Fprintf(os.Stderr, "tb add: %v\n", err)
		return exitUsage
	}
	title := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if title == "" {
		fmt.Fprintln(os.Stderr, "tb add: title required")
		return exitUsage
	}
	c, err := ensureClient(*addr, *agent)
	if err != nil {
		return fail(err)
	}
	projectID := *project
	if projectID == "" {
		projects, err := c.projects()
		if err != nil {
			return fail(err)
		}
		if len(projects) == 0 {
			return fail(errors.New("no projects"))
		}
		projectID = projects[0].ID
	}
	columnID := ""
	if *column != "" {
		board, err := c.board(projectID)
		if err != nil {
			return fail(err)
		}
		columnID, err = resolveColumn(board.Columns, *column)
		if err != nil {
			return fail(err)
		}
	}
	task, err := c.createTask(model.Task{
		ProjectID:   projectID,
		ColumnID:    columnID,
		Title:       title,
		Description: *body,
		Assignee:    *assignee,
	})
	if err != nil {
		return fail(err)
	}
	fmt.Printf("%s\t%s\t%s\tv%d\n", task.ID, task.ColumnID, task.Title, task.Version)
	return exitOK
}

func cmdMove(args []string) int {
	fs := flag.NewFlagSet("move", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("addr", "", "service address")
	agent := fs.String("agent", "", "agent identity")
	top := fs.Bool("top", false, "place at the top of the target column")
	after := fs.String("after", "", "place right after this card id")
	if err := fs.Parse(reorderArgs(args, "top")); err != nil {
		fmt.Fprintf(os.Stderr, "tb move: %v\n", err)
		return exitUsage
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "tb move: usage: tb move <taskID> <column NAME|ID>")
		return exitUsage
	}
	taskID, want := fs.Arg(0), fs.Arg(1)
	c, err := ensureClient(*addr, *agent)
	if err != nil {
		return fail(err)
	}
	task, err := c.task(taskID)
	if err != nil {
		return fail(err)
	}
	board, err := c.board(task.ProjectID)
	if err != nil {
		return fail(err)
	}
	columnID, err := resolveColumn(board.Columns, want)
	if err != nil {
		return fail(err)
	}
	moved, err := c.patchTask(task, patch{ColumnID: columnID, AtTop: *top, AfterID: *after})
	if err != nil {
		return fail(err)
	}
	fmt.Printf("%s\t%s\tv%d\n", moved.ID, moved.ColumnID, moved.Version)
	return exitOK
}

func cmdComment(args []string) int {
	fs := flag.NewFlagSet("comment", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("addr", "", "service address")
	agent := fs.String("agent", "", "agent identity")
	if err := fs.Parse(reorderArgs(args)); err != nil {
		fmt.Fprintf(os.Stderr, "tb comment: %v\n", err)
		return exitUsage
	}
	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "tb comment: usage: tb comment <taskID> <body>")
		return exitUsage
	}
	taskID := fs.Arg(0)
	body := strings.Join(fs.Args()[1:], " ")
	c, err := ensureClient(*addr, *agent)
	if err != nil {
		return fail(err)
	}
	comment, err := c.addComment(taskID, body)
	if err != nil {
		return fail(err)
	}
	fmt.Printf("%s\t%s\t%s\n", comment.ID, comment.Author, comment.Text)
	return exitOK
}

func cmdArchive(args []string) int {
	fs := flag.NewFlagSet("archive", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("addr", "", "service address")
	agent := fs.String("agent", "", "agent identity")
	if err := fs.Parse(reorderArgs(args)); err != nil {
		fmt.Fprintf(os.Stderr, "tb archive: %v\n", err)
		return exitUsage
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "tb archive: usage: tb archive <taskID>")
		return exitUsage
	}
	c, err := ensureClient(*addr, *agent)
	if err != nil {
		return fail(err)
	}
	task, err := c.archiveTask(fs.Arg(0))
	if err != nil {
		return fail(err)
	}
	fmt.Printf("%s\tarchived\tv%d\n", task.ID, task.Version)
	return exitOK
}

// cmdServe runs the service in the foreground, or detached in the background.
func cmdServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	addr := fs.String("addr", "", "listen address")
	db := fs.String("db", "", "database path")
	detach := fs.Bool("detach", false, "run in the background")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	listen := *addr
	if listen == "" {
		listen = service.DefaultListenAddr()
	}
	if *detach {
		return serveDetached(listen, *db)
	}
	srv, err := service.StartOptions(service.Options{Addr: listen, DBPath: *db})
	if err != nil {
		return fail(err)
	}
	defer srv.Close()
	fmt.Printf("taskboard listening on %s (db %s)\n", srv.Addr, srv.DBPath)
	waitForSignal()
	fmt.Println("taskboard stopped")
	return exitOK
}

// serveDetached re-executes this binary with `serve` and waits for health.
// An already-running service at addr is reported instead of started twice.
func serveDetached(addr, db string) int {
	if err := clientFor(addr).health(); err == nil {
		fmt.Printf("taskboard already listening on %s\n", addr)
		return exitOK
	}
	exe, err := os.Executable()
	if err != nil {
		return fail(err)
	}
	args := []string{"serve", "--addr", addr}
	if db != "" {
		args = append(args, "--db", db)
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = os.Environ()
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = detachedAttr()
	if err := cmd.Start(); err != nil {
		return fail(fmt.Errorf("start background service: %w", err))
	}
	// Do not wait on the child: it outlives this process.
	if err := cmd.Process.Release(); err != nil {
		return fail(err)
	}
	if err := waitHealthy(clientFor(addr), 3*time.Second); err != nil {
		return fail(fmt.Errorf("background service did not become healthy: %w", err))
	}
	fmt.Printf("taskboard listening on %s\n", addr)
	return exitOK
}

// reorderArgs moves flags ahead of positional arguments so that
// `tb add "标题" --column 完成` parses both. isBoolFlags names the flags that
// take no value; everything after a bare "--" stays positional.
func reorderArgs(args []string, boolFlags ...string) []string {
	isBool := make(map[string]bool, len(boolFlags))
	for _, f := range boolFlags {
		isBool["-"+f] = true
		isBool["--"+f] = true
	}
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if len(arg) < 2 || !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
			continue
		}
		flags = append(flags, arg)
		if strings.Contains(arg, "=") || isBool[arg] {
			continue
		}
		if i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, positional...)
}

func resolveColumn(columns []model.Column, want string) (string, error) {
	for _, c := range columns {
		if c.ID == want || c.Name == want {
			return c.ID, nil
		}
	}
	names := make([]string, 0, len(columns))
	for _, c := range columns {
		names = append(names, fmt.Sprintf("%s(%s)", c.Name, c.ID))
	}
	return "", fmt.Errorf("unknown column %q (available: %s)", want, strings.Join(names, ", "))
}

func fail(err error) int {
	fmt.Fprintf(os.Stderr, "tb: %v\n", err)
	return exitError
}

// --- client ---

type client struct {
	base  string
	agent string
	http  *http.Client
}

func clientFor(addr string) *client {
	return &client{
		base:  "http://" + addr,
		agent: identity(""),
		http:  &http.Client{Timeout: 10 * time.Second},
	}
}

// identity resolves the agent name: flag, TB_AGENT, then "local".
func identity(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if a := os.Getenv("TB_AGENT"); a != "" {
		return a
	}
	return "local"
}

// ensureClient builds a client and makes sure the service is reachable,
// starting it detached when health fails.
func ensureClient(addr, agentFlag string) (*client, error) {
	if addr == "" {
		addr = service.DefaultListenAddr()
	}
	c := clientFor(addr)
	c.agent = identity(agentFlag)
	if err := waitHealthy(c, 300*time.Millisecond); err == nil {
		return c, nil
	}
	if err := startDetached(addr); err != nil {
		return nil, err
	}
	if err := waitHealthy(c, 3*time.Second); err != nil {
		return nil, fmt.Errorf("service at %s is not healthy: %w", addr, err)
	}
	return c, nil
}

// startDetached spawns `tb serve --detach` for addr and returns immediately.
func startDetached(addr string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "serve", "--detach", "--addr", addr)
	cmd.Env = os.Environ()
	cmd.SysProcAttr = detachedAttr()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}
	return cmd.Process.Release()
}

func waitHealthy(c *client, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last error
	for {
		if err := c.health(); err == nil {
			return nil
		} else {
			last = err
		}
		if time.Now().After(deadline) {
			return last
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (c *client) health() error {
	resp, err := c.do(http.MethodGet, "/api/health", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health returned %s", resp.Status)
	}
	return nil
}

func (c *client) projects() ([]model.Project, error) {
	var out []model.Project
	return out, c.get("/api/projects", &out)
}

func (c *client) board(projectID string) (service.Board, error) {
	var out service.Board
	return out, c.get("/api/projects/"+projectID+"/board", &out)
}

func (c *client) tasks(projectID string) ([]model.Task, error) {
	var out []model.Task
	return out, c.get("/api/tasks?projectId="+projectID, &out)
}

func (c *client) task(id string) (model.Task, error) {
	var out model.Task
	return out, c.get("/api/tasks/"+id, &out)
}

func (c *client) createTask(task model.Task) (model.Task, error) {
	body := map[string]any{
		"projectId": task.ProjectID,
		"columnId":  task.ColumnID,
		"title":     task.Title,
	}
	if task.Description != "" {
		body["description"] = task.Description
	}
	if task.Assignee != "" {
		body["assignee"] = task.Assignee
	}
	var out model.Task
	return out, c.send(http.MethodPost, "/api/tasks", body, &out, http.StatusCreated)
}

// patch is the subset of task fields the CLI can change.
type patch struct {
	Title       string
	Description string
	Assignee    string
	ColumnID    string
	AfterID     string
	AtTop       bool
}

// patchTask sends only the fields set on patch, always with the version the
// caller fetched. A moved card lands at the bottom unless AtTop/AfterID say
// otherwise.
func (c *client) patchTask(current model.Task, p patch) (model.Task, error) {
	body := map[string]any{"version": current.Version}
	if p.Title != "" {
		body["title"] = p.Title
	}
	if p.Description != "" {
		body["description"] = p.Description
	}
	if p.Assignee != "" {
		body["assignee"] = p.Assignee
	}
	if p.ColumnID != "" {
		body["columnId"] = p.ColumnID
	}
	if p.AfterID != "" {
		body["afterId"] = p.AfterID
	}
	if p.AtTop {
		body["atTop"] = true
	}
	var out model.Task
	return out, c.send(http.MethodPatch, "/api/tasks/"+current.ID, body, &out, http.StatusOK)
}

func (c *client) addComment(taskID, text string) (model.Comment, error) {
	var out model.Comment
	return out, c.send(http.MethodPost, "/api/tasks/"+taskID+"/comments", map[string]any{"text": text}, &out, http.StatusCreated)
}

func (c *client) archiveTask(taskID string) (model.Task, error) {
	current, err := c.task(taskID)
	if err != nil {
		return model.Task{}, err
	}
	var out model.Task
	return out, c.send(http.MethodPost, "/api/tasks/"+taskID+"/archive", map[string]any{"version": current.Version}, &out, http.StatusOK)
}

func (c *client) get(path string, out any) error {
	resp, err := c.do(http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return responseError(resp)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *client) send(method, path string, body any, out any, want int) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	resp, err := c.do(method, path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		return responseError(resp)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *client) do(method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Agent", c.agent)
	return c.http.Do(req)
}

// responseError turns an API error body into a readable error, naming the
// current version on conflicts.
func responseError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var body struct {
		Error string      `json:"error"`
		Task  *model.Task `json:"task"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || body.Error == "" {
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	if resp.StatusCode == http.StatusConflict && body.Task != nil {
		return fmt.Errorf("version conflict: task %s is at v%d", body.Task.ID, body.Task.Version)
	}
	return errors.New(body.Error)
}
