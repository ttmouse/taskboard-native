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
	"net/url"
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
	// Global flags may come before the command: tb --agent rams list.
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		switch args[i] {
		case "-h", "--help":
			usage()
			return exitOK
		case "--agent", "--addr":
			i += 2
		default:
			i++
		}
	}
	if i >= len(args) {
		usage()
		return exitUsage
	}
	lead := args[:i]
	cmd := args[i]
	rest := append(append([]string{}, lead...), args[i+1:]...)
	switch cmd {
	case "help", "-h", "--help":
		usage()
		return exitOK
	case "projects":
		return cmdProjects(rest)
	case "list":
		return cmdList(rest)
	case "show":
		return cmdShow(rest)
	case "project":
		return cmdProject(rest)
	case "add":
		return cmdAdd(rest)
	case "move":
		return cmdMove(rest)
	case "column":
		return cmdColumn(rest)
	case "comment":
		return cmdComment(rest)
	case "followup":
		return cmdFollowup(rest)
	case "archive":
		return cmdArchive(rest)
	case "execution":
		return cmdExecution(rest)
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

  tb [--agent WHO] [--addr ADDR] <command>

  tb projects [--json]
  tb project add NAME [--path ABSOLUTE_DIRECTORY] [--json]
  tb list    [--project ID] [--column NAME|ID] [--assignee WHO] [--limit N --offset N] [--summary] [--json]
  tb show    <taskID> [--json]
  tb add     <title> [--project ID] [--column NAME|ID] [--body TEXT] [--assignee WHO]
             [--dispatch dsh|codex|pi|claude]
  tb move    <taskID> <column NAME|ID> [--top] [--after CARD_ID]
  tb column  <columnID> <role> [--json]   # role: intake|doing|review|done，空字符串清空
  tb comment <taskID> <body> [--mention dsh|codex|pi|claude]
  tb followup <taskID> <commentID>
  tb execution release <taskID>     # 执行记录收敛为 released（释放写锁）
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
	asJSON := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "tb projects: %v\n", err)
		return exitUsage
	}
	c, err := ensureClient(*addr, *agent)
	if err != nil {
		return fail(err)
	}
	projectsRaw, err := c.projectsRaw()
	if err != nil {
		return fail(err)
	}
	if *asJSON {
		fmt.Println(string(projectsRaw))
		return exitOK
	}
	var projects []projectInfo
	if err := json.Unmarshal(projectsRaw, &projects); err != nil {
		return fail(err)
	}
	for _, p := range projects {
		if p.WorkspacePath == "" {
			fmt.Printf("%s\t%s\n", p.ID, p.Name)
			continue
		}
		fmt.Printf("%s\t%s\t%s\n", p.ID, p.Name, p.WorkspacePath)
	}
	return exitOK
}

