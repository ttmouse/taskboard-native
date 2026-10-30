package executor

import (
	"fmt"
	"os"
	"strings"

	"taskboard-native/internal/model"
	"taskboard-native/internal/store"
)

// ErrProjectBusy reports that this project already has a running handoff.
type ErrProjectBusy struct{ Msg string }

func (e *ErrProjectBusy) Error() string { return e.Msg }

// DeleteProject removes a project only when no dispatch or project handoff can
// still write to its workspace. The projectRunMu and mu locks serialize this
// check with automatic dispatch and per-card dispatch; the store also checks
// persisted execution records under its transaction lock.
func (d *Dispatcher) DeleteProject(projectID, by string) error {
	d.projectRunMu.Lock()
	defer d.projectRunMu.Unlock()
	d.mu.Lock()
	defer d.mu.Unlock()

	projectRun, err := d.store.ProjectRun(projectID)
	if err == nil && projectRun.Running() {
		return &ErrProjectBusy{Msg: "项目自动承接仍在开启，请先关闭后再删除"}
	}
	if err != nil && err != store.ErrNotFound {
		return err
	}
	tasks, err := d.store.TasksByProject(projectID, true)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if _, ok := d.inflight[task.ID]; ok {
			return &ErrProjectBusy{Msg: "项目仍有任务在执行，请等原会话结束后再删除"}
		}
		run, err := d.store.Execution(task.ID)
		if err != nil {
			return err
		}
		if run != nil && run.Active() {
			return &ErrProjectBusy{Msg: "项目仍有任务的执行会话未结束，请先在原渠道处理后再删除"}
		}
	}
	return d.store.DeleteProject(projectID, by)
}

// StartProjectRun turns on continuous handoff for one project and remembers
// the channel. The switch stays on when the board has nothing ready.
func (d *Dispatcher) StartProjectRun(projectID, runtime, by string) (model.ProjectRun, error) {
	d.projectRunMu.Lock()
	defer d.projectRunMu.Unlock()
	if _, ok := runtimeDefinitionFor(runtime); !ok {
		return model.ProjectRun{}, &store.InvalidError{Msg: fmt.Sprintf("unknown runtime %q", runtime)}
	}
	project, err := d.store.Project(projectID)
	if err != nil {
		return model.ProjectRun{}, err
	}
	dir := strings.TrimSpace(project.WorkspacePath)
	if dir == "" {
		return model.ProjectRun{}, &store.InvalidError{Msg: fmt.Sprintf("project %q has no workspacePath; set one before dispatching", project.Name)}
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return model.ProjectRun{}, &store.InvalidError{Msg: fmt.Sprintf("workspacePath %q is not a directory on this machine", dir)}
	}
	if current, err := d.store.ProjectRun(projectID); err == nil && current.Running() {
		return model.ProjectRun{}, &ErrProjectBusy{Msg: "该项目已有一轮承接在进行"}
	} else if err != nil && err != store.ErrNotFound {
		return model.ProjectRun{}, err
	}
	ids, blocked, err := d.projectRunList(project)
	if err != nil {
		return model.ProjectRun{}, err
	}

	run := model.ProjectRun{ProjectID: projectID, Runtime: runtime, Status: "running", TaskIDs: ids}
	if blocked != "" {
		run.CurrentTaskID = blocked
		run.Detail = "等待卡片 " + blocked + " 的现有执行结束"
	}
	if err := d.store.SaveProjectRun(run); err != nil {
		return model.ProjectRun{}, err
	}
	if blocked != "" || len(ids) == 0 {
		return run, nil
	}
	if err := d.dispatchProjectCard(run, by); err != nil {
		run.Status = "failed"
		run.Detail = err.Error()
		_ = d.store.SaveProjectRun(run)
		return run, err
	}
	return d.store.ProjectRun(projectID)
}

// StopProjectRun turns the switch off. The current card keeps running.
func (d *Dispatcher) StopProjectRun(projectID string) (model.ProjectRun, error) {
	d.projectRunMu.Lock()
	defer d.projectRunMu.Unlock()
	run, err := d.store.ProjectRun(projectID)
	if err != nil {
		return model.ProjectRun{}, err
	}
	if run.Running() {
		run.Status = "stopped"
		run.Detail = "已关闭自动承接"
		if err := d.store.SaveProjectRun(run); err != nil {
			return model.ProjectRun{}, err
		}
	}
	return run, nil
}

