package service

import (
	"net/http"
	"testing"

	"taskboard-native/internal/model"
)

// A comment can address one execution channel. The address is a stored field,
// so an @ written inside prose can never be mistaken for an instruction.
func TestCommentMentionIsStoredAndValidated(t *testing.T) {
	srv := start(t)
	projectID := fixtureProjectID(t, srv)
	status, raw := request(t, srv, http.MethodPost, "/api/tasks", "human", map[string]any{
		"projectId": projectID, "title": "评论指定渠道", "columnId": "",
	})
	if status != http.StatusCreated {
		t.Fatalf("create task = %d %s", status, raw)
	}
	task := decode[model.Task](t, raw)

	status, raw = request(t, srv, http.MethodPost, "/api/tasks/"+task.ID+"/comments", "human", map[string]any{
		"text": "请复核这段改动", "mention": "claude",
	})
	if status != http.StatusCreated {
		t.Fatalf("add comment = %d %s", status, raw)
	}
	comment := decode[model.Comment](t, raw)
	if comment.Mention != "claude" {
		t.Fatalf("mention not stored: %+v", comment)
	}
	if comment.Text != "请复核这段改动" {
		t.Fatalf("mention leaked into the text: %q", comment.Text)
	}

	// It survives a re-read, which is what the dispatcher routes on.
	status, raw = request(t, srv, http.MethodGet, "/api/tasks/"+task.ID, "", nil)
	if status != http.StatusOK {
		t.Fatalf("task = %d %s", status, raw)
	}
	stored := decode[model.Task](t, raw)
	if len(stored.Comments) != 1 || stored.Comments[0].Mention != "claude" {
		t.Fatalf("comments = %+v", stored.Comments)
	}

	status, raw = request(t, srv, http.MethodPost, "/api/tasks/"+task.ID+"/comments", "human", map[string]any{
		"text": "写给不存在的渠道", "mention": "nope",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("unknown mention = %d %s", status, raw)
	}

	// A plain comment keeps the old shape: no address at all.
	status, raw = request(t, srv, http.MethodPost, "/api/tasks/"+task.ID+"/comments", "human", map[string]any{
		"text": "普通评论",
	})
	if status != http.StatusCreated {
		t.Fatalf("plain comment = %d %s", status, raw)
	}
	if plain := decode[model.Comment](t, raw); plain.Mention != "" || plain.Text != "普通评论" {
		t.Fatalf("plain comment = %+v", plain)
	}
}

func TestMentionedReviewFeedbackReturnsCardToIntake(t *testing.T) {
	srv := start(t)
	projectID := fixtureProjectID(t, srv)
	project, err := srv.store.Project(projectID)
	if err != nil {
		t.Fatal(err)
	}
	review, ok := project.ColumnByRole(model.ColumnRoleReview)
	if !ok {
		t.Fatal("review column missing")
	}
	intake, ok := project.ColumnByRole(model.ColumnRoleIntake)
	if !ok {
		t.Fatal("intake column missing")
	}
	status, raw := request(t, srv, http.MethodPost, "/api/tasks", "human", map[string]any{
		"projectId": projectID, "title": "评审反馈回退",
	})
	if status != http.StatusCreated {
		t.Fatalf("create task = %d %s", status, raw)
	}
	task := decode[model.Task](t, raw)
	inReview, err := srv.store.MoveTask(task.ID, task.Version, review.ID, "", "human")
	if err != nil {
		t.Fatal(err)
	}

	status, raw = request(t, srv, http.MethodPost, "/api/tasks/"+task.ID+"/comments", "human", map[string]any{
		"text": "请按反馈修改", "mention": "claude",
	})
	if status != http.StatusCreated {
		t.Fatalf("add addressed review comment = %d %s", status, raw)
	}
	status, raw = request(t, srv, http.MethodGet, "/api/tasks/"+task.ID, "", nil)
	if status != http.StatusOK {
		t.Fatalf("get task = %d %s", status, raw)
	}
	updated := decode[model.Task](t, raw)
	if updated.ColumnID != intake.ID || updated.Version != inReview.Version+1 {
		t.Fatalf("task after addressed review feedback = column %q version %d, want intake %q version %d", updated.ColumnID, updated.Version, intake.ID, inReview.Version+1)
	}
	if len(updated.Comments) != 1 || updated.Comments[0].Mention != "claude" {
		t.Fatalf("saved feedback = %+v", updated.Comments)
	}
}

func TestPlainReviewCommentDoesNotReturnCardToIntake(t *testing.T) {
	srv := start(t)
	projectID := fixtureProjectID(t, srv)
	project, err := srv.store.Project(projectID)
	if err != nil {
		t.Fatal(err)
	}
	review, _ := project.ColumnByRole(model.ColumnRoleReview)
	status, raw := request(t, srv, http.MethodPost, "/api/tasks", "human", map[string]any{
		"projectId": projectID, "title": "评审讨论",
	})
	if status != http.StatusCreated {
		t.Fatalf("create task = %d %s", status, raw)
	}
	task := decode[model.Task](t, raw)
	inReview, err := srv.store.MoveTask(task.ID, task.Version, review.ID, "", "human")
	if err != nil {
		t.Fatal(err)
	}
	status, raw = request(t, srv, http.MethodPost, "/api/tasks/"+task.ID+"/comments", "human", map[string]any{
		"text": "记录一下评审讨论",
	})
	if status != http.StatusCreated {
		t.Fatalf("add plain review comment = %d %s", status, raw)
	}
	updated, err := srv.store.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ColumnID != review.ID || updated.Version != inReview.Version {
		t.Fatalf("plain comment moved task: column %q version %d", updated.ColumnID, updated.Version)
	}
}
