package executor

import "taskboard-native/internal/model"

// syncRunningTaskColumn reflects an accepted comment follow-up in the board's
// workflow. New single-task dispatches remain owned by the execution Agent.
func (d *Dispatcher) syncRunningTaskColumn(taskID, by string) {
	task, err := d.store.Task(taskID)
	if err != nil || task.Archived {
		return
	}
	project, err := d.store.Project(task.ProjectID)
	if err != nil {
		return
	}
	doing, ok := project.ColumnByRole(model.ColumnRoleDoing)
	if !ok || task.ColumnID == doing.ID {
		return
	}
	if _, err := d.store.MoveTask(task.ID, task.Version, doing.ID, "", by); err != nil {
		latest, readErr := d.store.Task(taskID)
		if readErr == nil && !latest.Archived && latest.ColumnID == doing.ID {
			return
		}
		d.comment(taskID, by, "跟进已开始，但卡片未能自动移到「进行中」；请检查当前卡片状态："+err.Error())
	}
}
