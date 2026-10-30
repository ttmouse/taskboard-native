package store

import (
	"context"
	"github.com/egoist/mygo/plugins/sqlite"
	"taskboard-native/internal/model"
)

// Execution returns the durable identity and last observed state of a dispatch.
func (s *Store) Execution(taskID string) (*model.Execution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.executionLocked(taskID)
}

func (s *Store) executionLocked(taskID string) (*model.Execution, error) {
	rows, err := s.db.Query(context.Background(), `SELECT task_id, session_id, status, detail, runtime, followup_id, previous_turn_id FROM executions WHERE task_id = ?`, taskID)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) == 0 {
		return nil, nil
	}
	r := rows.Values[0]
	return executionRow(r), nil
}

func (s *Store) SaveExecution(e model.Execution) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.executionLocked(e.TaskID)
	if err != nil {
		return err
	}
	if current != nil && *current == e {
		return nil
	}
	stmt := executionStatement(e)
	if _, err := s.db.Execute(context.Background(), stmt.SQL, stmt.Args...); err != nil {
		return err
	}
	return s.emitExecutionUpdatedLocked(e.TaskID)
}

// ReleaseExecution converges an execution record to the released terminal state
// so it stops holding the card's write lock. It returns the updated record, or
// nil when the card has no execution at all.
func (s *Store) ReleaseExecution(taskID, detail string) (*model.Execution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.executionLocked(taskID)
	if err != nil || current == nil {
		return nil, err
	}
	current.Status, current.Detail = "released", detail
	stmt := executionStatement(*current)
	if _, err := s.db.Execute(context.Background(), stmt.SQL, stmt.Args...); err != nil {
		return nil, err
	}
	if err := s.emitExecutionUpdatedLocked(taskID); err != nil {
		return nil, err
	}
	return current, nil
}

// emitExecutionUpdatedLocked publishes the task snapshot that now contains the
// persisted execution. Execution state is not part of the task version, so
// clients receive the existing task.updated event with the current version.
// The caller holds s.mu to keep the event sequence and snapshot consistent.
func (s *Store) emitExecutionUpdatedLocked(taskID string) error {
	t, err := s.taskLocked(context.Background(), taskID)
	if err != nil {
		return err
	}
	emit(s, model.Event{
		Type: model.EventTaskUpdated, ProjectID: t.ProjectID,
		ID: t.ID, Version: t.Version, Task: &t,
	})
	return nil
}

func (s *Store) ActiveExecutions() ([]model.Execution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(context.Background(), `SELECT task_id, session_id, status, detail, runtime, followup_id, previous_turn_id FROM executions WHERE status NOT IN ('succeeded','returned','failed','released')`)
	if err != nil {
		return nil, err
	}
	var out []model.Execution
	for _, r := range rows.Values {
		out = append(out, *executionRow(r))
	}
	return out, nil
}

// ReturnedHerdrExecutions lists Herdr sessions whose last persisted status
// may have been observed during the terminal's startup idle window.
func (s *Store) ReturnedHerdrExecutions() ([]model.Execution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(context.Background(), `SELECT task_id, session_id, status, detail, runtime, followup_id, previous_turn_id FROM executions WHERE session_id LIKE 'herdr:%' AND status='returned'`)
	if err != nil {
		return nil, err
	}
	var out []model.Execution
	for _, r := range rows.Values {
		out = append(out, *executionRow(r))
	}
	return out, nil
}

func executionRow(r []any) *model.Execution {
	return &model.Execution{TaskID: str(r[0]), SessionID: str(r[1]), Status: str(r[2]), Detail: str(r[3]), Runtime: str(r[4]), FollowupID: str(r[5]), PreviousTurnID: str(r[6])}
}

func executionStatement(e model.Execution) sqlite.Statement {
	return sqlite.Statement{SQL: `INSERT INTO executions (task_id,session_id,status,detail,runtime,followup_id,previous_turn_id) VALUES (?,?,?,?,?,?,?) ON CONFLICT(task_id) DO UPDATE SET session_id=excluded.session_id,status=excluded.status,detail=excluded.detail,runtime=excluded.runtime,followup_id=excluded.followup_id,previous_turn_id=excluded.previous_turn_id`, Args: []any{e.TaskID, e.SessionID, e.Status, e.Detail, e.Runtime, e.FollowupID, e.PreviousTurnID}}
}

// ReserveFollowup persists both identity and receipt before an external write.
func (s *Store) ReserveFollowup(e model.Execution) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	current, err := s.executionLocked(e.TaskID)
	if err != nil {
		return false, err
	}
	rows, err := s.db.Query(ctx, `SELECT status FROM followups WHERE comment_id=? AND task_id=?`, e.FollowupID, e.TaskID)
	if err != nil {
		return false, err
	}
	if len(rows.Values) > 0 && str(rows.Values[0][0]) != "failed" {
		return false, nil
	}
	_, err = s.db.Transaction(ctx, []sqlite.Statement{
		{SQL: `INSERT INTO followups(comment_id,task_id,status) VALUES (?,?,'reserved') ON CONFLICT(comment_id) DO UPDATE SET status='reserved'`, Args: []any{e.FollowupID, e.TaskID}},
		executionStatement(e),
	})
	if err != nil {
		return false, err
	}
	if current == nil || *current != e {
		if err := s.emitExecutionUpdatedLocked(e.TaskID); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (s *Store) FollowupStatus(taskID, commentID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(context.Background(), `SELECT status FROM followups WHERE task_id=? AND comment_id=?`, taskID, commentID)
	if err != nil {
		return "", err
	}
	if len(rows.Values) == 0 {
		return "", nil
	}
	return str(rows.Values[0][0]), nil
}

func (s *Store) SetFollowupStatus(commentID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Execute(context.Background(), `UPDATE followups SET status=? WHERE comment_id=?`, status, commentID)
	return err
}