func cmdList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	project := fs.String("project", "", "project id")
	column := fs.String("column", "", "column name or id")
	assignee := fs.String("assignee", "", "exact assignee filter")
	limit := fs.Int("limit", 0, "maximum number of tasks (1-200)")
	offset := fs.Int("offset", 0, "number of matching tasks to skip")
	summary := fs.Bool("summary", false, "return summary fields without descriptions or comments")
	asJSON := fs.Bool("json", false, "output JSON")
	addr := fs.String("addr", "", "service address")
	agent := fs.String("agent", "", "agent identity")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "tb list: %v\n", err)
		return exitUsage
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "tb list: unexpected positional arguments")
		return exitUsage
	}
	limitSet, offsetSet := false, false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "limit":
			limitSet = true
		case "offset":
			offsetSet = true
		}
	})
	if limitSet && (*limit < 1 || *limit > 200) {
		fmt.Fprintln(os.Stderr, "tb list: --limit must be between 1 and 200")
		return exitUsage
	}
	if *offset < 0 {
		fmt.Fprintln(os.Stderr, "tb list: --offset must be non-negative")
		return exitUsage
	}
	if offsetSet && !limitSet {
		fmt.Fprintln(os.Stderr, "tb list: --offset requires --limit")
		return exitUsage
	}
	c, err := ensureClient(*addr, *agent)
	if err != nil {
		return fail(err)
	}
	projectID := *project
	var projects []projectInfo
	projectsLoaded := false
	loadProjects := func() error {
		if projectsLoaded {
			return nil
		}
		projects, err = c.projects()
		projectsLoaded = true
		return err
	}
	if projectID == "" {
		if err := loadProjects(); err != nil {
			return fail(err)
		}
		if len(projects) == 0 {
			fmt.Fprintln(os.Stderr, "tb list: no projects")
			return exitError
		}
		projectID = projects[0].ID
	}
	var columns []model.Column
	if *column != "" || !*asJSON {
		if err := loadProjects(); err != nil {
			return fail(err)
		}
		if p, ok := findProject(projects, projectID); ok {
			columns = p.Columns
		}
	}
	query := make(url.Values)
	query.Set("projectId", projectID)
	if *column != "" {
		columnID, err := resolveColumn(columns, *column)
		if err != nil {
			return fail(err)
		}
		query.Set("columnId", columnID)
	}
	if *assignee != "" {
		query.Set("assignee", *assignee)
	}
	if limitSet {
		query.Set("limit", fmt.Sprint(*limit))
		query.Set("offset", fmt.Sprint(*offset))
	}
	if *summary {
		query.Set("summary", "true")
	}
	tasksRaw, err := c.tasks(query)
	if err != nil {
		return fail(err)
	}
	if *asJSON {
		fmt.Println(string(tasksRaw))
		return exitOK
	}
	var tasks []model.Task
	if err := json.Unmarshal(tasksRaw, &tasks); err != nil {
		return fail(err)
	}
	names := make(map[string]string, len(columns))
	for _, col := range columns {
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

func cmdShow(args []string) int {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("addr", "", "service address")
	agent := fs.String("agent", "", "agent identity")
	asJSON := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(reorderArgs(args, "json")); err != nil {
		fmt.Fprintf(os.Stderr, "tb show: %v\n", err)
		return exitUsage
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "tb show: usage: tb show <taskID> [--json]")
		return exitUsage
	}
	c, err := ensureClient(*addr, *agent)
	if err != nil {
		return fail(err)
	}
	raw, err := c.taskRaw(fs.Arg(0))
	if err != nil {
		return fail(err)
	}
	if *asJSON {
		fmt.Println(string(raw))
		return exitOK
	}
	var task model.Task
	if err := json.Unmarshal(raw, &task); err != nil {
		return fail(err)
	}
	printTask(os.Stdout, task)
	return exitOK
}

func printTask(w io.Writer, task model.Task) {
	fmt.Fprintf(w, "ID:\t%s\nTitle:\t%s\nProject:\t%s\nColumn:\t%s\n", task.ID, task.Title, task.ProjectID, task.ColumnID)
	if task.Assignee != "" {
		fmt.Fprintf(w, "Assignee:\t%s\n", task.Assignee)
	}
	fmt.Fprintf(w, "Version:\tv%d\nUpdated:\t%s\nUpdated by:\t%s\nDescription:\n", task.Version, task.UpdatedAt.Format(time.RFC3339), task.UpdatedBy)
	printIndented(w, task.Description, "  ")
	fmt.Fprintf(w, "Comments (%d):\n", len(task.Comments))
	for _, comment := range task.Comments {
		fmt.Fprintf(w, "  [%s] %s:\n", comment.CreatedAt.Format(time.RFC3339), comment.Author)
		printIndented(w, comment.Text, "    ")
	}
}

func printIndented(w io.Writer, text, indent string) {
	if text == "" {
		fmt.Fprintf(w, "%s\n", indent)
		return
	}
	for _, line := range strings.Split(text, "\n") {
		fmt.Fprintf(w, "%s%s\n", indent, line)
	}
}

