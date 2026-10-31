package executor

import (
	"errors"
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
	d.logHandoff(handoffEvent{Event: "run.start_requested", ProjectID: projectID, Runtime: runtime, Actor: by})
	if _, ok := runtimeDefinitionFor(runtime); !ok {
		d.logHandoff(handoffEvent{Event: "run.start_rejected", ProjectID: projectID, Runtime: runtime, Actor: by, Reason: "unknown_runtime"})
		return model.ProjectRun{}, &store.InvalidError{Msg: fmt.Sprintf("unknown runtime %q", runtime)}
	}
	project, err := d.store.Project(projectID)
	if err != nil {
		d.logHandoff(handoffEvent{Event: "run.start_rejected", ProjectID: projectID, Runtime: runtime, Actor: by, Reason: "project_unavailable", ErrorType: errorType(err)})
		return model.ProjectRun{}, err
	}
	dir := strings.TrimSpace(project.WorkspacePath)
	if dir == "" {
		d.logHandoff(handoffEvent{Event: "run.start_rejected", ProjectID: projectID, Runtime: runtime, Actor: by, Reason: "workspace_missing"})
		return model.ProjectRun{}, &store.InvalidError{Msg: fmt.Sprintf("project %q has no workspacePath; set one before dispatching", project.Name)}
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		d.logHandoff(handoffEvent{Event: "run.start_rejected", ProjectID: projectID, Runtime: runtime, Actor: by, Reason: "workspace_unavailable", ErrorType: errorType(err)})
		return model.ProjectRun{}, &store.InvalidError{Msg: fmt.Sprintf("workspacePath %q is not a directory on this machine", dir)}
	}
	if current, err := d.store.ProjectRun(projectID); err == nil && current.Running() {
		d.logHandoff(handoffEvent{Event: "run.start_rejected", ProjectID: projectID, Runtime: runtime, Actor: by, RunStatus: current.Status, TaskID: current.CurrentTaskID, Reason: "already_running"})
		return model.ProjectRun{}, &ErrProjectBusy{Msg: "该项目已有一轮承接在进行"}
	} else if err != nil && err != store.ErrNotFound {
		d.logHandoff(handoffEvent{Event: "run.start_rejected", ProjectID: projectID, Runtime: runtime, Actor: by, Reason: "run_state_unavailable", ErrorType: errorType(err)})
		return model.ProjectRun{}, err
	}
	stalledDoing, ready, blocked, err := d.projectRunList(project)
	if err != nil {
		d.logHandoff(handoffEvent{Event: "run.queue_scan_failed", ProjectID: projectID, Runtime: runtime, Actor: by, ErrorType: errorType(err)})
		return model.ProjectRun{}, err
	}
	d.logHandoff(handoffEvent{Event: "run.queue_scanned", ProjectID: projectID, Runtime: runtime, Actor: by, TaskIDs: append(append([]string{}, stalledDoing...), ready...), TaskID: blocked, Reason: fmt.Sprintf("stalled_doing=%d ready=%d blocked=%t", len(stalledDoing), len(ready), blocked != "")})

	run := model.ProjectRun{ProjectID: projectID, Runtime: runtime, Status: "running"}
	if blocked != "" {
		run.CurrentTaskID = blocked
		run.Detail = "等待卡片 " + blocked + " 的现有执行结束"
		d.logHandoff(handoffEvent{Event: "run.waiting", ProjectID: project.ID, Runtime: run.Runtime, Actor: by, RunStatus: run.Status, TaskID: blocked, Reason: "existing_execution_active"})
	}
	if err := d.store.SaveProjectRun(run); err != nil {
		d.logHandoff(handoffEvent{Event: "run.start_failed", ProjectID: projectID, Runtime: runtime, Actor: by, ErrorType: errorType(err), Reason: "persist_initial_state"})
		return model.ProjectRun{}, err
	}
	d.logHandoff(handoffEvent{Event: "run.started", ProjectID: projectID, Runtime: runtime, Actor: by, RunStatus: run.Status, TaskID: run.CurrentTaskID, TaskIDs: append([]string(nil), run.TaskIDs...), Cursor: run.Cursor, Reason: run.Detail})
	if blocked != "" {
		if _, _, err := d.claimProjectCard(blocked, by); err != nil {
			run.Status, run.Detail = "failed", err.Error()
			_ = d.store.SaveProjectRun(run)
			return run, err
		}
		event := handoffEvent{Event: "task.claimed_existing_execution", ProjectID: projectID, Runtime: runtime, Actor: by, RunStatus: run.Status, TaskID: blocked, Reason: "do_not_redispatch_active_session"}
		if execution, readErr := d.store.Execution(blocked); readErr == nil && execution != nil {
			event.SessionID, event.ExecutionState = execution.SessionID, execution.Status
		} else if readErr != nil {
			event.ErrorType = errorType(readErr)
		}
		d.logHandoff(event)
		return d.store.ProjectRun(projectID)
	}
	// Finished or broken 进行中 cards are pushed to 等你, never re-run; only
	// work that never started is dispatched fresh.
	ready = append(d.pushStalledDoingCards(project, stalledDoing, by), ready...)
	run.TaskIDs = ready
	if len(ready) == 0 {
		run.Detail = "等待下一张卡片"
		_ = d.store.SaveProjectRun(run)
		d.logHandoff(handoffEvent{Event: "run.waiting", ProjectID: projectID, Runtime: runtime, Actor: by, RunStatus: run.Status, Reason: "no_ready_card"})
		return run, nil
	}
	if err := d.store.SaveProjectRun(run); err != nil {
		return model.ProjectRun{}, err
	}
	if err := d.dispatchProjectCard(run, by); err != nil {
		run.Status = "failed"
		run.Detail = err.Error()
		if saveErr := d.store.SaveProjectRun(run); saveErr != nil {
			d.logHandoff(handoffEvent{Event: "run.failure_persist_failed", ProjectID: projectID, Runtime: runtime, RunStatus: run.Status, ErrorType: errorType(saveErr)})
		}
		d.logHandoff(handoffEvent{Event: "run.failed", ProjectID: projectID, Runtime: runtime, Actor: by, RunStatus: run.Status, TaskID: run.CurrentTaskID, TaskIDs: append([]string(nil), run.TaskIDs...), Cursor: run.Cursor, ErrorType: errorType(err), Reason: "initial_dispatch_failed"})
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
			d.logHandoff(handoffEvent{Event: "run.stop_failed", ProjectID: projectID, Runtime: run.Runtime, RunStatus: run.Status, ErrorType: errorType(err)})
			return model.ProjectRun{}, err
		}
		d.logHandoff(handoffEvent{Event: "run.stopped", ProjectID: projectID, Runtime: run.Runtime, RunStatus: run.Status, TaskID: run.CurrentTaskID, Reason: "user_stopped"})
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
			// Archiving is a deliberate human decision: the card left the
			// queue, so keep the switch on and continue with the next card.
			run.CurrentTaskID = ""
			run.Detail = "卡片 " + task.ID + " 已归档，继续自动承接"
			d.logHandoff(handoffEvent{Event: "task.left_queue", ProjectID: run.ProjectID, Runtime: run.Runtime, RunStatus: run.Status, TaskID: task.ID, ColumnID: task.ColumnID, Reason: "archived"})
			if err := d.store.SaveProjectRun(run); err != nil {
				return
			}
			d.continueProjectRun(project, "自动承接")
			return
		}
		if task.ColumnID == doing.ID {
			return
		}
		d.logHandoff(handoffEvent{Event: "task.left_queue", ProjectID: run.ProjectID, Runtime: run.Runtime, RunStatus: run.Status, TaskID: task.ID, ColumnID: task.ColumnID, Reason: "moved_by_user_or_agent"})
		run.CurrentTaskID = ""
		if err := d.store.SaveProjectRun(run); err != nil {
			return
		}
	}
	d.continueProjectRun(project, "自动承接")
}

