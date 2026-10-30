// Package executor runs kanban tasks in the DSH desktop or Herdr-hosted agents.
// The cwd is the project's workspacePath, and every state change goes
// back through the store so GUI/CLI/MCP see it via the normal SSE events.
package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"taskboard-native/internal/model"
	"taskboard-native/internal/store"
)

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
	store             *store.Store
	runtimeConfigPath string
	boardURL          string

	mu           sync.Mutex
	projectRunMu sync.Mutex
	inflight     map[string]*execution // taskID -> execution
	closed       bool
	recoveryErr  error
}

// NewDispatcher returns a dispatcher writing back through st.
func NewDispatcher(st *store.Store, configPaths ...string) *Dispatcher {
	d := &Dispatcher{store: st, inflight: map[string]*execution{}, boardURL: "http://127.0.0.1:47826"}
	if len(configPaths) > 0 {
		d.runtimeConfigPath = configPaths[0]
	}
	if len(configPaths) > 1 && configPaths[1] != "" {
		d.boardURL = strings.TrimRight(configPaths[1], "/")
	}
	if err := d.importDesktopReceipts(); err != nil {
		d.recoveryErr = err
		return d
	}
	runs, err := st.ActiveExecutions()
	if err != nil {
		d.recoveryErr = err
		return d
	}
	for _, run := range runs {
		if _, ok := herdrAddress(run.SessionID); ok {
			d.resumeHerdrTracking(run)
			continue
		}
		if run.SessionID == "" && (run.Status == "submitting" || run.Status == "unknown") {
			// Herdr may have accepted the tab creation before its response was
			// lost. There is no address to reconnect to, so block redispatch and
			// leave the uncertainty for a human to resolve.
			continue
		}
		if run.Channel() != RuntimeDSH {
			run.Status = "disconnected"
			run.Detail = "服务重启，CLI 执行已中断；请核对原会话，不自动重跑"
			if err := st.SaveExecution(run); err != nil {
				d.recoveryErr = err
				return d
			}
			continue
		}
		if err := d.Dispatch(run.TaskID, RuntimeDSH, "恢复桌面跟踪"); err != nil {
			d.comment(run.TaskID, "DSH", "恢复桌面跟踪失败："+err.Error())
		}
	}
	if err := d.recoverReturnedHerdrExecutions(); err != nil {
		d.recoveryErr = err
	}
	return d
}

func (d *Dispatcher) recoverReturnedHerdrExecutions() error {
	runs, err := d.store.ReturnedHerdrExecutions()
	if err != nil {
		return err
	}
	for _, run := range runs {
		address, ok := herdrAddress(run.SessionID)
		if !ok {
			continue
		}
		status, err := herdrGet(context.Background(), address)
		if err != nil {
			continue
		}
		state := herdrState(status)
		if state != "working" && state != "blocked" && !(status.Turn == 0 && status.ExitCode == nil) {
			if run.Status == "returned" {
				if err := d.recoverProjectRunAfterReturnedHerdr(run); err != nil {
					return err
				}
			}
			continue
		}
		if state == "blocked" {
			run.Status, run.Detail = "waiting_approval", "Herdr agent 等待人工处理"
		} else {
			run.Status, run.Detail = "running", "恢复 Herdr 原会话跟踪"
		}
		if err := d.store.SaveExecution(run); err != nil {
			return err
		}
		task, err := d.store.Task(run.TaskID)
		if err != nil {
			return err
		}
		projectRun, err := d.store.ProjectRun(task.ProjectID)
		if err != nil && err != store.ErrNotFound {
			return err
		}
		if err == nil && projectRun.Status == "failed" && projectRun.CurrentTaskID == run.TaskID && strings.Contains(projectRun.Detail, "执行结束但仍在进行中") {
			projectRun.Status, projectRun.Detail = "running", "已恢复 Herdr 原会话跟踪"
			if err := d.store.SaveProjectRun(projectRun); err != nil {
				return err
			}
		}
		d.resumeHerdrTracking(run)
	}
	return nil
}

func (d *Dispatcher) recoverProjectRunAfterReturnedHerdr(completed model.Execution) error {
	task, err := d.store.Task(completed.TaskID)
	if err != nil {
		return err
	}
	project, err := d.store.Project(task.ProjectID)
	if err != nil {
		return err
	}
	doing, hasDoing := project.ColumnByRole(model.ColumnRoleDoing)
	if !hasDoing || task.Archived {
		return nil
	}
	run, err := d.store.ProjectRun(task.ProjectID)
	if err == store.ErrNotFound {
		return nil
	}
	if err != nil {
		return err
	}
	if run.Status != "failed" || run.CurrentTaskID != completed.TaskID || !strings.Contains(run.Detail, "执行结束但仍在进行中") {
		return nil
	}
	run.Status, run.Detail = "running", "已恢复 Herdr 自动承接"
	if hasDoing && task.ColumnID == doing.ID && !task.Archived {
		run.Detail = "等待卡片 " + task.ID + " 离开进行中后继续自动承接"
		return d.store.SaveProjectRun(run)
	}
	if run.Cursor < len(run.TaskIDs) {
		nextID := run.TaskIDs[run.Cursor]
		nextExecution, err := d.store.Execution(nextID)
		if err != nil {
			return err
		}
		if nextExecution != nil && nextExecution.Active() && !nextExecution.Stalled() {
			run.CurrentTaskID = nextID
			run.Cursor++
			run.Detail = "已接续正在运行的 Herdr 会话"
		}
	} else {
		run.CurrentTaskID = ""
		run.Detail = "等待下一张卡片"
	}
	return d.store.SaveProjectRun(run)
}

