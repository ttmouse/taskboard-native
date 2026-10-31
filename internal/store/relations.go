package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/egoist/mygo/plugins/sqlite"

	"taskboard-native/internal/model"
)

// Relation types accepted by the API. blocked_by is stored as blocks with
// reversed endpoints (DSH semantics).
const (
	RelationParent    = "parent"
	RelationBlocks    = "blocks"
	RelationBlockedBy = "blocked_by"
	RelationRelated   = "related"
)

// AddTaskRelation links relatedID to id under type, bumping id's version.
// type is parent | blocks | blocked_by | related.
func (s *Store) AddTaskRelation(id string, version int, relType, relatedID, by string) (model.RelationResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	task, err := s.taskLocked(ctx, id)
	if err != nil {
		return model.RelationResult{}, err
	}
	related, err := s.taskLocked(ctx, relatedID)
	if err != nil {
		return model.RelationResult{}, err
	}
	if version != task.Version {
		return model.RelationResult{}, &VersionError{Current: task}
	}
	if task.Archived {
		return model.RelationResult{}, invalid("task %s is archived", task.ID)
	}
	if err := validateRelationTasks(task, related); err != nil {
		return model.RelationResult{}, err
	}
	storedType, sourceID, targetID, err := relationEndpoints(relType, task.ID, related.ID)
	if err != nil {
		return model.RelationResult{}, err
	}
	if storedType == RelationBlocks && targetID == task.ID {
		p, err := s.projectLocked(ctx, task.ProjectID)
		if err != nil {
			return model.RelationResult{}, err
		}
		activeColumn := false
		for _, column := range p.Columns {
			if column.ID == task.ColumnID && (column.Role == model.ColumnRoleDoing || column.Role == model.ColumnRoleReview) {
				activeColumn = true
				break
			}
		}
		if activeColumn {
			blocker := model.TaskRelationSummary{ID: related.ID, Number: related.Number, Title: related.Title, ColumnID: related.ColumnID, Archived: related.Archived}
			if err := s.assertUnblockedWithExtraLocked(ctx, task, p, &blocker); err != nil {
				return model.RelationResult{}, err
			}
		}
	}
	stmts := []sqlite.Statement{}
	var previousParent *model.Task
	if storedType == RelationParent {
		if err := s.assertNoParentCycleLocked(ctx, task.ID, related.ID); err != nil {
			return model.RelationResult{}, err
		}
		existing, err := s.db.Query(ctx,
			`SELECT source_task_id FROM task_relations WHERE relation_type = 'parent' AND target_task_id = ?`,
			task.ID)
		if err != nil {
			return model.RelationResult{}, err
		}
		if len(existing.Values) == 1 {
			curParent := str(existing.Values[0][0])
			if curParent == related.ID {
				return model.RelationResult{}, invalid("父子关系已存在")
			}
			parent, err := s.taskLocked(ctx, curParent)
			if err != nil {
				return model.RelationResult{}, err
			}
			previousParent = &parent
			stmts = append(stmts, sqlite.Statement{
				SQL:  `DELETE FROM task_relations WHERE relation_type = 'parent' AND target_task_id = ?`,
				Args: []any{task.ID},
			})
		}
	} else {
		if storedType == RelationBlocks {
			if err := s.assertNoBlockingCycleLocked(ctx, sourceID, targetID); err != nil {
				return model.RelationResult{}, err
			}
		}
		existing, err := s.db.Query(ctx,
			`SELECT 1 FROM task_relations WHERE relation_type = ? AND source_task_id = ? AND target_task_id = ?`,
			storedType, sourceID, targetID)
		if err != nil {
			return model.RelationResult{}, err
		}
		if len(existing.Values) > 0 {
			return model.RelationResult{}, invalid("该关系已存在")
		}
	}
	now := nowUTC()
	stmts = append(stmts, sqlite.Statement{
		SQL:  `INSERT INTO task_relations (relation_type, source_task_id, target_task_id, created_at) VALUES (?, ?, ?, ?)`,
		Args: []any{storedType, sourceID, targetID, timestamp(now)},
	})
	task.Version = version + 1
	task.UpdatedAt = now
	task.UpdatedBy = by
	stmts = append(stmts, s.updateTaskStatement(task, by))
	if _, err := s.db.Transaction(ctx, stmts); err != nil {
		return model.RelationResult{}, err
	}
	if err := s.enrichTaskLocked(ctx, &task); err != nil {
		return model.RelationResult{}, err
	}
	if err := s.enrichTaskLocked(ctx, &related); err != nil {
		return model.RelationResult{}, err
	}
	emit(s, model.Event{Type: model.EventTaskUpdated, ProjectID: task.ProjectID, ID: task.ID, Version: task.Version, By: by, Task: &task})
	emit(s, model.Event{Type: model.EventTaskUpdated, ProjectID: related.ProjectID, ID: related.ID, Version: related.Version, By: by, Task: &related})
	if previousParent != nil {
		if err := s.enrichTaskLocked(ctx, previousParent); err != nil {
			return model.RelationResult{}, err
		}
		emit(s, model.Event{Type: model.EventTaskUpdated, ProjectID: previousParent.ProjectID, ID: previousParent.ID, Version: previousParent.Version, By: by, Task: previousParent})
	}
	return model.RelationResult{Task: task, RelatedTask: related}, nil
}