// continueProjectRun dispatches the next ready card after the current one
// left the queue. A dispatch error fails the run with the reason.
func (d *Dispatcher) continueProjectRun(project model.Project, by string) {
	if err := d.dispatchNextReady(project, by); err != nil {
		if current, readErr := d.store.ProjectRun(project.ID); readErr == nil {
			d.failProjectRun(current, err.Error())
			d.logHandoff(handoffEvent{Event: "run.continuation_failed", ProjectID: project.ID, Runtime: current.Runtime, Actor: by, RunStatus: "failed", TaskID: current.CurrentTaskID, ErrorType: errorType(err)})
		}
	}
}

// projectRunList splits the switch's work queue: stalledDoing holds 进行中
// cards whose execution record is already finished or unrecoverable (the
// caller pushes them to 等你 instead of re-running them), ready holds cards
// that were never started or wait in 待办 (dispatched fresh, doing before
// intake), and blocked reports a card whose session is still alive.
func (d *Dispatcher) projectRunList(project model.Project) (stalledDoing, ready []string, blocked string, err error) {
	doing, hasDoing := project.ColumnByRole(model.ColumnRoleDoing)
	intake, hasIntake := project.ColumnByRole(model.ColumnRoleIntake)
	tasks, err := d.store.TasksByProject(project.ID, false)
	if err != nil {
		return nil, nil, "", err
	}
	for _, task := range tasks {
		if hasDoing && task.ColumnID == doing.ID {
			execution, err := d.store.Execution(task.ID)
			if err != nil {
				return nil, nil, "", err
			}
			if execution == nil {
				// A claim without a record: the work never started, so a
				// fresh dispatch is the push this card needs.
				ready = append(ready, task.ID)
				continue
			}
			if execution.Active() && !execution.Stalled() {
				return nil, nil, task.ID, nil
			}
			stalledDoing = append(stalledDoing, task.ID)
			continue
		}
		if hasIntake && task.ColumnID == intake.ID {
			execution, err := d.store.Execution(task.ID)
			if err != nil {
				return nil, nil, "", err
			}
			if execution != nil && execution.Active() && !execution.Stalled() {
				return nil, nil, task.ID, nil
			}
			ready = append(ready, task.ID)
		}
	}
	return stalledDoing, ready, "", nil
}

