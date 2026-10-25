package store

import (
	"context"
	"strings"

	"github.com/egoist/mygo/plugins/sqlite"

	"taskboard-native/internal/model"
)

// TaskPatch carries an optional field update. A non-nil ColumnID moves the
// task into that column. Placement inside the target column: AtTop puts the
// card first, AfterID puts it right after that card, and otherwise it goes
// to the bottom. Archived=false restores an archived card to the bottom of
// its column; Archived=true archives a live card.
type TaskPatch struct {
	Title       *string
	Description *string
	Assignee    *string
	ColumnID    *string
	AfterID     string
	AtTop       bool
	Archived    *bool
}

// CreateTask appends a card to the bottom of a column. An empty columnID
// means the project's first column.
func (s *Store) CreateTask(projectID, columnID, title, description, assignee, by string) (model.Task, error) {
	if title == "" {
		return model.Task{}, invalid("title required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	p, err := s.projectLocked(ctx, projectID)
	if err != nil {
		return model.Task{}, err
	}
	if columnID == "" {
		if len(p.Columns) == 0 {
			return model.Task{}, invalid("project has no columns")
		}
		columnID = p.Columns[0].ID
	}
	if err := hasColumn(p, columnID); err != nil {
		return model.Task{}, err
	}
	order, err := s.columnOrderLocked(ctx, projectID, columnID, "")
	if err != nil {
		return model.Task{}, err
	}
	now := nowUTC()
	t := model.Task{
		ID:          newID(),
		ProjectID:   projectID,
		ColumnID:    columnID,
		Title:       title,
		Description: description,
		Assignee:    assignee,
		Position:    float64((len(order) + 1) * step),
		Version:     1,
		UpdatedBy:   by,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	stmts := []sqlite.Statement{{
		SQL: `INSERT INTO tasks (id, project_id, column_id, title, body, assignee, position, version, archived, last_agent, created_at, updated_at)
		      VALUES (?, ?, ?, ?, ?, ?, ?, 1, 0, ?, ?, ?)`,
		Args: []any{t.ID, t.ProjectID, t.ColumnID, t.Title, t.Description, t.Assignee, t.Position, by, timestamp(now), timestamp(now)},
	}}
	stmts = append(stmts, renumber(order, "")...)
	if _, err := s.db.Transaction(ctx, stmts); err != nil {
		return model.Task{}, err
	}
	emit(s, model.Event{Type: model.EventTaskCreated, ProjectID: projectID, ID: t.ID, Version: t.Version, By: by, Task: &t})
	return t, nil
}

// PatchTask applies field edits, an optional column move and an optional
// archived transition under an optimistic version check, bumping the version
// once. The emitted event is task.moved (with from/to) when the column
// changes, task.archived when the card is archived, task.updated otherwise
// (including a restore).
func (s *Store) PatchTask(id string, version int, patch TaskPatch, by string) (model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	t, err := s.taskLocked(ctx, id)
	if err != nil {
		return model.Task{}, err
	}
	if version != t.Version {
		return t, &VersionError{Current: t}
	}
	// restore un-archives; archived:false on a live card is a plain no-op
	// patch, so it must not reorder anything.
	restore := patch.Archived != nil && !*patch.Archived && t.Archived
	archive := patch.Archived != nil && *patch.Archived
	if t.Archived && !restore {
		return t, invalid("task %s is archived", id)
	}
	if patch.Title != nil && *patch.Title == "" {
		return t, invalid("title cannot be empty")
	}
	from := t.ColumnID
	if patch.Title != nil {
		t.Title = *patch.Title
	}
	if patch.Description != nil {
		t.Description = *patch.Description
	}
	if patch.Assignee != nil {
		t.Assignee = *patch.Assignee
	}
	if patch.ColumnID != nil {
		p, err := s.projectLocked(ctx, t.ProjectID)
		if err != nil {
			return t, err
		}
		if err := hasColumn(p, *patch.ColumnID); err != nil {
			return t, err
		}
		t.ColumnID = *patch.ColumnID
	}

	var stmts []sqlite.Statement
	// A restored card re-enters its column at the bottom, so it needs a free
	// slot on the column grid like any moved card.
	if patch.ColumnID != nil || patch.AfterID != "" || patch.AtTop || restore {
		pos, place, err := s.placeLocked(ctx, t.ProjectID, t.ColumnID, t.ID, patch.AfterID, patch.AtTop)
		if err != nil {
			return t, err
		}
		t.Position = pos
		stmts = append(stmts, place...)
	}
	if from != t.ColumnID {
		compact, err := s.compactLocked(ctx, t.ProjectID, from, t.ID)
		if err != nil {
			return t, err
		}
		stmts = append(stmts, compact...)
	}
	switch {
	case restore:
		t.Archived = false
	case archive:
		t.Archived = true
		compact, err := s.compactLocked(ctx, t.ProjectID, t.ColumnID, t.ID)
		if err != nil {
			return t, err
		}
		stmts = append(stmts, compact...)
	}
	t.Version = version + 1
	t.UpdatedAt = nowUTC()
	t.UpdatedBy = by
	if _, err := s.db.Transaction(ctx, append(stmts, s.updateTaskStatement(t, by))); err != nil {
		return model.Task{}, err
	}

	ev := model.Event{Type: model.EventTaskUpdated, ProjectID: t.ProjectID, ID: t.ID, Version: t.Version, By: by, Task: &t}
	switch {
	case from != t.ColumnID:
		ev.Type = model.EventTaskMoved
		ev.Column = &struct {
			FromID string `json:"fromId"`
			ToID   string `json:"toId"`
		}{from, t.ColumnID}
	case archive:
		ev.Type = model.EventTaskArchived
	}
	emit(s, ev)
	return t, nil
}

// MoveTask moves a card into columnID after afterID (empty = bottom).
func (s *Store) MoveTask(id string, version int, columnID, afterID, by string) (model.Task, error) {
	return s.PatchTask(id, version, TaskPatch{ColumnID: &columnID, AfterID: afterID}, by)
}

// MoveTaskToTop moves a card to the head of columnID.
func (s *Store) MoveTaskToTop(id string, version int, columnID, by string) (model.Task, error) {
	return s.PatchTask(id, version, TaskPatch{ColumnID: &columnID, AtTop: true}, by)
}

// ArchiveTask soft-deletes a task, bumping its version.
func (s *Store) ArchiveTask(id string, version int, by string) (model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	t, err := s.taskLocked(ctx, id)
	if err != nil {
		return model.Task{}, err
	}
	if version != t.Version {
		return t, &VersionError{Current: t}
	}
	if t.Archived {
		return t, invalid("task %s is already archived", id)
	}
	t.Archived = true
	t.Version = version + 1
	t.UpdatedAt = nowUTC()
	t.UpdatedBy = by
	stmts := []sqlite.Statement{{
		SQL:  `UPDATE tasks SET archived = 1, version = ?, last_agent = ?, updated_at = ? WHERE id = ?`,
		Args: []any{t.Version, by, timestamp(t.UpdatedAt), t.ID},
	}}
	compact, err := s.compactLocked(ctx, t.ProjectID, t.ColumnID, t.ID)
	if err != nil {
		return t, err
	}
	stmts = append(stmts, compact...)
	if _, err := s.db.Transaction(ctx, stmts); err != nil {
		return model.Task{}, err
	}
	emit(s, model.Event{Type: model.EventTaskArchived, ProjectID: t.ProjectID, ID: t.ID, Version: t.Version, By: by, Task: &t})
	return t, nil
}

// AddComment appends a comment; the task version is untouched.
func (s *Store) AddComment(taskID, text, by string) (model.Comment, error) {
	if text == "" {
		return model.Comment{}, invalid("comment text required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	t, err := s.taskLocked(ctx, taskID)
	if err != nil {
		return model.Comment{}, err
	}
	c := model.Comment{ID: newID(), Author: by, Text: text, CreatedAt: nowUTC()}
	if _, err := s.db.Execute(ctx,
		`INSERT INTO comments (id, task_id, author, body, created_at) VALUES (?, ?, ?, ?, ?)`,
		c.ID, taskID, c.Author, c.Text, timestamp(c.CreatedAt),
	); err != nil {
		return model.Comment{}, err
	}
	emit(s, model.Event{Type: model.EventCommentAdded, ProjectID: t.ProjectID, ID: t.ID, Version: t.Version, By: by, Comment: &c})
	return c, nil
}

// --- helpers (caller holds s.mu) ---

// step is the gap between neighbouring cards; columns are always compacted
// to multiples of it, so positions stay exact integers.
const step = 1000

func hasColumn(p model.Project, columnID string) error {
	for _, c := range p.Columns {
		if c.ID == columnID {
			return nil
		}
	}
	return invalid("unknown column %q", columnID)
}

func (s *Store) updateTaskStatement(t model.Task, by string) sqlite.Statement {
	return sqlite.Statement{
		SQL:  `UPDATE tasks SET column_id = ?, title = ?, body = ?, assignee = ?, position = ?, version = ?, archived = ?, last_agent = ?, updated_at = ? WHERE id = ?`,
		Args: []any{t.ColumnID, t.Title, t.Description, t.Assignee, t.Position, t.Version, t.Archived, by, timestamp(t.UpdatedAt), t.ID},
	}
}

// cardPos is one live card's id and stored position inside a column.
type cardPos struct {
	id  string
	pos float64
}

// columnOrderLocked lists a column's live cards in position order,
// excluding excludeID.
func (s *Store) columnOrderLocked(ctx context.Context, projectID, columnID, excludeID string) ([]cardPos, error) {
	query := `SELECT id, position FROM tasks WHERE project_id = ? AND column_id = ? AND archived = 0`
	args := []any{projectID, columnID}
	if excludeID != "" {
		query += ` AND id <> ?`
		args = append(args, excludeID)
	}
	query += ` ORDER BY position, created_at`
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	out := make([]cardPos, 0, len(rows.Values))
	for _, r := range rows.Values {
		out = append(out, cardPos{str(r[0]), num(r[1])})
	}
	return out, nil
}

// renumber returns the statements that put cards on the step grid, leaving
// skipID (the card the caller updates itself) alone. Cards already on the
// grid cost nothing, so a write touches only what it has to.
func renumber(cards []cardPos, skipID string) []sqlite.Statement {
	var stmts []sqlite.Statement
	for i, card := range cards {
		if card.id == skipID {
			continue
		}
		want := float64((i + 1) * step)
		if card.pos == want {
			continue
		}
		stmts = append(stmts, sqlite.Statement{
			SQL:  `UPDATE tasks SET position = ? WHERE id = ?`,
			Args: []any{want, card.id},
		})
	}
	return stmts
}

// insertIndex returns where a card belongs among cards: first when atTop,
// right after afterID when it names one of them, bottom otherwise.
func insertIndex(cards []cardPos, afterID string, atTop bool) int {
	if atTop {
		return 0
	}
	if afterID != "" {
		for i, card := range cards {
			if card.id == afterID {
				return i + 1
			}
		}
	}
	return len(cards)
}

// placeLocked returns the moved card's new position plus the statements
// that keep its column on the step grid.
func (s *Store) placeLocked(ctx context.Context, projectID, columnID, id, afterID string, atTop bool) (float64, []sqlite.Statement, error) {
	cards, err := s.columnOrderLocked(ctx, projectID, columnID, id)
	if err != nil {
		return 0, nil, err
	}
	at := insertIndex(cards, afterID, atTop)
	cards = append(cards, cardPos{})
	copy(cards[at+1:], cards[at:])
	cards[at] = cardPos{id: id, pos: float64((at + 1) * step)}
	return cards[at].pos, renumber(cards, id), nil
}

// compactLocked renumbers a column's live cards onto the step grid, skipping
// the card id (the caller updates that one itself).
func (s *Store) compactLocked(ctx context.Context, projectID, columnID, skipID string) ([]sqlite.Statement, error) {
	cards, err := s.columnOrderLocked(ctx, projectID, columnID, skipID)
	if err != nil {
		return nil, err
	}
	return renumber(cards, ""), nil
}

// CreateProject appends a project with the standard four columns.
func (s *Store) CreateProject(name, by string) (model.Project, error) {
	return s.CreateProjectWithPath(name, "", by)
}

// CreateProjectWithPath persists a workspace location without touching
// the workspace itself. The HTTP boundary validates supplied paths.
func (s *Store) CreateProjectWithPath(name, workspacePath, by string) (model.Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return model.Project{}, invalid("project name required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	rows, err := s.db.Query(ctx, `SELECT COALESCE(MAX(position), 0) FROM projects`)
	if err != nil {
		return model.Project{}, err
	}
	position := integer(rows.Values[0][0]) + step
	now := nowUTC()
	p := model.Project{ID: newID(), Name: name, WorkspacePath: workspacePath, CreatedAt: now}
	stmts := []sqlite.Statement{{
		SQL:  `INSERT INTO projects (id, name, position, created_at, updated_at, workspace_path) VALUES (?, ?, ?, ?, ?, ?)`,
		Args: []any{p.ID, p.Name, position, timestamp(now), timestamp(now), p.WorkspacePath},
	}}
	for i, column := range DefaultColumns() {
		column.ID = newID()
		p.Columns = append(p.Columns, column)
		stmts = append(stmts, sqlite.Statement{
			SQL:  `INSERT INTO "columns" (id, project_id, name, position) VALUES (?, ?, ?, ?)`,
			Args: []any{column.ID, p.ID, column.Name, (i + 1) * step},
		})
	}
	if _, err := s.db.Transaction(ctx, stmts); err != nil {
		return model.Project{}, err
	}
	emit(s, model.Event{Type: model.EventProjectChange, ProjectID: p.ID, ID: p.ID, By: by})
	return p, nil
}