func (s *Store) assertNoBlockingCycleLocked(ctx context.Context, sourceID, targetID string) error {
	// source blocks target. A path from target back to source would make the
	// dependency graph impossible to satisfy.
	rows, err := s.db.Query(ctx, `
WITH RECURSIVE descendants(id) AS (
  SELECT target_task_id FROM task_relations WHERE relation_type='blocks' AND source_task_id=?
  UNION
  SELECT r.target_task_id FROM task_relations r JOIN descendants d ON r.source_task_id=d.id
  WHERE r.relation_type='blocks'
)
SELECT 1 FROM descendants WHERE id=?`, targetID, sourceID)
	if err != nil {
		return err
	}
	if len(rows.Values) > 0 {
		return invalid("依赖关系会形成环")
	}
	return nil
}

// RemoveTaskRelation deletes the named link, bumping id's version.
func (s *Store) RemoveTaskRelation(id string, version int, relType, relatedID, by string) (model.RelationResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	task, err := s.taskLocked(ctx, id)
	if err != nil {
		return model.RelationResult{}, err
	}
	related, err := s.taskLocked(ctx, relatedID)
	if err != nil {
		return model.RelationResult{}, err
	}
	if version != task.Version {
		return model.RelationResult{}, &VersionError{Current: task}
	}
	if task.Archived {
		return model.RelationResult{}, invalid("task %s is archived", task.ID)
	}
	if err := validateRelationTasks(task, related); err != nil {
		return model.RelationResult{}, err
	}
	storedType, sourceID, targetID, err := relationEndpoints(relType, task.ID, related.ID)
	if err != nil {
		return model.RelationResult{}, err
	}
	existing, err := s.db.Query(ctx,
		`SELECT 1 FROM task_relations WHERE relation_type = ? AND source_task_id = ? AND target_task_id = ?`,
		storedType, sourceID, targetID)
	if err != nil {
		return model.RelationResult{}, err
	}
	if len(existing.Values) != 1 {
		return model.RelationResult{}, invalid("关系不存在")
	}
	now := nowUTC()
	task.Version = version + 1
	task.UpdatedAt = now
	task.UpdatedBy = by
	stmts := []sqlite.Statement{
		{SQL: `DELETE FROM task_relations WHERE relation_type = ? AND source_task_id = ? AND target_task_id = ?`,
			Args: []any{storedType, sourceID, targetID}},
		s.updateTaskStatement(task, by),
	}
	if _, err := s.db.Transaction(ctx, stmts); err != nil {
		return model.RelationResult{}, err
	}
	if err := s.enrichTaskLocked(ctx, &task); err != nil {
		return model.RelationResult{}, err
	}
	if err := s.enrichTaskLocked(ctx, &related); err != nil {
		return model.RelationResult{}, err
	}
	emit(s, model.Event{Type: model.EventTaskUpdated, ProjectID: task.ProjectID, ID: task.ID, Version: task.Version, By: by, Task: &task})
	emit(s, model.Event{Type: model.EventTaskUpdated, ProjectID: related.ProjectID, ID: related.ID, Version: related.Version, By: by, Task: &related})
	return model.RelationResult{Task: task, RelatedTask: related}, nil
}

