// Package executor runs kanban tasks on execution runtimes. v1 ships one
// runtime: DSH headless (`dsh headless "<task text>"`) — one process per
// dispatch, cwd is the project's workspacePath, and every state change goes
// back through the store so GUI/CLI/MCP see it via the normal SSE events.
package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"taskboard-native/internal/model"
	"taskboard-native/internal/store"
)

// RuntimeDSH is the only supported runtime in v1.
const RuntimeDSH = "dsh"

// ErrInProgress reports that the task already has a running execution.
var ErrInProgress = errors.New("dispatch already in progress")

// outputTail is how much of a finished process's output is kept in the
// result comment. Diagnostics and answers can be long; the board wants the
// conclusion, not the log.
const outputTail = 800

// Dispatcher starts and tracks task executions. It lives inside the service
// process: the service is the store's only writer, and the dispatcher is
// part of the service.
type Dispatcher struct {
	store *store.Store

	mu       sync.Mutex
	inflight map[string]*execution // taskID -> execution
	closed   bool
}

// NewDispatcher returns a dispatcher writing back through st.
func NewDispatcher(st *store.Store) *Dispatcher {
	return &Dispatcher{store: st, inflight: map[string]*execution{}}
}

// execution is one in-flight DSH run.
type execution struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// Dispatch validates the request and starts an async run. It returns as
// soon as the process is launched; progress lands as task comments and
// column moves. Caller mistakes return *store.InvalidError; a task that
// already has a run in flight returns ErrInProgress.
func (d *Dispatcher) Dispatch(taskID, runtime, by string) error {
	if runtime != RuntimeDSH {
		return &store.InvalidError{Msg: fmt.Sprintf("unknown runtime %q (supported: %s)", runtime, RuntimeDSH)}
	}
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return &store.InvalidError{Msg: "dispatcher is closed"}
	}
	if _, busy := d.inflight[taskID]; busy {
		d.mu.Unlock()
		return ErrInProgress
	}
	task, err := d.store.Task(taskID)
	if err != nil {
		d.mu.Unlock()
		return err
	}
	if task.Archived {
		d.mu.Unlock()
		return &store.InvalidError{Msg: "task is archived"}
	}
	project, err := d.store.Project(task.ProjectID)
	if err != nil {
		d.mu.Unlock()
		return err
	}
	dir := strings.TrimSpace(project.WorkspacePath)
	if dir == "" {
		d.mu.Unlock()
		return &store.InvalidError{Msg: fmt.Sprintf("project %q has no workspacePath; set one before dispatching", project.Name)}
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		d.mu.Unlock()
		return &store.InvalidError{Msg: fmt.Sprintf("workspacePath %q is not a directory on this machine", dir)}
	}
	ctx, cancel := context.WithCancel(context.Background())
	ex := &execution{cancel: cancel, done: make(chan struct{})}
	d.inflight[taskID] = ex
	d.mu.Unlock()

	go func() {
		defer close(ex.done)
		defer func() {
			d.mu.Lock()
			delete(d.inflight, taskID)
			d.mu.Unlock()
		}()
		d.run(ctx, task, project, dir, by)
	}()
	return nil
}

// Close cancels every running execution and waits (bounded) for the
// goroutines to wind down.
func (d *Dispatcher) Close() {
	d.mu.Lock()
	d.closed = true
	for _, ex := range d.inflight {
		ex.cancel()
	}
	list := make([]*execution, 0, len(d.inflight))
	for _, ex := range d.inflight {
		list = append(list, ex)
	}
	d.mu.Unlock()
	for _, ex := range list {
		select {
		case <-ex.done:
		case <-time.After(5 * time.Second):
		}
	}
}

