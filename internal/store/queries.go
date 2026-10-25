package store

import (
	"context"

	"taskboard-native/internal/model"
)

// Board is one project's full column-and-card snapshot.
type Board struct {
	Project model.Project  `json:"project"`
	Columns []model.Column `json:"columns"`
	Tasks   []model.Task   `json:"tasks"`
}

const taskColumns = `id, project_id, column_id, title, body, assignee, position, version, archived, last_agent, created_at, updated_at`

func scanTask(vals []any) model.Task {
	t := model.Task{
		ID:          str(vals[0]),
		ProjectID:   str(vals[1]),
		ColumnID:    str(vals[2]),
		Title:       str(vals[3]),
		Description: str(vals[4]),
		Assignee:    str(vals[5]),
		Position:    num(vals[6]),
		Version:     int(integer(vals[7])),
		Archived:    integer(vals[8]) != 0,
		UpdatedBy:   str(vals[9]),
		CreatedAt:   parseTime(str(vals[10])),
		UpdatedAt:   parseTime(str(vals[11])),
	}
	return t
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func integer(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	}
	return 0
}

func num(v any) float64 {
	switch n := v.(type) {
	case int64:
		return float64(n)
	case float64:
		return n
	}
	return 0
}

// Projects lists every project with its columns, oldest first.
func (s *Store) Projects() ([]model.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.projectsLocked(context.Background())
}

func (s *Store) projectsLocked(ctx context.Context) ([]model.Project, error) {
	rows, err := s.db.Query(ctx, `SELECT id, name, position, created_at, updated_at, workspace_path FROM projects ORDER BY position, created_at`)
	if err != nil {
		return nil, err
	}
	out := make([]model.Project, 0, len(rows.Values))
	for _, r := range rows.Values {
		out = append(out, model.Project{
			ID:            str(r[0]),
			Name:          str(r[1]),
			CreatedAt:     parseTime(str(r[3])),
			WorkspacePath: str(r[5]),
		})
	}
	cols, err := s.db.Query(ctx, `SELECT id, project_id, name FROM "columns" ORDER BY position`)
	if err != nil {
		return nil, err
	}
	byProject := map[string][]model.Column{}
	for _, r := range cols.Values {
		id := str(r[1])
		byProject[id] = append(byProject[id], model.Column{ID: str(r[0]), Name: str(r[2])})
	}
	for i := range out {
		out[i].Columns = byProject[out[i].ID]
		if out[i].Columns == nil {
			out[i].Columns = []model.Column{}
		}
	}
	return out, nil
}

// Project returns one project with its columns.
func (s *Store) Project(id string) (model.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.projectLocked(context.Background(), id)
}

func (s *Store) projectLocked(ctx context.Context, id string) (model.Project, error) {
	rows, err := s.db.Query(ctx, `SELECT id, name, created_at, workspace_path FROM projects WHERE id = ?`, id)
	if err != nil {
		return model.Project{}, err
	}
	if len(rows.Values) == 0 {
		return model.Project{}, ErrNotFound
	}
	r := rows.Values[0]
	p := model.Project{ID: str(r[0]), Name: str(r[1]), CreatedAt: parseTime(str(r[2])), WorkspacePath: str(r[3])}
	cols, err := s.db.Query(ctx, `SELECT id, name FROM "columns" WHERE project_id = ? ORDER BY position`, id)
	if err != nil {
		return model.Project{}, err
	}
	p.Columns = make([]model.Column, 0, len(cols.Values))
	for _, c := range cols.Values {
		p.Columns = append(p.Columns, model.Column{ID: str(c[0]), Name: str(c[1])})
	}
	return p, nil
}

// Board returns the project, its columns and every live (non-archived) task.
func (s *Store) Board(projectID string) (Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	p, err := s.projectLocked(ctx, projectID)
	if err != nil {
		return Board{}, err
	}
	tasks, err := s.tasksLocked(ctx, projectID, false)
	if err != nil {
		return Board{}, err
	}
	return Board{Project: p, Columns: p.Columns, Tasks: tasks}, nil
}

// TasksByProject lists one project's tasks by column position. Tasks are
// always scoped to projectID: there is no cross-project listing.
func (s *Store) TasksByProject(projectID string, includeArchived bool) ([]model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.projectLocked(context.Background(), projectID); err != nil {
		return nil, err
	}
	return s.tasksLocked(context.Background(), projectID, includeArchived)
}

func (s *Store) tasksLocked(ctx context.Context, projectID string, includeArchived bool) ([]model.Task, error) {
	query := `SELECT ` + taskColumns + ` FROM tasks WHERE project_id = ?`
	args := []any{projectID}
	if !includeArchived {
		query += ` AND archived = 0`
	}
	// Board order: column order first, then card order inside the column.
	query += ` ORDER BY (SELECT position FROM "columns" WHERE id = tasks.column_id), position, created_at`
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	out := make([]model.Task, 0, len(rows.Values))
	for _, r := range rows.Values {
		out = append(out, scanTask(r))
	}
	return out, nil
}

// Task returns one task, archived or not, with its comments.
func (s *Store) Task(id string) (model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	t, err := s.taskLocked(ctx, id)
	if err != nil {
		return model.Task{}, err
	}
	comments, err := s.commentsLocked(ctx, id)
	if err != nil {
		return model.Task{}, err
	}
	if len(comments) > 0 {
		t.Comments = comments
	}
	return t, nil
}

func (s *Store) taskLocked(ctx context.Context, id string) (model.Task, error) {
	rows, err := s.db.Query(ctx, `SELECT `+taskColumns+` FROM tasks WHERE id = ?`, id)
	if err != nil {
		return model.Task{}, err
	}
	if len(rows.Values) == 0 {
		return model.Task{}, ErrNotFound
	}
	return scanTask(rows.Values[0]), nil
}

// Comments lists a task's comments oldest first.
func (s *Store) Comments(taskID string) ([]model.Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	if _, err := s.taskLocked(ctx, taskID); err != nil {
		return nil, err
	}
	return s.commentsLocked(ctx, taskID)
}

func (s *Store) commentsLocked(ctx context.Context, taskID string) ([]model.Comment, error) {
	rows, err := s.db.Query(ctx, `SELECT id, author, body, created_at FROM comments WHERE task_id = ? ORDER BY created_at, id`, taskID)
	if err != nil {
		return nil, err
	}
	out := make([]model.Comment, 0, len(rows.Values))
	for _, r := range rows.Values {
		out = append(out, model.Comment{
			ID:        str(r[0]),
			Author:    str(r[1]),
			Text:      str(r[2]),
			CreatedAt: parseTime(str(r[3])),
		})
	}
	return out, nil
}