// pushStalledDoingCards moves finished-or-broken 进行中 cards to the review
// column. Cards that cannot be pushed (no review column, lost a version race)
// fall back to a plain re-dispatch so the queue still moves.
func (d *Dispatcher) pushStalledDoingCards(project model.Project, ids []string, by string) []string {
	var fallback []string
	for _, id := range ids {
		task, err := d.store.Task(id)
		if err != nil {
			continue
		}
		execution, err := d.store.Execution(id)
		if err != nil || execution == nil {
			continue
		}
		if d.advanceFinishedCard(project, task, *execution, by) {
			continue
		}
		fallback = append(fallback, id)
	}
	return fallback
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
	claimed, version, err := d.claimProjectCard(id, by)
	if err != nil {
		d.logHandoff(handoffEvent{Event: "task.claim_failed", ProjectID: run.ProjectID, Runtime: run.Runtime, Actor: by, RunStatus: run.Status, TaskID: id, ErrorType: errorType(err)})
		return err
	}
	d.logHandoff(handoffEvent{Event: "task.claimed", ProjectID: run.ProjectID, Runtime: run.Runtime, Actor: by, RunStatus: run.Status, TaskID: id, Cursor: run.Cursor, Reason: map[bool]string{true: "moved_to_doing", false: "already_doing"}[claimed]})
	d.logHandoff(handoffEvent{Event: "task.dispatch_started", ProjectID: run.ProjectID, Runtime: run.Runtime, Actor: by, RunStatus: run.Status, TaskID: id, Cursor: run.Cursor})
	if err := d.Dispatch(id, run.Runtime, by); err != nil {
		d.logHandoff(handoffEvent{Event: "task.dispatch_failed", ProjectID: run.ProjectID, Runtime: run.Runtime, Actor: by, RunStatus: run.Status, TaskID: id, ErrorType: errorType(err), Reason: map[bool]string{true: "existing_execution", false: "dispatch_error"}[errors.Is(err, ErrInProgress)]})
		if errors.Is(err, ErrInProgress) {
			latest, readErr := d.store.ProjectRun(run.ProjectID)
			if readErr != nil {
				return readErr
			}
			latest.Detail = "等待卡片 " + id + " 的现有执行结束"
			return d.store.SaveProjectRun(latest)
		}
		if latest, readErr := d.store.ProjectRun(run.ProjectID); readErr == nil {
			latest.Status, latest.Detail = "failed", err.Error()
			_ = d.store.SaveProjectRun(latest)
		}
		if claimed {
			d.releaseProjectCard(id, version, by)
		}
		return err
	}
	event := handoffEvent{Event: "task.dispatch_accepted", ProjectID: run.ProjectID, Runtime: run.Runtime, Actor: by, RunStatus: run.Status, TaskID: id}
	if execution, readErr := d.store.Execution(id); readErr == nil && execution != nil {
		event.SessionID, event.ExecutionState = execution.SessionID, execution.Status
	}
	d.logHandoff(event)
	return nil
}

