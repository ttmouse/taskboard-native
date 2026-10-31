package executor

import "taskboard-native/internal/model"

// syncRunningTaskColumn reflects a channel-accepted session in the board's
// workflow. A card already in doing is left unchanged; otherwise, accepted
// work belongs in the active-work column while its session is running.
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
		d.comment(taskID, by, "执行已开始，但卡片未能自动移到「进行中」；请检查当前卡片状态："+err.Error())
	}
}