func (d *Dispatcher) resumeHerdrTracking(run model.Execution) {
	ctx, cancel := context.WithCancel(context.Background())
	ex := &execution{cancel: cancel, done: make(chan struct{})}
	d.inflight[run.TaskID] = ex
	go func() {
		defer close(ex.done)
		defer func() {
			d.mu.Lock()
			delete(d.inflight, run.TaskID)
			d.mu.Unlock()
		}()
		d.trackHerdr(ctx, &run)
		d.finishProjectCard(run.TaskID)
	}()
}

// execution is one in-flight channel run.
type execution struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// Dispatch validates the request and starts an async run. It returns as
// soon as the process is launched; progress lands as task comments and
// column moves. Caller mistakes return *store.InvalidError; a task that
// already has a run in flight returns ErrInProgress.
func (d *Dispatcher) Dispatch(taskID, runtime, by string) error {
	if _, ok := runtimeDefinitionFor(runtime); !ok {
		return &store.InvalidError{Msg: fmt.Sprintf("unknown runtime %q", runtime)}
	}
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return &store.InvalidError{Msg: "dispatcher is closed"}
	}
	if d.recoveryErr != nil {
		d.mu.Unlock()
		return fmt.Errorf("读取执行记录失败：%w", d.recoveryErr)
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
	run, err := d.store.Execution(taskID)
	if err != nil {
		d.mu.Unlock()
		return err
	}
	if run != nil && run.Active() && !run.Stalled() && (runtime != RuntimeDSH || run.Channel() != RuntimeDSH) {
		d.mu.Unlock()
		return ErrInProgress
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
		d.run(ctx, task, project, dir, runtime, by)
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
func (d *Dispatcher) run(ctx context.Context, task model.Task, project model.Project, dir, runtime, by string) {
	d.runWithText(ctx, task, project.Name, dir, runtime, by, BuildTaskText(task), "")
}

// runWithText hands `text` to `runtime` as a fresh session. A non-empty
// followupID marks this as an @mention hand-off: the saved comment is the
// instruction, and its delivery status must be tracked like any other.
func (d *Dispatcher) runWithText(ctx context.Context, task model.Task, projectName, dir, runtime, by, text, followupID string) {
	definition, _ := runtimeDefinitionFor(runtime) // callers validated the channel
	actor := definition.Label
	if runtime == RuntimeDSH {
		// The desktop receipt is this channel's one dispatch comment: it carries
		// the session id, so a generic "派发 …" line written first would put two
		// near-identical messages on the card for every dispatch.
		d.runDesktop(ctx, task, dir, text, actor, by, followupID)
		return
	}
	if followupID != "" {
		if err := d.comment(task.ID, actor, fmt.Sprintf("已按评论 @%s 改由该渠道新建会话处理（工作目录 %s）…", actor, dir)); err != nil {
			return
		}
	} else if err := d.comment(task.ID, actor, fmt.Sprintf("派发 %s 执行（工作目录 %s；发起人 %s）…", runtime, dir, by)); err != nil {
		return // store is gone; nothing to write back to
	}
	if err := ctx.Err(); err != nil {
		return
	}
	if runtime != RuntimeDSH {
		defer d.finishProjectCard(task.ID)
		start := time.Now()
		run := &model.Execution{TaskID: task.ID, Runtime: runtime, Status: "submitting", FollowupID: followupID, Detail: "正在请求 Herdr 在项目 space 中创建 tab"}
		if err := d.store.SaveExecution(*run); err != nil {
			d.setFollowup(followupID, "failed")
			d.comment(task.ID, actor, "保存 Herdr 派发意图失败，尚未创建 tab："+err.Error())
			return
		}
		sessionID, err := herdrCreate(ctx, task, projectName, dir, runtime, d.boardURL, actor, text)
		if err != nil {
			var launchErr *herdrLaunchError
			if errors.As(err, &launchErr) && launchErr.uncertain {
				run.Status, run.Detail = "unknown", "Herdr 创建 tab 的回执不确定；禁止再次创建，请先在项目 space 核对："+err.Error()
				d.setFollowup(followupID, "unknown")
			} else {
				run.Status, run.Detail = "failed", err.Error()
				d.setFollowup(followupID, "failed")
			}
			_ = d.store.SaveExecution(*run)
			d.comment(task.ID, actor, fmt.Sprintf("Herdr 派发 %s 失败：%v。未回退到独立 CLI。", runtime, err))
			return
		}
		run.SessionID, run.Status, run.Detail = sessionID, "running", ""
		if err := d.store.SaveExecution(*run); err != nil {
			d.setFollowup(followupID, "unknown")
			d.comment(task.ID, actor, "Herdr 已创建 terminal，但执行记录保存失败；不会另建会话："+err.Error())
			return
		}
		d.setFollowup(followupID, "sent")
		d.comment(task.ID, actor, fmt.Sprintf("已交给 Herdr %s（项目 space 中的任务 tab 已打开；发起人 %s；%s）。", runtime, by, time.Since(start).Round(time.Second)))
		d.trackHerdr(ctx, run)
		return
	}

	start := time.Now()
	cmd, err := execCommand(ctx, dir, text, runtime, d.runtimeConfigPath)
	if err != nil {
		if followupID != "" {
			d.store.SetFollowupStatus(followupID, "failed")
		}
		d.comment(task.ID, actor, fmt.Sprintf("派发 %s 失败：%v。", runtime, err))
		return
	}
	run := &model.Execution{TaskID: task.ID, Runtime: runtime, Status: "running", FollowupID: followupID}
	if err := d.prepareCLI(cmd, run, false); err != nil {
		if followupID != "" {
			d.store.SetFollowupStatus(followupID, "failed")
		}
		d.comment(task.ID, actor, "保存执行会话失败，尚未提交任务："+err.Error())
		return
	}
	d.executeCLI(ctx, cmd, task, run, actor, start)
}

func (d *Dispatcher) executeCLI(ctx context.Context, cmd *exec.Cmd, task model.Task, run *model.Execution, actor string, start time.Time) {
	runtime := run.Channel()
	// execCommand already supplies the launcher's PATH. Add board routing and
	// attribution per process so isolated boards cannot write to the default one.
	cmd.Env = append(cmd.Environ(), "TASKBOARD_ADDR="+strings.TrimPrefix(d.boardURL, "http://"), "TB_AGENT="+actor)
	var stdout, stderr bytes.Buffer
	observer := &sessionObserver{d: d, run: run}
	cmd.Stdout = io.MultiWriter(&stdout, observer)
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	defer d.finishProjectCard(task.ID)
	if observer.err != nil {
		runErr = observer.err
	}
	status := "returned"
	if runErr != nil {
		status = "failed"
	}
	if ctx.Err() != nil {
		status = "disconnected"
	}
	run.Status = status
	if err := d.store.SaveExecution(*run); err != nil {
		d.comment(task.ID, actor, "保存会话状态失败："+err.Error())
	}
	if run.FollowupID != "" {
		// Even a failed process may have changed files. Once it started, never
		// replay this comment automatically; a fresh instruction is required.
		if cmd.Process != nil {
			d.store.SetFollowupStatus(run.FollowupID, "sent")
		} else {
			d.store.SetFollowupStatus(run.FollowupID, "failed")
		}
	}
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
		if strings.TrimSpace(stdout.String()) == "" {
			d.comment(task.ID, actor, fmt.Sprintf("%s 进程未返回结果文字（%s）；程序未修改任务状态，请以执行 Agent 的原卡回写为准。", runtime, elapsed))
			return
		}
		d.comment(task.ID, actor, fmt.Sprintf("%s 会话已结束（%s）；工作结果和卡片状态由执行 Agent 回写。", runtime, elapsed))
		return
	}
	if ctx.Err() != nil {
		d.comment(task.ID, actor, fmt.Sprintf("%s 执行被取消（服务关闭），已运行 %s；任务留在原列。", runtime, elapsed))
		return
	}
	detail := failureDiagnostic(stdout.String(), stderr.String())
	body := fmt.Sprintf("%s 执行失败（exit %d，%s）。", runtime, exit, elapsed)
	if detail != "" {
		body += "诊断：\n\n" + detail
	}
	d.comment(task.ID, actor, body)
}

func (d *Dispatcher) comment(taskID, by, text string) error {
	_, err := d.store.AddComment(taskID, text, by)
	return err
}

// setFollowup records a reserved comment hand-off's delivery outcome. An empty
// id means this run was not started by a comment, so there is nothing to mark.
func (d *Dispatcher) setFollowup(commentID, status string) {
	if commentID == "" {
		return
	}
	d.store.SetFollowupStatus(commentID, status)
}

// BuildTaskText hands off a card reference; the skill owns the workflow and
// reads current task content and project metadata from the board.
func BuildTaskText(task model.Task) string {
	return fmt.Sprintf("使用 manage-taskboard-native 执行任务 %s。", task.ID)
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