// claimProjectCard moves an intake card into doing before automatic dispatch.
// Existing doing cards already represent a claim and are left in place.
func (d *Dispatcher) claimProjectCard(taskID, by string) (bool, int, error) {
	task, err := d.store.Task(taskID)
	if err != nil {
		return false, 0, err
	}
	if task.Archived {
		return false, 0, fmt.Errorf("卡片 %s 已归档，自动承接未派发", taskID)
	}
	project, err := d.store.Project(task.ProjectID)
	if err != nil {
		return false, 0, err
	}
	doing, ok := project.ColumnByRole(model.ColumnRoleDoing)
	if !ok {
		return false, 0, errors.New("项目没有进行中列")
	}
	if task.ColumnID == doing.ID {
		return false, task.Version, nil
	}
	intake, ok := project.ColumnByRole(model.ColumnRoleIntake)
	if !ok || task.ColumnID != intake.ID {
		return false, 0, fmt.Errorf("卡片 %s 已离开待办列，自动承接未派发", taskID)
	}
	claimed, err := d.store.MoveTask(task.ID, task.Version, doing.ID, "", by)
	if err != nil {
		return false, 0, err
	}
	return true, claimed.Version, nil
}

// releaseProjectCard restores a claim only when the card is still at the
// version created by our move. A concurrent user edit or move wins.
func (d *Dispatcher) releaseProjectCard(taskID string, claimedVersion int, by string) {
	task, err := d.store.Task(taskID)
	if err != nil || task.Archived || task.Version != claimedVersion {
		return
	}
	project, err := d.store.Project(task.ProjectID)
	if err != nil {
		return
	}
	doing, hasDoing := project.ColumnByRole(model.ColumnRoleDoing)
	intake, hasIntake := project.ColumnByRole(model.ColumnRoleIntake)
	if hasDoing && hasIntake && task.ColumnID == doing.ID {
		_, _ = d.store.MoveTask(task.ID, task.Version, intake.ID, "", by)
	}
}

