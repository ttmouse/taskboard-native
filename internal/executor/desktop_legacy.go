package executor

import (
	"regexp"
	"strings"

	"taskboard-native/internal/model"
)

var legacyDesktopID = regexp.MustCompile(`^aa_[0-9a-f]{16}_taskboard-[0-9a-f]{32}$`)

// Upgrade only a receipt explicitly written by the previous desktop dispatcher.
// Never infer a desktop session from an old headless failure or from a task title.
func (d *Dispatcher) importDesktopReceipts() error {
	projects, err := d.store.Projects()
	if err != nil {
		return err
	}
	for _, project := range projects {
		// 进行中 by role, falling back to its legacy name for old boards.
		column, ok := project.ColumnByRole(model.ColumnRoleDoing)
		if !ok {
			continue
		}
		doing := column.ID
		tasks, err := d.store.TasksByProject(project.ID, false)
		if err != nil {
			return err
		}
		for _, task := range tasks {
			if task.ColumnID != doing {
				continue
			}
			run, err := d.store.Execution(task.ID)
			if err != nil {
				return err
			}
			if run != nil {
				continue
			}
			comments, err := d.store.Comments(task.ID)
			if err != nil {
				return err
			}
			id := ""
			for _, c := range comments {
				if c.Author != "DSH" {
					continue
				}
				if strings.HasPrefix(c.Text, "派发 ") || strings.HasPrefix(c.Text, "DSH 桌面端执行完成（") || model.CompletingAgent(c) == "DSH" {
					id = ""
				}
				if strings.HasPrefix(c.Text, "已交给 DSH 桌面端。") {
					_, candidate, ok := strings.Cut(c.Text, "会话 ID：")
					if ok && legacyDesktopID.MatchString(strings.TrimSpace(candidate)) {
						id = strings.TrimSpace(candidate)
					}
				}
			}
			if id != "" {
				if err := d.store.SaveExecution(model.Execution{TaskID: task.ID, SessionID: id, Status: "detached", Detail: "从旧版桌面回执恢复"}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func desktopSessionParams(id string) map[string]any {
	if legacyDesktopID.MatchString(id) {
		return map[string]any{"externalSessionId": id}
	}
	return map[string]any{"sessionId": id}
}