// AssertUnblocked reports whether taskID may enter doing/review / be dispatched.
// Returns an InvalidError with a Chinese message when a live blocker remains.
func (s *Store) AssertUnblocked(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	t, err := s.taskLocked(ctx, taskID)
	if err != nil {
		return err
	}
	p, err := s.projectLocked(ctx, t.ProjectID)
	if err != nil {
		return err
	}
	return s.assertUnblockedLocked(ctx, t, p)
}

func (s *Store) assertUnblockedForColumnLocked(ctx context.Context, t model.Task, p model.Project, destColumnID string) error {
	role := ""
	for _, c := range p.Columns {
		if c.ID == destColumnID {
			role = c.Role
			break
		}
	}
	switch role {
	case model.ColumnRoleDoing, model.ColumnRoleReview:
		return s.assertUnblockedLocked(ctx, t, p)
	default:
		return nil
	}
}

func (s *Store) assertUnblockedLocked(ctx context.Context, t model.Task, p model.Project) error {
	return s.assertUnblockedWithExtraLocked(ctx, t, p, nil)
}

func (s *Store) assertUnblockedWithExtraLocked(ctx context.Context, t model.Task, p model.Project, extra *model.TaskRelationSummary) error {
	blockers, err := s.relationSummariesLocked(ctx, RelationBlocks, "source", t.ID)
	if err != nil {
		return err
	}
	if extra != nil {
		blockers = append(blockers, *extra)
	}
	doneIDs := map[string]bool{}
	for _, c := range p.Columns {
		if c.Role == model.ColumnRoleDone {
			doneIDs[c.ID] = true
		}
	}
	var pending []string
	for _, b := range blockers {
		if b.Archived {
			continue
		}
		if doneIDs[b.ColumnID] {
			continue
		}
		label := b.Title
		if b.Number > 0 {
			label = fmt.Sprintf("#%d %s", b.Number, b.Title)
		}
		pending = append(pending, label)
	}
	if len(pending) == 0 {
		return nil
	}
	return invalid("前置未完成：%s", strings.Join(pending, "；"))
}

func validateRelationTasks(task, related model.Task) error {
	if task.ID == related.ID {
		return invalid("不能与自身建立关系")
	}
	if task.ProjectID != related.ProjectID {
		return invalid("关系必须在同一项目内")
	}
	return nil
}

// relationEndpoints maps API type + path/related ids onto the stored edge.
// parent: related is parent of path task → source=related, target=path.
// blocks: path blocks related → source=path, target=related.
// blocked_by: related blocks path → source=related, target=path.
// related: undirected, normalized source < target.
func relationEndpoints(relType, taskID, relatedID string) (storedType, sourceID, targetID string, err error) {
	switch relType {
	case RelationParent:
		return RelationParent, relatedID, taskID, nil
	case RelationBlocks:
		return RelationBlocks, taskID, relatedID, nil
	case RelationBlockedBy:
		return RelationBlocks, relatedID, taskID, nil
	case RelationRelated:
		a, b := taskID, relatedID
		if a > b {
			a, b = b, a
		}
		return RelationRelated, a, b, nil
	default:
		return "", "", "", invalid("未知关系类型 %q", relType)
	}
}

func (s *Store) assertNoParentCycleLocked(ctx context.Context, childID, parentID string) error {
	// Walk ancestors of parentID; if childID appears, attaching parentID as
	// parent of childID would close a cycle.
	rows, err := s.db.Query(ctx, `
WITH RECURSIVE ancestors(id) AS (
  SELECT source_task_id
  FROM task_relations
  WHERE relation_type = 'parent' AND target_task_id = ?
  UNION
  SELECT task_relations.source_task_id
  FROM task_relations
  JOIN ancestors ON task_relations.target_task_id = ancestors.id
  WHERE task_relations.relation_type = 'parent'
)
SELECT 1 FROM ancestors WHERE id = ?`, parentID, childID)
	if err != nil {
		return err
	}
	if len(rows.Values) > 0 {
		return invalid("父子关系会形成环")
	}
	return nil
}

func (s *Store) enrichTaskLocked(ctx context.Context, t *model.Task) error {
	tasks := []model.Task{*t}
	if err := s.enrichTasksLocked(ctx, tasks); err != nil {
		return err
	}
	*t = tasks[0]
	return nil
}