// recoverProjectRuns reconciles durable project handoffs after service startup.
// A current card still in intake is claimed without opening another session
// when an execution record already exists.
func (d *Dispatcher) recoverProjectRuns() error {
	projects, err := d.store.Projects()
	if err != nil {
		return err
	}
	for _, project := range projects {
		run, err := d.store.ProjectRun(project.ID)
		if err == store.ErrNotFound {
			continue
		}
		if err != nil {
			return err
		}
		if !run.Running() {
			continue
		}
		d.logHandoff(handoffEvent{Event: "run.recovery_observed", ProjectID: run.ProjectID, Runtime: run.Runtime, RunStatus: run.Status, TaskID: run.CurrentTaskID, TaskIDs: append([]string(nil), run.TaskIDs...), Cursor: run.Cursor, Reason: "service_startup"})
		if run.CurrentTaskID == "" {
			d.continueProjectRun(project, "自动承接恢复")
			continue
		}

		task, err := d.store.Task(run.CurrentTaskID)
		if err != nil {
			d.failProjectRun(run, "自动承接恢复失败：当前卡片不存在")
			continue
		}
		if task.Archived {
			// 归档即离队：继续派发下一张，开关保持开启。
			run.CurrentTaskID, run.Detail = "", "卡片 "+task.ID+" 已归档，继续自动承接"
			if err := d.store.SaveProjectRun(run); err != nil {
				return err
			}
			d.continueProjectRun(project, "自动承接恢复")
			continue
		}
		doing, hasDoing := project.ColumnByRole(model.ColumnRoleDoing)
		intake, hasIntake := project.ColumnByRole(model.ColumnRoleIntake)
		if hasDoing && task.ColumnID == doing.ID || hasIntake && task.ColumnID == intake.ID {
			wasDoing := hasDoing && task.ColumnID == doing.ID
			execution, err := d.store.Execution(task.ID)
			if err != nil {
				return err
			}
			if execution != nil && execution.Active() && !execution.Stalled() {
				d.logHandoff(handoffEvent{Event: "run.recovery_session_active", ProjectID: run.ProjectID, Runtime: run.Runtime, RunStatus: run.Status, TaskID: task.ID, SessionID: execution.SessionID, ExecutionState: execution.Status, ColumnID: task.ColumnID})
				// The session is still alive: resume tracking it and claim an
				// intake card so the board shows where the work happens.
				if _, _, err := d.claimProjectCard(task.ID, "自动承接恢复"); err != nil {
					d.failProjectRun(run, err.Error())
				}
				continue
			}
			if execution != nil {
				d.logHandoff(handoffEvent{Event: "run.recovery_session_ended", ProjectID: run.ProjectID, Runtime: run.Runtime, RunStatus: run.Status, TaskID: task.ID, SessionID: execution.SessionID, ExecutionState: execution.Status, ColumnID: task.ColumnID})
				// The session is over (or a CLI process died with the last
				// service): the card must not block the queue. One that was
				// already worked moves to the review column; an intake card
				// is dispatched again like any other queued card.
				if wasDoing && d.advanceFinishedCard(project, task, *execution, "自动承接恢复") {
					run.CurrentTaskID, run.Detail = "", ""
					if err := d.store.SaveProjectRun(run); err != nil {
						return err
					}
					d.continueProjectRun(project, "自动承接恢复")
					continue
				}
				if wasDoing {
					run.Detail = "等待卡片 " + task.ID + " 离开进行中后继续自动承接"
					if err := d.store.SaveProjectRun(run); err != nil {
						return err
					}
					continue
				}
				run.CurrentTaskID, run.Detail = "", ""
				if err := d.store.SaveProjectRun(run); err != nil {
					return err
				}
				d.continueProjectRun(project, "自动承接恢复")
				continue
			}
			// No execution record: claim and start a fresh session.
			claimed, version, err := d.claimProjectCard(task.ID, "自动承接恢复")
			if err != nil {
				d.logHandoff(handoffEvent{Event: "task.claim_failed", ProjectID: run.ProjectID, Runtime: run.Runtime, Actor: "自动承接恢复", RunStatus: run.Status, TaskID: task.ID, ErrorType: errorType(err)})
				d.failProjectRun(run, err.Error())
				continue
			}
			d.logHandoff(handoffEvent{Event: "task.dispatch_started", ProjectID: run.ProjectID, Runtime: run.Runtime, Actor: "自动承接恢复", RunStatus: run.Status, TaskID: task.ID, Cursor: run.Cursor})
			if err := d.Dispatch(task.ID, run.Runtime, "自动承接恢复"); err != nil {
				d.logHandoff(handoffEvent{Event: "task.dispatch_failed", ProjectID: run.ProjectID, Runtime: run.Runtime, Actor: "自动承接恢复", RunStatus: run.Status, TaskID: task.ID, ErrorType: errorType(err), Reason: "recovery_dispatch_error"})
				if claimed {
					d.releaseProjectCard(task.ID, version, "自动承接恢复")
				}
				d.failProjectRun(run, err.Error())
			} else {
				d.logHandoff(handoffEvent{Event: "task.dispatch_accepted", ProjectID: run.ProjectID, Runtime: run.Runtime, Actor: "自动承接恢复", RunStatus: run.Status, TaskID: task.ID})
			}
			continue
		}

		// The current card already left 进行中 before the service stopped.
		run.CurrentTaskID, run.Detail = "", ""
		if err := d.store.SaveProjectRun(run); err != nil {
			return err
		}
		d.continueProjectRun(project, "自动承接恢复")
	}
	return nil
}