// NoteProjectRun keeps a switched-on project moving. The current card must
// leave 进行中 before another starts. With nothing in flight, a newly ready
// card is picked up, including cards created after the switch was turned on.
func (d *Dispatcher) NoteProjectRun(task model.Task) {
	d.projectRunMu.Lock()
	defer d.projectRunMu.Unlock()
	run, err := d.store.ProjectRun(task.ProjectID)
	if err != nil || !run.Running() {
		return
	}
	if run.CurrentTaskID != "" && run.CurrentTaskID != task.ID {
		return
	}
	project, err := d.store.Project(task.ProjectID)
	if err != nil {
		return
	}
	doing, ok := project.ColumnByRole(model.ColumnRoleDoing)
	if !ok {
		d.failProjectRun(run, "项目没有进行中列")
		return
	}
	if run.CurrentTaskID == task.ID {
		if task.Archived {
			d.failProjectRun(run, "卡片 "+task.ID+" 已归档，自动承接已关闭")
			return
		}
		if task.ColumnID == doing.ID {
			return
		}
		run.CurrentTaskID = ""
		if err := d.store.SaveProjectRun(run); err != nil {
			return
		}
	}
	if err := d.dispatchNextReady(project, "自动承接"); err != nil {
		current, readErr := d.store.ProjectRun(project.ID)
		if readErr == nil {
			d.failProjectRun(current, err.Error())
		}
	}
}

func (d *Dispatcher) projectRunList(project model.Project) ([]string, string, error) {
	doing, hasDoing := project.ColumnByRole(model.ColumnRoleDoing)
	intake, hasIntake := project.ColumnByRole(model.ColumnRoleIntake)
	tasks, err := d.store.TasksByProject(project.ID, false)
	if err != nil {
		return nil, "", err
	}
	var stalled, ready []string
	for _, task := range tasks {
		if hasDoing && task.ColumnID == doing.ID {
			execution, err := d.store.Execution(task.ID)
			if err != nil {
				return nil, "", err
			}
			if execution != nil && execution.Active() && !execution.Stalled() {
				return nil, task.ID, nil
			}
			stalled = append(stalled, task.ID)
			continue
		}
		if hasIntake && task.ColumnID == intake.ID {
			ready = append(ready, task.ID)
		}
	}
	return append(stalled, ready...), "", nil
}

func (d *Dispatcher) dispatchProjectCard(run model.ProjectRun, by string) error {
	if run.Cursor >= len(run.TaskIDs) {
		run.CurrentTaskID = ""
		run.Detail = "等待下一张卡片"
		return d.store.SaveProjectRun(run)
	}
	id := run.TaskIDs[run.Cursor]
	run.CurrentTaskID = id
	run.Cursor++
	if err := d.store.SaveProjectRun(run); err != nil {
		return err
	}
	return d.Dispatch(id, run.Runtime, by)
}

func (d *Dispatcher) dispatchNextReady(project model.Project, by string) error {
	run, err := d.store.ProjectRun(project.ID)
	if err != nil || !run.Running() || run.CurrentTaskID != "" {
		return err
	}
	ids, blocked, err := d.projectRunList(project)
	if err != nil {
		return err
	}
	if blocked != "" {
		run.CurrentTaskID = blocked
		run.Detail = "等待卡片 " + blocked + " 的现有执行结束"
		return d.store.SaveProjectRun(run)
	}
	run.TaskIDs = ids
	run.Cursor = 0
	run.Detail = ""
	if len(ids) == 0 {
		run.Detail = "等待下一张卡片"
		return d.store.SaveProjectRun(run)
	}
	if err := d.store.SaveProjectRun(run); err != nil {
		return err
	}
	return d.dispatchProjectCard(run, by)
}

// finishProjectCard runs after a project-run dispatch returns. If the Agent
// turn returned normally but left the card in 进行中, keep the switch enabled
// and wait for a human to move the card; never start another session for it.
func (d *Dispatcher) finishProjectCard(taskID string) {
	d.projectRunMu.Lock()
	defer d.projectRunMu.Unlock()
	// Closing the service is not a failed project run. Keep the durable switch
	// enabled so the next dispatcher can resume tracking the existing session
	// and continue the project after the app restarts.
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	task, err := d.store.Task(taskID)
	if err != nil {
		return
	}
	run, err := d.store.ProjectRun(task.ProjectID)
	if err != nil || !run.Running() || run.CurrentTaskID != task.ID {
		return
	}
	project, err := d.store.Project(task.ProjectID)
	if err != nil {
		return
	}
	doing, ok := project.ColumnByRole(model.ColumnRoleDoing)
	if ok && task.ColumnID == doing.ID {
		execution, err := d.store.Execution(task.ID)
		if err == nil && execution != nil && execution.Status == "returned" {
			run.Detail = "等待卡片 " + task.ID + " 离开进行中后继续自动承接"
			_ = d.store.SaveProjectRun(run)
			return
		}
		d.failProjectRun(run, "卡片 "+task.ID+" 执行失败且仍在进行中，承接停止")
	}
}

func (d *Dispatcher) failProjectRun(run model.ProjectRun, detail string) {
	run.Status = "failed"
	run.Detail = detail
	_ = d.store.SaveProjectRun(run)
}