func (s *Store) enrichTasksLocked(ctx context.Context, tasks []model.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	ids := make([]string, len(tasks))
	index := make(map[string]int, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
		index[t.ID] = i
		tasks[i].Relations = model.TaskRelations{}
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(ids)*2)
	for i, id := range ids {
		args[i] = id
		args[len(ids)+i] = id
	}
	rows, err := s.db.Query(ctx, `
SELECT r.relation_type, r.source_task_id, r.target_task_id,
       src.id, src.number, src.title, src.column_id, src.archived, COALESCE(sc.role, ''),
       tgt.id, tgt.number, tgt.title, tgt.column_id, tgt.archived, COALESCE(tc.role, '')
FROM task_relations r
JOIN tasks src ON src.id = r.source_task_id
JOIN tasks tgt ON tgt.id = r.target_task_id
LEFT JOIN "columns" sc ON sc.id = src.column_id
LEFT JOIN "columns" tc ON tc.id = tgt.column_id
WHERE r.source_task_id IN (`+placeholders+`) OR r.target_task_id IN (`+placeholders+`)`,
		args...)
	if err != nil {
		return err
	}
	for _, row := range rows.Values {
		relType := str(row[0])
		sourceID := str(row[1])
		targetID := str(row[2])
		srcSum := summaryFromVals(row[3:9])
		tgtSum := summaryFromVals(row[9:15])
		switch relType {
		case RelationParent:
			if i, ok := index[targetID]; ok {
				parent := srcSum
				tasks[i].Relations.Parent = &parent
			}
			if i, ok := index[sourceID]; ok {
				tasks[i].Relations.SubIssues = append(tasks[i].Relations.SubIssues, tgtSum)
			}
		case RelationBlocks:
			if i, ok := index[targetID]; ok {
				tasks[i].Relations.BlockedBy = append(tasks[i].Relations.BlockedBy, srcSum)
			}
			if i, ok := index[sourceID]; ok {
				tasks[i].Relations.Blocks = append(tasks[i].Relations.Blocks, tgtSum)
			}
		case RelationRelated:
			if i, ok := index[sourceID]; ok {
				tasks[i].Relations.Related = append(tasks[i].Relations.Related, tgtSum)
			}
			if i, ok := index[targetID]; ok {
				tasks[i].Relations.Related = append(tasks[i].Relations.Related, srcSum)
			}
		}
	}
	return nil
}

func summaryFromVals(vals []any) model.TaskRelationSummary {
	return model.TaskRelationSummary{
		ID:         str(vals[0]),
		Number:     int(integer(vals[1])),
		Title:      str(vals[2]),
		ColumnID:   str(vals[3]),
		Archived:   integer(vals[4]) != 0,
		ColumnRole: str(vals[5]),
	}
}

// relationSummariesLocked returns the other ends of edges of storedType where
// side is "source" (we are the target → return sources) or "target".
func (s *Store) relationSummariesLocked(ctx context.Context, storedType, side, taskID string) ([]model.TaskRelationSummary, error) {
	var query string
	switch side {
	case "source":
		query = `
SELECT t.id, t.number, t.title, t.column_id, t.archived, COALESCE(c.role, '')
FROM task_relations r
JOIN tasks t ON t.id = r.source_task_id
LEFT JOIN "columns" c ON c.id = t.column_id
WHERE r.relation_type = ? AND r.target_task_id = ?
ORDER BY t.position, t.created_at, t.id`
	case "target":
		query = `
SELECT t.id, t.number, t.title, t.column_id, t.archived, COALESCE(c.role, '')
FROM task_relations r
JOIN tasks t ON t.id = r.target_task_id
LEFT JOIN "columns" c ON c.id = t.column_id
WHERE r.relation_type = ? AND r.source_task_id = ?
ORDER BY t.position, t.created_at, t.id`
	default:
		return nil, invalid("bad relation side")
	}
	rows, err := s.db.Query(ctx, query, storedType, taskID)
	if err != nil {
		return nil, err
	}
	out := make([]model.TaskRelationSummary, 0, len(rows.Values))
	for _, r := range rows.Values {
		out = append(out, summaryFromVals(r))
	}
	return out, nil
}
