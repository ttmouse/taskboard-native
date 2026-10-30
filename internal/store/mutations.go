package store

import (
	"context"
	"fmt"
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

// ProjectPatch edits a project's mutable fields. A nil pointer leaves the
// field alone; a non-nil empty WorkspacePath string clears the path.
type ProjectPatch struct {
	Name          *string
	WorkspacePath *string
	Color         *string
}

// UpdateProject applies a ProjectPatch and emits project.changed. Name must
// stay non-empty; the workspace path is trusted to have been validated at
// the HTTP boundary (same split as CreateProjectWithPath).
func (s *Store) UpdateProject(id string, patch ProjectPatch, by string) (model.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	p, err := s.projectLocked(ctx, id)
	if err != nil {
		return model.Project{}, err
	}
	name := p.Name
	if patch.Name != nil {
		name = strings.TrimSpace(*patch.Name)
		if name == "" {
			return model.Project{}, invalid("project name required")
		}
	}
	if patch.WorkspacePath != nil {
		p.WorkspacePath = *patch.WorkspacePath
	}
	if patch.Color != nil {
		p.Color = *patch.Color
	}
	p.Name = name
	now := nowUTC()
	if _, err := s.db.Execute(ctx,
		`UPDATE projects SET name = ?, workspace_path = ?, color = ?, updated_at = ? WHERE id = ?`,
		p.Name, p.WorkspacePath, p.Color, timestamp(now), id,
	); err != nil {
		return model.Project{}, err
	}
	emit(s, model.Event{Type: model.EventProjectChange, ProjectID: p.ID, ID: p.ID, By: by})
	return p, nil
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
	return s.AddCommentTo(taskID, text, by, "")
}

// AddCommentTo appends a comment addressed to one execution channel (@mention).
// An empty mention means the comment carries no address. The store does not
// know channel names; the service validates the id before calling here.
func (s *Store) AddCommentTo(taskID, text, by, mention string) (model.Comment, error) {
	return s.AddCommentWithParent(taskID, text, by, mention, "")
}

// AddCommentWithParent appends a top-level comment or a reply within the same task.
func (s *Store) AddCommentWithParent(taskID, text, by, mention, parentCommentID string) (model.Comment, error) {
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
	if parentCommentID != "" {
		comments, err := s.commentsLocked(ctx, taskID)
		if err != nil {
			return model.Comment{}, err
		}
		found := false
		for _, comment := range comments {
			if comment.ID == parentCommentID {
				found = true
				break
			}
		}
		if !found {
			return model.Comment{}, invalid("parent comment must belong to the task")
		}
	}
	c := model.Comment{ID: newID(), Author: by, Text: text, Mention: mention, ParentCommentID: parentCommentID, CreatedAt: nowUTC()}
	if _, err := s.db.Execute(ctx,
		`INSERT INTO comments (id, task_id, author, body, mention, parent_comment_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.ID, taskID, c.Author, c.Text, c.Mention, c.ParentCommentID, timestamp(c.CreatedAt),
	); err != nil {
		return model.Comment{}, err
	}
	emit(s, model.Event{Type: model.EventCommentAdded, ProjectID: t.ProjectID, ID: t.ID, Version: t.Version, By: by, Comment: &c})
	return c, nil
}

// UpdateComment changes the text of a comment authored by by. It keeps the
// original author, address and timestamp; changing it never replays followup.
func (s *Store) UpdateComment(taskID, commentID, text, by string) (model.Comment, error) {
	if strings.TrimSpace(text) == "" {
		return model.Comment{}, invalid("comment text required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	task, err := s.taskLocked(ctx, taskID)
	if err != nil {
		return model.Comment{}, err
	}
	comments, err := s.commentsLocked(ctx, taskID)
	if err != nil {
		return model.Comment{}, err
	}
	for _, comment := range comments {
		if comment.ID != commentID {
			continue
		}
		if comment.Author != by {
			return model.Comment{}, ErrForbidden
		}
		comment.Text = text
		if _, err := s.db.Execute(ctx, `UPDATE comments SET body=? WHERE id=? AND task_id=?`, text, commentID, taskID); err != nil {
			return model.Comment{}, err
		}
		emit(s, model.Event{Type: model.EventCommentUpdated, ProjectID: task.ProjectID, ID: taskID, Version: task.Version, By: by, Comment: &comment})
		return comment, nil
	}
	return model.Comment{}, ErrNotFound
}

// DeleteComment removes a comment authored by by. Followup delivery records
// remain as an audit trail and the removed comment is not replayed.
func (s *Store) DeleteComment(taskID, commentID, by string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	task, err := s.taskLocked(ctx, taskID)
	if err != nil {
		return err
	}
	comments, err := s.commentsLocked(ctx, taskID)
	if err != nil {
		return err
	}
	for _, comment := range comments {
		if comment.ID != commentID {
			continue
		}
		if comment.Author != by {
			return ErrForbidden
		}
		rows, err := s.db.Query(ctx, `SELECT id, author, body, created_at, mention, parent_comment_id FROM comments WHERE task_id=? AND parent_comment_id=? ORDER BY created_at, rowid`, taskID, commentID)
		if err != nil {
			return err
		}
		for _, row := range rows.Values {
			reply := model.Comment{ID: str(row[0]), Author: str(row[1]), Text: str(row[2]), CreatedAt: parseTime(str(row[3])), Mention: str(row[4])}
			if _, err := s.db.Execute(ctx, `UPDATE comments SET parent_comment_id='' WHERE id=? AND task_id=?`, reply.ID, taskID); err != nil {
				return err
			}
			reply.ParentCommentID = ""
			emit(s, model.Event{Type: model.EventCommentUpdated, ProjectID: task.ProjectID, ID: taskID, Version: task.Version, By: by, Comment: &reply})
		}
		if _, err := s.db.Execute(ctx, `DELETE FROM comments WHERE id=? AND task_id=?`, commentID, taskID); err != nil {
			return err
		}
		emit(s, model.Event{Type: model.EventCommentDeleted, ProjectID: task.ProjectID, ID: taskID, Version: task.Version, By: by, Comment: &comment})
		return nil
	}
	return ErrNotFound
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

// RoleConflictError reports that another column of the same project already
// carries the role. Roles are unique per project so nothing silently steals
// a workflow step.
type RoleConflictError struct {
	Role     string
	ColumnID string // the column already holding Role
}

func (e *RoleConflictError) Error() string {
	return fmt.Sprintf("role %q is already taken by column %s", e.Role, e.ColumnID)
}

// SetColumnRole labels a column with a workflow role; an empty role clears
// it. The column must belong to the project. On success it emits
// project.changed, so clients refresh their columns in place.
func (s *Store) SetColumnRole(projectID, columnID, role string, by string) (model.Column, error) {
	if !model.ValidColumnRole(role) {
		return model.Column{}, invalid("unknown role %q", role)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	p, err := s.projectLocked(ctx, projectID)
	if err != nil {
		return model.Column{}, err
	}
	at := -1
	for i, c := range p.Columns {
		if c.ID == columnID {
			at = i
		}
	}
	if at < 0 {
		return model.Column{}, ErrNotFound
	}
	if role != "" {
		for _, c := range p.Columns {
			if c.ID != columnID && c.Role == role {
				return model.Column{}, &RoleConflictError{Role: role, ColumnID: c.ID}
			}
		}
	}
	if _, err := s.db.Execute(ctx,
		`UPDATE "columns" SET role = ? WHERE id = ? AND project_id = ?`, role, columnID, projectID,
	); err != nil {
		return model.Column{}, err
	}
	p.Columns[at].Role = role
	emit(s, model.Event{Type: model.EventProjectChange, ProjectID: p.ID, ID: p.ID, By: by})
	return p.Columns[at], nil
}

// DeleteProject removes a project and everything under it: its columns, all
// of its tasks — archived ones included — and the comments, executions and
// followups hanging off those tasks. One transaction; the project.changed
// event lets every open client drop the project and, if it was on screen,
// fall back to the global view. There is no undo: the GUI confirms first.
func (s *Store) DeleteProject(id, by string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	if _, err := s.projectLocked(ctx, id); err != nil {
		return err
	}
	active, err := s.db.Query(ctx, `SELECT e.session_id, e.status, e.runtime, e.detail, e.followup_id, e.previous_turn_id, e.task_id
		FROM executions e JOIN tasks t ON t.id = e.task_id WHERE t.project_id = ?`, id)
	if err != nil {
		return err
	}
	for _, row := range active.Values {
		run := model.Execution{
			SessionID: str(row[0]), Status: str(row[1]), Runtime: str(row[2]),
			Detail: str(row[3]), FollowupID: str(row[4]), PreviousTurnID: str(row[5]),
		}
		if run.Active() {
			return fmt.Errorf("%w: task %s", ErrActiveExecution, str(row[6]))
		}
	}
	stmts := []sqlite.Statement{
		{SQL: `DELETE FROM followups WHERE task_id IN (SELECT id FROM tasks WHERE project_id = ?)`, Args: []any{id}},
		{SQL: `DELETE FROM executions WHERE task_id IN (SELECT id FROM tasks WHERE project_id = ?)`, Args: []any{id}},
		{SQL: `DELETE FROM comments WHERE task_id IN (SELECT id FROM tasks WHERE project_id = ?)`, Args: []any{id}},
		{SQL: `DELETE FROM tasks WHERE project_id = ?`, Args: []any{id}},
		{SQL: `DELETE FROM "columns" WHERE project_id = ?`, Args: []any{id}},
		{SQL: `DELETE FROM projects WHERE id = ?`, Args: []any{id}},
	}
	if _, err := s.db.Transaction(ctx, stmts); err != nil {
		return err
	}
	emit(s, model.Event{Type: model.EventProjectChange, ProjectID: id, ID: id, By: by})
	return nil
}

// CreateProject appends a project with the standard four columns.
func (s *Store) CreateProject(name, by string) (model.Project, error) {
	return s.CreateProjectWithColor(name, "", "", by)
}

// CreateProjectWithPath persists a workspace location without touching
// the workspace itself. The HTTP boundary validates supplied paths.
func (s *Store) CreateProjectWithPath(name, workspacePath, by string) (model.Project, error) {
	return s.CreateProjectWithColor(name, workspacePath, "", by)
}

// CreateProjectWithColor persists a workspace location and an accent color
// (a "#rrggbb" hex string, empty for the window's stable palette slot).
// The HTTP boundary validates supplied paths.
func (s *Store) CreateProjectWithColor(name, workspacePath, color, by string) (model.Project, error) {
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
	p := model.Project{ID: newID(), Name: name, WorkspacePath: workspacePath, Color: color, CreatedAt: now}
	stmts := []sqlite.Statement{{
		SQL:  `INSERT INTO projects (id, name, position, created_at, updated_at, workspace_path, color) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		Args: []any{p.ID, p.Name, position, timestamp(now), timestamp(now), p.WorkspacePath, p.Color},
	}}
	for i, column := range DefaultColumns() {
		column.ID = newID()
		p.Columns = append(p.Columns, column)
		stmts = append(stmts, sqlite.Statement{
			SQL:  `INSERT INTO "columns" (id, project_id, name, position, role) VALUES (?, ?, ?, ?, ?)`,
			Args: []any{column.ID, p.ID, column.Name, (i + 1) * step, column.Role},
		})
	}
	if _, err := s.db.Transaction(ctx, stmts); err != nil {
		return model.Project{}, err
	}
	emit(s, model.Event{Type: model.EventProjectChange, ProjectID: p.ID, ID: p.ID, By: by})
	return p, nil
}