func cmdProject(args []string) int {
	addAt := 0
	for addAt < len(args) && args[addAt] != "add" {
		if (args[addAt] == "--addr" || args[addAt] == "--agent") && addAt+1 < len(args) {
			addAt += 2
			continue
		}
		break
	}
	if addAt >= len(args) || args[addAt] != "add" {
		fmt.Fprintln(os.Stderr, "tb project: usage: tb project add NAME [--path ABSOLUTE_DIRECTORY] [--json]")
		return exitUsage
	}
	fs := flag.NewFlagSet("project add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("path", "", "project workspace path")
	addr := fs.String("addr", "", "service address")
	agent := fs.String("agent", "", "agent identity")
	asJSON := fs.Bool("json", false, "output JSON")
	commandArgs := append(append([]string{}, args[:addAt]...), args[addAt+1:]...)
	if err := fs.Parse(reorderArgs(commandArgs, "json")); err != nil {
		fmt.Fprintf(os.Stderr, "tb project add: %v\n", err)
		return exitUsage
	}
	name := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if name == "" {
		fmt.Fprintln(os.Stderr, "tb project add: name required")
		return exitUsage
	}
	c, err := ensureClient(*addr, *agent)
	if err != nil {
		return fail(err)
	}
	raw, err := c.createProject(name, *path)
	if err != nil {
		return fail(err)
	}
	if *asJSON {
		fmt.Println(string(raw))
		return exitOK
	}
	var project projectInfo
	if err := json.Unmarshal(raw, &project); err != nil {
		return fail(err)
	}
	if project.WorkspacePath == "" {
		fmt.Printf("%s\t%s\n", project.ID, project.Name)
	} else {
		fmt.Printf("%s\t%s\t%s\n", project.ID, project.Name, project.WorkspacePath)
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
	dispatch := fs.String("dispatch", "", "create and dispatch to channel (dsh|codex|pi|claude)")
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
	}, *dispatch)
	if err != nil {
		return fail(err)
	}
	line := fmt.Sprintf("%s\t%s\t%s\tv%d", task.ID, task.ColumnID, task.Title, task.Version)
	if d := strings.TrimSpace(*dispatch); d != "" {
		line += "\t已派发 " + d
	}
	fmt.Println(line)
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

// cmdColumn labels a column with a workflow role (empty clears it). The
// column is located across projects by ID or name, so the caller does not
// have to know which project owns it.
func cmdColumn(args []string) int {
	fs := flag.NewFlagSet("column", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("addr", "", "service address")
	agent := fs.String("agent", "", "agent identity")
	asJSON := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(reorderArgs(args, "json")); err != nil {
		fmt.Fprintf(os.Stderr, "tb column: %v\n", err)
		return exitUsage
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "tb column: usage: tb column <columnID> <role>")
		return exitUsage
	}
	want, role := fs.Arg(0), fs.Arg(1)
	if !model.ValidColumnRole(role) {
		fmt.Fprintf(os.Stderr, "tb column: unknown role %q (use intake|doing|review|done, or \"\" to clear)\n", role)
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
	projectID, columnID := "", ""
	for _, p := range projects {
		for _, col := range p.Columns {
			if col.ID == want || col.Name == want {
				projectID, columnID = p.ID, col.ID
				break
			}
		}
		if projectID != "" {
			break
		}
	}
	if columnID == "" {
		return fail(fmt.Errorf("unknown column %q", want))
	}
	raw, err := c.setColumnRole(projectID, columnID, role)
	if err != nil {
		if err.Error() == "role_conflict" {
			return fail(fmt.Errorf("该角色已被其他列占用"))
		}
		return fail(err)
	}
	if *asJSON {
		fmt.Println(string(raw))
		return exitOK
	}
	var column model.Column
	if err := json.Unmarshal(raw, &column); err != nil {
		return fail(err)
	}
	fmt.Printf("%s\t%s\t%s\n", column.ID, column.Name, column.Role)
	return exitOK
}

func cmdComment(args []string) int {
	fs := flag.NewFlagSet("comment", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("addr", "", "service address")
	agent := fs.String("agent", "", "agent identity")
	mention := fs.String("mention", "", "address the comment to one channel: dsh|codex|pi|claude")
	if err := fs.Parse(reorderArgs(args)); err != nil {
		fmt.Fprintf(os.Stderr, "tb comment: %v\n", err)
		return exitUsage
	}
	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "tb comment: usage: tb comment <taskID> <body> [--mention dsh|codex|pi|claude]")
		return exitUsage
	}
	taskID := fs.Arg(0)
	body := strings.Join(fs.Args()[1:], " ")
	c, err := ensureClient(*addr, *agent)
	if err != nil {
		return fail(err)
	}
	comment, err := c.addComment(taskID, body, *mention)
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

// cmdExecution manages a card's execution record. release converges it to
// detached so a session that is gone for good stops blocking dispatch and
// comment follow-up.
func cmdExecution(args []string) int {
	fs := flag.NewFlagSet("execution", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("addr", "", "service address")
	agent := fs.String("agent", "", "agent identity")
	if err := fs.Parse(reorderArgs(args)); err != nil {
		fmt.Fprintf(os.Stderr, "tb execution: %v\n", err)
		return exitUsage
	}
	if fs.NArg() != 2 || fs.Arg(0) != "release" {
		fmt.Fprintln(os.Stderr, "tb execution: usage: tb execution release <taskID>")
		return exitUsage
	}
	c, err := ensureClient(*addr, *agent)
	if err != nil {
		return fail(err)
	}
	task, err := c.releaseExecution(fs.Arg(1))
	if err != nil {
		return fail(err)
	}
	fmt.Printf("%s\tdetached\tv%d\n", task.ID, task.Version)
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

func findProject(projects []projectInfo, id string) (projectInfo, bool) {
	for _, project := range projects {
		if project.ID == id {
			return project, true
		}
	}
	return projectInfo{}, false
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

// projectInfo is the read-only project shape used by the CLI. Keeping the
// workspace path here also lets older model builds preserve that API field.
type projectInfo struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	WorkspacePath string         `json:"workspacePath,omitempty"`
	Columns       []model.Column `json:"columns"`
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

func (c *client) projectsRaw() (json.RawMessage, error) {
	var out json.RawMessage
	return out, c.get("/api/projects", &out)
}

func (c *client) projects() ([]projectInfo, error) {
	raw, err := c.projectsRaw()
	if err != nil {
		return nil, err
	}
	var out []projectInfo
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *client) board(projectID string) (service.Board, error) {
	var out service.Board
	return out, c.get("/api/projects/"+projectID+"/board", &out)
}

func (c *client) task(id string) (model.Task, error) {
	var out model.Task
	return out, c.get("/api/tasks/"+id, &out)
}

func (c *client) taskRaw(id string) (json.RawMessage, error) {
	var out json.RawMessage
	return out, c.get("/api/tasks/"+url.PathEscape(id), &out)
}

func (c *client) tasks(query url.Values) (json.RawMessage, error) {
	var out json.RawMessage
	return out, c.get("/api/tasks?"+query.Encode(), &out)
}

func (c *client) createProject(name, workspacePath string) (json.RawMessage, error) {
	body := map[string]string{"name": name}
	if workspacePath != "" {
		body["workspacePath"] = workspacePath
	}
	var out json.RawMessage
	return out, c.send(http.MethodPost, "/api/projects", body, &out, http.StatusCreated)
}

// createTask creates a card; a non-empty dispatchRuntime additionally hands
// it to that channel in the same call (the service validates the runtime and
// the project's workspacePath before creating).
func (c *client) createTask(task model.Task, dispatchRuntime string) (model.Task, error) {
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
	if d := strings.TrimSpace(dispatchRuntime); d != "" {
		body["dispatchRuntime"] = d
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

func (c *client) addComment(taskID, text, mention string) (model.Comment, error) {
	body := map[string]any{"text": text}
	if mention != "" {
		body["mention"] = mention
	}
	var out model.Comment
	return out, c.send(http.MethodPost, "/api/tasks/"+taskID+"/comments", body, &out, http.StatusCreated)
}

// setColumnRole writes a column's workflow role and returns the updated
// column as the service sent it (role "" clears it).
func (c *client) setColumnRole(projectID, columnID, role string) (json.RawMessage, error) {
	body := map[string]any{"role": role}
	var out json.RawMessage
	path := "/api/projects/" + url.PathEscape(projectID) + "/columns/" + url.PathEscape(columnID)
	return out, c.send(http.MethodPatch, path, body, &out, http.StatusOK)
}

func (c *client) archiveTask(taskID string) (model.Task, error) {
	current, err := c.task(taskID)
	if err != nil {
		return model.Task{}, err
	}
	var out model.Task
	return out, c.send(http.MethodPost, "/api/tasks/"+taskID+"/archive", map[string]any{"version": current.Version}, &out, http.StatusOK)
}

// releaseExecution converges the card's execution record to detached so a
// session that is gone for good stops blocking dispatch and follow-up.
func (c *client) releaseExecution(taskID string) (model.Task, error) {
	var out model.Task
	return out, c.send(http.MethodPost, "/api/tasks/"+taskID+"/execution/release", nil, &out, http.StatusOK)
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