func (d *Dispatcher) dispatchNextReady(project model.Project, by string) error {
	run, err := d.store.ProjectRun(project.ID)
	if err != nil || !run.Running() || run.CurrentTaskID != "" {
		return err
	}
	stalledDoing, ready, blocked, err := d.projectRunList(project)
	if err != nil {
		return err
	}
	d.logHandoff(handoffEvent{Event: "run.queue_scanned", ProjectID: project.ID, Runtime: run.Runtime, Actor: by, TaskIDs: append(append([]string{}, stalledDoing...), ready...), TaskID: blocked, Reason: fmt.Sprintf("stalled_doing=%d ready=%d blocked=%t", len(stalledDoing), len(ready), blocked != "")})
	if blocked != "" {
		run.CurrentTaskID = blocked
		run.Detail = "等待卡片 " + blocked + " 的现有执行结束"
		if err := d.store.SaveProjectRun(run); err != nil {
			return err
		}
		claimed, _, err := d.claimProjectCard(blocked, by)
		if err != nil {
			d.logHandoff(handoffEvent{Event: "task.claim_failed", ProjectID: project.ID, Runtime: run.Runtime, Actor: by, RunStatus: run.Status, TaskID: blocked, ErrorType: errorType(err)})
			return err
		}
		event := handoffEvent{Event: "task.claimed_existing_execution", ProjectID: project.ID, Runtime: run.Runtime, Actor: by, RunStatus: run.Status, TaskID: blocked, Reason: "do_not_redispatch_active_session"}
		if execution, readErr := d.store.Execution(blocked); readErr == nil && execution != nil {
			event.SessionID, event.ExecutionState = execution.SessionID, execution.Status
		} else if readErr != nil {
			event.ErrorType = errorType(readErr)
		}
		if claimed {
			event.Reason = "claimed_without_redispatch_active_session"
		}
		d.logHandoff(event)
		return err
	}
	ready = append(d.pushStalledDoingCards(project, stalledDoing, by), ready...)
	run.TaskIDs = ready
	run.Cursor = 0
	run.Detail = ""
	if len(ready) == 0 {
		run.Detail = "等待下一张卡片"
		d.logHandoff(handoffEvent{Event: "run.waiting", ProjectID: project.ID, Runtime: run.Runtime, Actor: by, RunStatus: run.Status, Reason: "no_ready_card"})
		return d.store.SaveProjectRun(run)
	}
	if err := d.store.SaveProjectRun(run); err != nil {
		return err
	}
	return d.dispatchProjectCard(run, by)
}

