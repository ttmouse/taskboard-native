package store

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"

	"taskboard-native/internal/model"
)

// ProjectRun returns the latest handoff for a project, or ErrNotFound.
func (s *Store) ProjectRun(projectID string) (model.ProjectRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.projectRunLocked(projectID)
}

func (s *Store) projectRunLocked(projectID string) (model.ProjectRun, error) {
	rows, err := s.db.Query(context.Background(), `SELECT project_id, runtime, status, task_ids, cursor, current_task_id, detail, updated_at FROM project_runs WHERE project_id = ?`, projectID)
	if err != nil {
		return model.ProjectRun{}, err
	}
	if len(rows.Values) == 0 {
		return model.ProjectRun{}, ErrNotFound
	}
	return scanProjectRun(rows.Values[0]), nil
}

// SaveProjectRun upserts one project's handoff. The table keeps one row.
func (s *Store) SaveProjectRun(run model.ProjectRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = time.Now().UTC()
	}
	_, err := s.db.Execute(context.Background(), projectRunStatement(run).SQL, projectRunStatement(run).Args...)
	return err
}

func projectRunStatement(run model.ProjectRun) sqlite.Statement {
	ids := strings.Join(run.TaskIDs, ",")
	return sqlite.Statement{
		SQL:  `INSERT INTO project_runs (project_id, runtime, status, task_ids, cursor, current_task_id, detail, updated_at) VALUES (?,?,?,?,?,?,?,?) ON CONFLICT(project_id) DO UPDATE SET runtime=excluded.runtime, status=excluded.status, task_ids=excluded.task_ids, cursor=excluded.cursor, current_task_id=excluded.current_task_id, detail=excluded.detail, updated_at=excluded.updated_at`,
		Args: []any{run.ProjectID, run.Runtime, run.Status, ids, run.Cursor, run.CurrentTaskID, run.Detail, run.UpdatedAt.UTC().Format(time.RFC3339)},
	}
}

func scanProjectRun(r []any) model.ProjectRun {
	var ids []string
	if raw := str(r[3]); raw != "" {
		ids = strings.Split(raw, ",")
	}
	return model.ProjectRun{
		ProjectID: str(r[0]), Runtime: str(r[1]), Status: str(r[2]), TaskIDs: ids,
		Cursor: int(integer(r[4])), CurrentTaskID: str(r[5]), Detail: str(r[6]), UpdatedAt: parseTime(str(r[7])),
	}
}
