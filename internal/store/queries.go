package store

import (
	"context"
	"strconv"
	"strings"

	"taskboard-native/internal/model"
)

// Board is one project's full column-and-card snapshot.
type Board = model.ProjectBoard

const taskColumns = `tasks.id, tasks.number, tasks.project_id, tasks.column_id, tasks.title, tasks.body, tasks.assignee, tasks.position, tasks.version, tasks.archived, tasks.last_agent, tasks.created_at, tasks.updated_at,
  COALESCE((SELECT body FROM comments WHERE task_id = tasks.id
    AND (body LIKE 'codex 执行完成（%' OR body LIKE 'dsh 执行完成（%' OR body LIKE 'pi 执行完成（%')
    AND instr(body, '）。输出：' || char(10) || char(10)) > 0
    ORDER BY created_at DESC, rowid DESC LIMIT 1), ''),
  e.task_id, e.session_id, e.status, e.detail, e.runtime, e.followup_id, e.previous_turn_id`

func scanTask(vals []any) model.Task {
	t := model.Task{
		ID:          str(vals[0]),
		Number:      int(integer(vals[1])),
		ProjectID:   str(vals[2]),
		ColumnID:    str(vals[3]),
		Title:       str(vals[4]),
		Description: str(vals[5]),
		Assignee:    str(vals[6]),
		Position:    num(vals[7]),
		Version:     int(integer(vals[8])),
		Archived:    integer(vals[9]) != 0,
		UpdatedBy:   str(vals[10]),
		CreatedAt:   parseTime(str(vals[11])),
		UpdatedAt:   parseTime(str(vals[12])),
	}
	t.CompletedBy = model.CompletingAgent(model.Comment{Text: str(vals[13])})
	if str(vals[14]) != "" {
		t.Execution = &model.Execution{
			TaskID: str(vals[14]), SessionID: str(vals[15]), Status: str(vals[16]),
			Detail: str(vals[17]), Runtime: str(vals[18]), FollowupID: str(vals[19]),
			PreviousTurnID: str(vals[20]),
		}
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

// Snapshot returns a consistent view of the board and the event sequence
// while holding the store lock. SSE subscribers register before taking this
// snapshot, so mutations after it are queued and delivered afterward.
func (s *Store) Snapshot(projectID string) (model.BoardSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	projects, err := s.projectsLocked(ctx)
	if err != nil {
		return model.BoardSnapshot{}, err
	}
	if projectID != "" {
		filtered := projects[:0]
		for _, p := range projects {
			if p.ID == projectID {
				filtered = append(filtered, p)
			}
		}
		projects = filtered
	}
	snapshot := model.BoardSnapshot{
		Seq:      s.seq,
		Projects: projects,
		Boards:   make([]model.ProjectBoard, 0, len(projects)),
	}
	for _, p := range projects {
		tasks, err := s.tasksLocked(ctx, p.ID, false)
		if err != nil {
			return model.BoardSnapshot{}, err
		}
		snapshot.Boards = append(snapshot.Boards, model.ProjectBoard{Project: p, Columns: p.Columns, Tasks: tasks})
	}
	return snapshot, nil
}

func (s *Store) projectsLocked(ctx context.Context) ([]model.Project, error) {
	rows, err := s.db.Query(ctx, `SELECT p.id, p.name, p.position, p.created_at, p.updated_at, p.workspace_path, p.color,
	COALESCE((SELECT MAX(t.updated_at) FROM tasks t WHERE t.project_id = p.id AND t.archived = 0), p.created_at)
	FROM projects p ORDER BY p.position, p.created_at`)
	if err != nil {
		return nil, err
	}
	out := make([]model.Project, 0, len(rows.Values))
	for _, r := range rows.Values {
		out = append(out, model.Project{
			ID:            str(r[0]),
			Name:          str(r[1]),
			CreatedAt:     parseTime(str(r[3])),
			UpdatedAt:     parseTime(str(r[4])),
			WorkspacePath: str(r[5]),
			Color:         str(r[6]),
			ActiveAt:      parseTime(str(r[7])),
		})
	}
	cols, err := s.db.Query(ctx, `SELECT id, project_id, name, role FROM "columns" ORDER BY position`)
	if err != nil {
		return nil, err
	}
	byProject := map[string][]model.Column{}
	for _, r := range cols.Values {
		id := str(r[1])
		byProject[id] = append(byProject[id], model.Column{ID: str(r[0]), Name: str(r[2]), Role: str(r[3])})
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
	rows, err := s.db.Query(ctx, `SELECT p.id, p.name, p.created_at, p.updated_at, p.workspace_path, p.color,
	COALESCE((SELECT MAX(t.updated_at) FROM tasks t WHERE t.project_id = p.id AND t.archived = 0), p.created_at)
	FROM projects p WHERE p.id = ?`, id)
	if err != nil {
		return model.Project{}, err
	}
	if len(rows.Values) == 0 {
		return model.Project{}, ErrNotFound
	}
	r := rows.Values[0]
	p := model.Project{
		ID: str(r[0]), Name: str(r[1]), CreatedAt: parseTime(str(r[2])),
		UpdatedAt: parseTime(str(r[3])), WorkspacePath: str(r[4]), Color: str(r[5]),
		ActiveAt: parseTime(str(r[6])),
	}
	cols, err := s.db.Query(ctx, `SELECT id, name, role FROM "columns" WHERE project_id = ? ORDER BY position`, id)
	if err != nil {
		return model.Project{}, err
	}
	p.Columns = make([]model.Column, 0, len(cols.Values))
	for _, c := range cols.Values {
		p.Columns = append(p.Columns, model.Column{ID: str(c[0]), Name: str(c[1]), Role: str(c[2])})
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
	query := `SELECT ` + taskColumns + ` FROM tasks LEFT JOIN executions e ON e.task_id = tasks.id WHERE tasks.project_id = ?`
	args := []any{projectID}
	if !includeArchived {
		query += ` AND tasks.archived = 0`
	}
	// Board order: column order first, then card order inside the column.
	query += ` ORDER BY (SELECT position FROM "columns" WHERE id = tasks.column_id), tasks.position, tasks.created_at`
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	out := make([]model.Task, 0, len(rows.Values))
	for _, r := range rows.Values {
		out = append(out, scanTask(r))
	}
	if err := s.enrichTasksLocked(ctx, out); err != nil {
		return nil, err
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
	comments, err := s.commentsLocked(ctx, t.ID)
	if err != nil {
		return model.Task{}, err
	}
	if len(comments) > 0 {
		t.Comments = comments
	}
	return t, nil
}

func (s *Store) taskLocked(ctx context.Context, id string) (model.Task, error) {
	resolved, err := s.resolveTaskIDLocked(ctx, id)
	if err != nil {
		return model.Task{}, err
	}
	rows, err := s.db.Query(ctx, `SELECT `+taskColumns+` FROM tasks LEFT JOIN executions e ON e.task_id = tasks.id WHERE tasks.id = ?`, resolved)
	if err != nil {
		return model.Task{}, err
	}
	if len(rows.Values) == 0 {
		return model.Task{}, ErrNotFound
	}
	t := scanTask(rows.Values[0])
	if err := s.enrichTaskLocked(ctx, &t); err != nil {
		return model.Task{}, err
	}
	return t, nil
}

// resolveTaskIDLocked accepts a human-facing decimal task number, an exact
// task id (any length), or a unique hex prefix of at least HexPrefixMinLen
// characters.
func (s *Store) resolveTaskIDLocked(ctx context.Context, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", ErrNotFound
	}
	// Exact primary-key hit first so fixture/short ids and full UUIDs both work.
	exact, err := s.db.Query(ctx, `SELECT id FROM tasks WHERE id = ?`, id)
	if err != nil {
		return "", err
	}
	if len(exact.Values) == 1 {
		return str(exact.Values[0][0]), nil
	}
	if isAllDigits(id) {
		n, err := strconv.Atoi(id)
		if err != nil || n <= 0 {
			return "", ErrNotFound
		}
		rows, err := s.db.Query(ctx, `SELECT id FROM tasks WHERE number = ?`, n)
		if err != nil {
			return "", err
		}
		if len(rows.Values) == 1 {
			return str(rows.Values[0][0]), nil
		}
		if len(rows.Values) > 1 {
			return "", invalid("task number %q matches %d tasks", id, len(rows.Values))
		}
		if len(id) < HexPrefixMinLen {
			return "", ErrNotFound
		}
	}
	if len(id) < HexPrefixMinLen {
		return "", ErrNotFound
	}
	rows, err := s.db.Query(ctx, `SELECT id FROM tasks WHERE id LIKE ? ESCAPE '\' AND archived = 0`, likePrefix(id))
	if err != nil {
		return "", err
	}
	switch len(rows.Values) {
	case 0:
		// Also try including archived, so short copy still opens old cards.
		rows, err = s.db.Query(ctx, `SELECT id FROM tasks WHERE id LIKE ? ESCAPE '\'`, likePrefix(id))
		if err != nil {
			return "", err
		}
		if len(rows.Values) == 0 {
			return "", ErrNotFound
		}
		if len(rows.Values) > 1 {
			return "", invalid("task id %q matches %d tasks; paste more characters", id, len(rows.Values))
		}
		return str(rows.Values[0][0]), nil
	case 1:
		return str(rows.Values[0][0]), nil
	default:
		return "", invalid("task id %q matches %d tasks; paste more characters", id, len(rows.Values))
	}
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func likePrefix(id string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(id) + `%`
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
	rows, err := s.db.Query(ctx, `SELECT id, author, body, created_at, mention, parent_comment_id FROM comments WHERE task_id = ? ORDER BY created_at, rowid`, taskID)
	if err != nil {
		return nil, err
	}
	out := make([]model.Comment, 0, len(rows.Values))
	for _, r := range rows.Values {
		out = append(out, model.Comment{
			ID:              str(r[0]),
			Author:          str(r[1]),
			Text:            str(r[2]),
			CreatedAt:       parseTime(str(r[3])),
			Mention:         str(r[4]),
			ParentCommentID: str(r[5]),
		})
	}
	return out, nil
}