// finishProjectCard runs after a project-run dispatch returns. The switch
// stays enabled: a converged session (returned, failed, released) has its
// card pushed to 等你 by the service and the next card starts right away.
// Completion stays with the human — the card is never moved to 完成.
func (d *Dispatcher) finishProjectCard(taskID string) {
	d.projectRunMu.Lock()
	defer d.projectRunMu.Unlock()
	// Closing the service is not a failed project run. Keep the durable switch
	// enabled so the next dispatcher can resume tracking the existing session
	// and continue the project after the app restarts.
	d.mu.Lock()
	closed := d.closed
	d.mu.Unlock()
	if closed {
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
	if !ok || task.ColumnID != doing.ID {
		return
	}
	execution, err := d.store.Execution(task.ID)
	if err != nil || execution == nil || !execution.Ended() {
		d.logHandoff(handoffEvent{Event: "task.execution_not_converged", ProjectID: task.ProjectID, Runtime: run.Runtime, RunStatus: run.Status, TaskID: task.ID, ColumnID: task.ColumnID, ErrorType: errorType(err), Reason: "execution_missing_or_active"})
		d.failProjectRun(run, "卡片 "+task.ID+" 执行失败且仍在进行中，承接停止")
		return
	}
	d.logHandoff(handoffEvent{Event: "task.execution_converged", ProjectID: task.ProjectID, Runtime: run.Runtime, RunStatus: run.Status, TaskID: task.ID, SessionID: execution.SessionID, ExecutionState: execution.Status, ColumnID: task.ColumnID})
	if d.advanceFinishedCard(project, task, *execution, "自动承接") {
		run.CurrentTaskID, run.Detail = "", ""
		if err := d.store.SaveProjectRun(run); err != nil {
			return
		}
		d.continueProjectRun(project, "自动承接")
		return
	}
	// No review column, or the move lost a race with a human edit: wait for a
	// human instead of silently blocking the queue.
	run.Detail = "等待卡片 " + task.ID + " 离开进行中后继续自动承接"
	_ = d.store.SaveProjectRun(run)
}

// parkProjectCardForApproval moves a Herdr task that needs a human decision
// out of the active queue, then lets the project run inspect the next card.
// Its execution record remains active so no second writer can be dispatched
// for this task while approval is pending.
func (d *Dispatcher) parkProjectCardForApproval(taskID string) {
	d.projectRunMu.Lock()
	defer d.projectRunMu.Unlock()
	task, err := d.store.Task(taskID)
	if err != nil || task.Archived {
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
	if doing, ok := project.ColumnByRole(model.ColumnRoleDoing); ok && task.ColumnID == doing.ID {
		review, hasReview := project.ColumnByRole(model.ColumnRoleReview)
		if !hasReview {
			run.Detail = "卡片 " + task.ID + " 等待人工审批，但项目没有用户等待列"
			d.logHandoff(handoffEvent{Event: "task.approval_waiting", ProjectID: project.ID, Runtime: run.Runtime, RunStatus: run.Status, TaskID: task.ID, Reason: "review_column_missing"})
			_ = d.store.SaveProjectRun(run)
			return
		}
		if _, err := d.store.MoveTask(task.ID, task.Version, review.ID, "", "自动承接"); err != nil {
			run.Detail = "卡片 " + task.ID + " 等待人工审批，移入「等你」失败：" + err.Error()
			d.logHandoff(handoffEvent{Event: "task.approval_move_failed", ProjectID: project.ID, Runtime: run.Runtime, RunStatus: run.Status, TaskID: task.ID, ColumnID: task.ColumnID, ErrorType: errorType(err)})
			_ = d.store.SaveProjectRun(run)
			return
		}
		_ = d.comment(task.ID, "自动承接", "自动承接：当前会话等待人工审批，卡片移入「等你」；请完成审批或在评论中继续跟进。自动承接将继续检查后续卡片。")
		d.logHandoff(handoffEvent{Event: "task.approval_parked", ProjectID: project.ID, Runtime: run.Runtime, RunStatus: run.Status, TaskID: task.ID, ColumnID: review.ID, Reason: "agent_requested_human_approval"})
	}
	run.CurrentTaskID, run.Detail = "", ""
	if err := d.store.SaveProjectRun(run); err != nil {
		return
	}
	d.continueProjectRun(project, "自动承接")
}

// advanceFinishedCard pushes a finished card out of 进行中 into the project's
// review column and writes a short service comment explaining the move. The
// card is never moved to the done column: finishing work is still confirmed
// by a human. It reports whether the card actually moved.
func (d *Dispatcher) advanceFinishedCard(project model.Project, task model.Task, execution model.Execution, by string) bool {
	if task.Archived {
		return false
	}
	review, ok := project.ColumnByRole(model.ColumnRoleReview)
	if !ok || task.ColumnID == review.ID {
		return false
	}
	if _, err := d.store.MoveTask(task.ID, task.Version, review.ID, "", by); err != nil {
		d.logHandoff(handoffEvent{Event: "task.review_move_failed", ProjectID: task.ProjectID, Runtime: execution.Channel(), Actor: by, TaskID: task.ID, ExecutionState: execution.Status, ColumnID: task.ColumnID, ErrorType: errorType(err)})
		return false
	}
	d.logHandoff(handoffEvent{Event: "task.moved_to_review", ProjectID: task.ProjectID, Runtime: execution.Channel(), Actor: by, TaskID: task.ID, ExecutionState: execution.Status, ColumnID: review.ID, Reason: "execution_ended_human_acceptance_required"})
	_ = d.comment(task.ID, by, finishedCardReason(execution))
	return true
}

func finishedCardReason(execution model.Execution) string {
	switch execution.Status {
	case "returned", "succeeded":
		return "自动承接：会话已返回，卡片自动移入「等你」；是否完成由人工确认。"
	case "released":
		return "自动承接：原会话已不可达并释放写锁，卡片自动移入「等你」等待人工处理。"
	case "disconnected":
		return "自动承接：原会话已中断且无法恢复，卡片自动移入「等你」等待人工处理。"
	default:
		return "自动承接：执行失败（" + execution.Status + "），卡片自动移入「等你」等待人工处理。"
	}
}

func (d *Dispatcher) failProjectRun(run model.ProjectRun, detail string) {
	run.Status = "failed"
	run.Detail = detail
	if err := d.store.SaveProjectRun(run); err != nil {
		d.logHandoff(handoffEvent{Event: "run.failure_persist_failed", ProjectID: run.ProjectID, Runtime: run.Runtime, RunStatus: run.Status, TaskID: run.CurrentTaskID, ErrorType: errorType(err)})
		return
	}
	d.logHandoff(handoffEvent{Event: "run.failed", ProjectID: run.ProjectID, Runtime: run.Runtime, RunStatus: run.Status, TaskID: run.CurrentTaskID, TaskIDs: append([]string(nil), run.TaskIDs...), Cursor: run.Cursor, Reason: "internal_error"})
}