// run carries one task from claim to result. Every failure path still
// comments on the task — a silent dispatch is worse than a failed one.
func (d *Dispatcher) run(ctx context.Context, task model.Task, project model.Project, dir, by string) {
	text := BuildTaskText(task, project)
	if err := d.comment(task.ID, by, fmt.Sprintf("派发 %s 执行（工作目录 %s）…", RuntimeDSH, dir)); err != nil {
		return // store is gone; nothing to write back to
	}
	// Claim: move to 进行中 unless it is already there.
	if err := d.claim(task, by); err != nil {
		d.comment(task.ID, by, fmt.Sprintf("派发后认领失败：%v；继续执行。", err))
	}

	start := time.Now()
	cmd, err := execCommand(ctx, dir, text)
	if err != nil {
		d.comment(task.ID, by, fmt.Sprintf("派发失败：找不到 %s 可执行文件（%v）。", RuntimeDSH, err))
		return
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	elapsed := time.Since(start).Round(time.Second)

	exit := 0
	if runErr != nil {
		exit = -1
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			exit = ee.ExitCode()
		}
	}
	if runErr == nil {
		body := fmt.Sprintf("%s 执行完成（%s）。输出：\n\n%s",
			RuntimeDSH, elapsed, tail(strings.TrimSpace(stdout.String()), outputTail))
		if strings.TrimSpace(stdout.String()) == "" {
			body = fmt.Sprintf("%s 执行完成（%s），无输出。", RuntimeDSH, elapsed)
		}
		d.comment(task.ID, by, body)
		d.moveToReview(task, by)
		return
	}
	if ctx.Err() != nil {
		d.comment(task.ID, by, fmt.Sprintf("%s 执行被取消（服务关闭），已运行 %s；任务留在原列。", RuntimeDSH, elapsed))
		return
	}
	detail := tail(strings.TrimSpace(stderr.String()), outputTail)
	if detail == "" {
		detail = tail(strings.TrimSpace(stdout.String()), outputTail)
	}
	body := fmt.Sprintf("%s 执行失败（exit %d，%s）。", RuntimeDSH, exit, elapsed)
	if detail != "" {
		body += "诊断：\n\n" + detail
	}
	d.comment(task.ID, by, body)
}

// claim moves the task to the project's 进行中 column, tolerating a version
// bump by re-reading once. A task already in 进行中 stays put.
func (d *Dispatcher) claim(task model.Task, by string) error {
	project, err := d.store.Project(task.ProjectID)
	if err != nil {
		return err
	}
	target, ok := columnByName(project, "进行中")
	if !ok {
		return fmt.Errorf("project %q has no 进行中 column", project.Name)
	}
	if task.ColumnID == target {
		return nil
	}
	for range 2 {
		cur, err := d.store.Task(task.ID)
		if err != nil {
			return err
		}
		if cur.ColumnID == target {
			return nil
		}
		if _, err := d.store.MoveTask(cur.ID, cur.Version, target, "", by); err == nil {
			return nil
		}
	}
	return fmt.Errorf("could not move task to 进行中 after retry")
}

// moveToReview mirrors claim for the 评审 column; a failed move only costs
// the comment, not the result.
func (d *Dispatcher) moveToReview(task model.Task, by string) {
	project, err := d.store.Project(task.ProjectID)
	if err != nil {
		return
	}
	target, ok := columnByName(project, "评审")
	if !ok {
		return
	}
	for range 2 {
		cur, err := d.store.Task(task.ID)
		if err != nil {
			return
		}
		if cur.ColumnID == target {
			return
		}
		if _, err := d.store.MoveTask(cur.ID, cur.Version, target, "", by); err == nil {
			return
		}
	}
}

func (d *Dispatcher) comment(taskID, by, text string) error {
	_, err := d.store.AddComment(taskID, text, by)
	return err
}

// columnByName finds a column id by display name.
func columnByName(p model.Project, name string) (string, bool) {
	for _, c := range p.Columns {
		if c.Name == name {
			return c.ID, true
		}
	}
	return "", false
}

// BuildTaskText renders the self-contained prompt handed to the runtime.
// The dispatched session has no conversation context; the card must carry
// everything it needs.
func BuildTaskText(task model.Task, project model.Project) string {
	var b strings.Builder
	b.WriteString("【看板任务派发】\n")
	b.WriteString("项目：" + project.Name + "（本目录即项目工作目录）\n")
	b.WriteString("任务标题：" + task.Title + "\n")
	if body := strings.TrimSpace(task.Description); body != "" {
		b.WriteString("\n任务正文：\n" + body + "\n")
	}
	b.WriteString(`
要求：
1. 直接在本目录完成上述任务。
2. 完成后输出：变更摘要（改了哪些文件/做了什么）、验证方式与结果、遗留风险。
3. 不要向看板写评论或移动卡片；执行结果由派发方回写。`)
	return b.String()
}

// tail keeps the last n runes of s (whole runes, no mid-character cut).
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "…\n" + string(r[len(r)-n:])
}
