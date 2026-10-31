package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"taskboard-native/internal/client"
	"taskboard-native/internal/executor"
	"taskboard-native/internal/model"
	"taskboard-native/internal/store"
)

// fakeBoard is an in-memory implementation of api for the render tests.
// It mirrors the service's wire behaviour: writes bump versions, return
// conflicts when the version is stale, and push events to subscribers.
type fakeBoard struct {
	mu                  sync.Mutex
	projects            []model.Project
	tasks               []model.Task
	seq                 int64
	delay               time.Duration // injected latency, standing in for a slow service
	fail                error         // returned by every read when set
	createFail          error         // returned by CreateTask when set
	projectFail         error         // returned by project create/update when set
	commentFail         error         // returned by AddComment* when set
	subs                []func(model.Event)
	now                 time.Time
	dispatches          [][2]string // recorded Dispatch(taskID, runtime) calls
	createAndDispatches [][2]string // recorded atomic create-and-dispatch calls
	projectRuns         [][2]string // recorded StartProjectRun(projectID, runtime) calls
}

// seedBoard is the demo board the screenshots use: four columns, five
// cards, one comment, one agent-authored card.
func seedBoard() *fakeBoard {
	now := time.Date(2026, 10, 24, 9, 30, 0, 0, time.UTC)
	cols := []model.Column{
		{ID: "c-todo", Name: "待办"},
		{ID: "c-doing", Name: "进行中"},
		{ID: "c-review", Name: "评审"},
		{ID: "c-done", Name: "完成"},
	}
	f := &fakeBoard{
		projects: []model.Project{{ID: "local", Name: "local", Columns: cols, CreatedAt: now}},
		now:      now,
	}
	f.add(model.Task{
		ID: "t-1", ProjectID: "local", ColumnID: "c-todo", Title: "把看板接上 SSE",
		Description: "事件到达后就地更新卡片，不轮询、不整表重建。",
		Position:    1000, Version: 1, CreatedAt: now, UpdatedAt: now,
		Comments: []model.Comment{{
			ID: "m-1", Author: "linus", Text: "已加 atTop，拖到列顶也能落位。",
			CreatedAt: now.Add(30 * time.Minute),
		}},
	})
	f.add(model.Task{
		ID: "t-2", ProjectID: "local", ColumnID: "c-todo", Title: "对齐设计规格的间距与字号",
		Description: "间距走 t.Space，字号走 f-* 刻度。",
		Position:    2000, Version: 2, CreatedAt: now, UpdatedAt: now,
	})
	f.add(model.Task{
		ID: "t-3", ProjectID: "local", ColumnID: "c-doing", Title: "原生窗口渲染验收",
		Description: "1280×800 下四列等分，明暗两种外观都要出图。",
		Position:    1000, Version: 3, CreatedAt: now, UpdatedAt: now,
		Assignee: "ive", UpdatedBy: "ive",
	})
	f.add(model.Task{
		ID: "t-4", ProjectID: "local", ColumnID: "c-doing", Title: "CLI 与 MCP 写入立刻反映到窗口",
		Description: "写入方把身份带进事件，卡片显示代理徽标。",
		Position:    2000, Version: 1, CreatedAt: now, UpdatedAt: now,
		UpdatedBy: "mcp",
	})
	f.add(model.Task{
		ID: "t-5", ProjectID: "local", ColumnID: "c-review", Title: "版本冲突时拉一次详情对齐",
		Description: "409 之后用服务端版本重发，不覆盖别人的修改。",
		Position:    1000, Version: 1, CreatedAt: now, UpdatedAt: now,
	})
	// The seeded cards already hold t-1 through t-5, so the counter has to
	// start past them. Otherwise a created task takes an ID that is already
	// in use and silently overwrites a seeded card.
	f.seq = 5
	return f
}

func (f *fakeBoard) add(t model.Task) {
	f.tasks = append(f.tasks, t)
}

func (f *fakeBoard) wait() {
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
}

func (f *fakeBoard) Projects() ([]model.Project, error) {
	f.wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	out := make([]model.Project, len(f.projects))
	copy(out, f.projects)
	return out, nil
}

func (f *fakeBoard) Board(projectID string) (client.Board, error) {
	f.wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return client.Board{}, f.fail
	}
	var p model.Project
	for _, cand := range f.projects {
		if cand.ID == projectID {
			p = cand
		}
	}
	if p.ID == "" {
		return client.Board{}, fmt.Errorf("项目不存在")
	}
	b := client.Board{Project: p, Columns: p.Columns}
	for _, t := range f.tasks {
		if t.ProjectID == projectID && !t.Archived {
			b.Tasks = append(b.Tasks, t)
		}
	}
	return b, nil
}

func (f *fakeBoard) Task(id string) (model.Task, error) {
	f.wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.tasks {
		if t.ID == id {
			return t, nil
		}
	}
	return model.Task{}, fmt.Errorf("任务不存在")
}


func (f *fakeBoard) AddRelation(id string, version int, kind, relatedID string) (model.RelationResult, error) {
	task, err := f.Task(id)
	if err != nil {
		return model.RelationResult{}, err
	}
	related, err := f.Task(relatedID)
	if err != nil {
		return model.RelationResult{}, err
	}
	task.Version++
	if kind == "parent" {
		task.Relations.Parent = &model.TaskRelationSummary{ID: related.ID, Number: related.Number, Title: related.Title, ColumnID: related.ColumnID}
	}
	if kind == "blocked_by" {
		task.Relations.BlockedBy = append(task.Relations.BlockedBy, model.TaskRelationSummary{ID: related.ID, Number: related.Number, Title: related.Title, ColumnID: related.ColumnID})
	}
	return model.RelationResult{Task: task, RelatedTask: related}, nil
}

func (f *fakeBoard) RemoveRelation(id string, version int, kind, relatedID string) (model.RelationResult, error) {
	task, err := f.Task(id)
	if err != nil {
		return model.RelationResult{}, err
	}
	related, err := f.Task(relatedID)
	if err != nil {
		return model.RelationResult{}, err
	}
	task.Version++
	if kind == "parent" {
		task.Relations.Parent = nil
	}
	if kind == "blocked_by" {
		task.Relations.BlockedBy = nil
	}
	return model.RelationResult{Task: task, RelatedTask: related}, nil
}

func (f *fakeBoard) CreateTask(projectID, columnID, title, description, assignee string) (model.Task, error) {
	f.wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createFail != nil {
		return model.Task{}, f.createFail
	}
	var p model.Project
	for _, cand := range f.projects {
		if cand.ID == projectID {
			p = cand
		}
	}
	if len(p.Columns) == 0 {
		return model.Task{}, fmt.Errorf("项目不存在")
	}
	if columnID == "" {
		columnID = p.Columns[0].ID
	}
	t := model.Task{
		ID: nextID("t", f.seq), ProjectID: projectID, ColumnID: columnID,
		Title: title, Description: description, Assignee: assignee,
		Position: float64(len(f.tasks)+1) * 1000, Version: 1,
		CreatedAt: f.now, UpdatedAt: f.now, UpdatedBy: "gui",
	}
	f.tasks = append(f.tasks, t)
	f.emitLocked(model.Event{Type: model.EventTaskCreated, ProjectID: projectID, ID: t.ID, Version: 1, By: "gui", Task: &t})
	return t, nil
}

func (f *fakeBoard) CreateTaskAndDispatch(projectID, columnID, title, description, assignee, runtime string) (model.Task, error) {
	t, err := f.CreateTask(projectID, columnID, title, description, assignee)
	if err != nil || runtime == "" {
		return t, err
	}
	f.mu.Lock()
	f.createAndDispatches = append(f.createAndDispatches, [2]string{t.ID, runtime})
	f.mu.Unlock()
	return t, nil
}

func (f *fakeBoard) Update(id string, version int, title, description, assignee *string) (model.Task, error) {
	f.wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.tasks {
		t := &f.tasks[i]
		if t.ID != id {
			continue
		}
		if version != 0 && version != t.Version {
			return *t, &client.ConflictError{Current: *t}
		}
		if title != nil {
			t.Title = *title
		}
		if description != nil {
			t.Description = *description
		}
		if assignee != nil {
			t.Assignee = *assignee
		}
		t.Version++
		t.UpdatedAt = f.now
		t.UpdatedBy = "gui"
		cp := *t
		f.emitLocked(model.Event{Type: model.EventTaskUpdated, ProjectID: t.ProjectID, ID: t.ID, Version: t.Version, By: "gui", Task: &cp})
		return cp, nil
	}
	return model.Task{}, fmt.Errorf("任务不存在")
}

func (f *fakeBoard) Move(id string, version int, toColumn, afterID string, atTop bool) (model.Task, error) {
	f.wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.tasks {
		t := &f.tasks[i]
		if t.ID != id {
			continue
		}
		if version != 0 && version != t.Version {
			return *t, &client.ConflictError{Current: *t}
		}
		from := t.ColumnID
		t.ColumnID = toColumn
		if atTop {
			t.Position = 0.5
		} else {
			t.Position = float64(len(f.tasks)+1) * 1000
		}
		t.Version++
		t.UpdatedAt = f.now
		t.UpdatedBy = "gui"
		cp := *t
		ev := model.Event{Type: model.EventTaskUpdated, ProjectID: t.ProjectID, ID: t.ID, Version: t.Version, By: "gui", Task: &cp}
		if from != toColumn {
			ev.Type = model.EventTaskMoved
			ev.Column = &struct {
				FromID string `json:"fromId"`
				ToID   string `json:"toId"`
			}{FromID: from, ToID: toColumn}
		}
		f.emitLocked(ev)
		return cp, nil
	}
	return model.Task{}, fmt.Errorf("任务不存在")
}

func (f *fakeBoard) Archive(id string, version int) (model.Task, error) {
	f.wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.tasks {
		t := &f.tasks[i]
		if t.ID != id {
			continue
		}
		if version != 0 && version != t.Version {
			return *t, &client.ConflictError{Current: *t}
		}
		t.Archived = true
		t.Version++
		t.UpdatedAt = f.now
		cp := *t
		f.emitLocked(model.Event{Type: model.EventTaskArchived, ProjectID: t.ProjectID, ID: t.ID, Version: t.Version, By: "gui", Task: &cp})
		return cp, nil
	}
	return model.Task{}, fmt.Errorf("任务不存在")
}

func (f *fakeBoard) Restore(id string, version int) (model.Task, error) {
	f.wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.tasks {
		t := &f.tasks[i]
		if t.ID != id {
			continue
		}
		if version != 0 && version != t.Version {
			return *t, &client.ConflictError{Current: *t}
		}
		t.Archived = false
		t.Version++
		t.UpdatedAt = f.now
		t.UpdatedBy = "gui"
		cp := *t
		f.emitLocked(model.Event{Type: model.EventTaskUpdated, ProjectID: t.ProjectID, ID: t.ID, Version: t.Version, By: "gui", Task: &cp})
		return cp, nil
	}
	return model.Task{}, fmt.Errorf("任务不存在")
}

func (f *fakeBoard) Comments(taskID string) ([]model.Comment, error) {
	f.wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.tasks {
		if t.ID == taskID {
			out := make([]model.Comment, len(t.Comments))
			copy(out, t.Comments)
			return out, nil
		}
	}
	return nil, fmt.Errorf("任务不存在")
}

func (f *fakeBoard) HandoffLog(string) (executor.HandoffLogView, error) {
	return executor.HandoffLogView{}, nil
}

func (f *fakeBoard) AddComment(taskID, text string) (model.Comment, error) {
	return f.AddCommentTo(taskID, text, "")
}

func (f *fakeBoard) AddCommentTo(taskID, text, mention string) (model.Comment, error) {
	return f.AddCommentWithParent(taskID, text, mention, "")
}

func (f *fakeBoard) AddCommentWithParent(taskID, text, mention, parentCommentID string) (model.Comment, error) {
	f.wait()
	if f.commentFail != nil {
		return model.Comment{}, f.commentFail
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.tasks {
		t := &f.tasks[i]
		if t.ID != taskID {
			continue
		}
		cm := model.Comment{ID: nextID("m", f.seq), Author: "gui", Text: text, Mention: mention, ParentCommentID: parentCommentID, CreatedAt: f.now}
		t.Comments = append(t.Comments, cm)
		f.emitLocked(model.Event{Type: model.EventCommentAdded, ProjectID: t.ProjectID, ID: t.ID, Version: t.Version, By: "gui", Comment: &cm})
		return cm, nil
	}
	return model.Comment{}, fmt.Errorf("任务不存在")
}

func (f *fakeBoard) UpdateComment(taskID, commentID, text string) (model.Comment, error) {
	f.wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.tasks {
		if f.tasks[i].ID != taskID {
			continue
		}
		for j := range f.tasks[i].Comments {
			cm := &f.tasks[i].Comments[j]
			if cm.ID == commentID {
				if cm.Author != "gui" {
					return model.Comment{}, fmt.Errorf("comment author mismatch")
				}
				cm.Text = text
				updated := *cm
				f.emitLocked(model.Event{Type: model.EventCommentUpdated, ProjectID: f.tasks[i].ProjectID, ID: taskID, By: "gui", Comment: &updated})
				return updated, nil
			}
		}
	}
	return model.Comment{}, fmt.Errorf("评论不存在")
}

func (f *fakeBoard) DeleteComment(taskID, commentID string) error {
	f.wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.tasks {
		if f.tasks[i].ID != taskID {
			continue
		}
		for j := range f.tasks[i].Comments {
			cm := f.tasks[i].Comments[j]
			if cm.ID == commentID {
				if cm.Author != "gui" {
					return fmt.Errorf("comment author mismatch")
				}
				f.tasks[i].Comments = append(f.tasks[i].Comments[:j], f.tasks[i].Comments[j+1:]...)
				f.emitLocked(model.Event{Type: model.EventCommentDeleted, ProjectID: f.tasks[i].ProjectID, ID: taskID, By: "gui", Comment: &cm})
				return nil
			}
		}
	}
	return fmt.Errorf("评论不存在")
}

func (f *fakeBoard) CreateProject(name string) (model.Project, error) {
	f.wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.projectFail != nil {
		return model.Project{}, f.projectFail
	}
	p := model.Project{ID: nextID("p", f.seq), Name: name, CreatedAt: f.now,
		Columns: []model.Column{{ID: nextID("c", f.seq), Name: "待办"}}}
	f.projects = append(f.projects, p)
	return p, nil
}

// Subscribe registers for pushed events until ctx ends, reporting Online
// at once.
func (f *fakeBoard) CreateProjectWithPath(name, workspacePath string) (model.Project, error) {
	return f.CreateProjectWithColor(name, workspacePath, "")
}

func (f *fakeBoard) CreateProjectWithColor(name, workspacePath, color string) (model.Project, error) {
	p, err := f.CreateProject(name)
	if err != nil {
		return p, err
	}
	p.WorkspacePath = workspacePath
	p.Color = color
	for i := range f.projects {
		if f.projects[i].ID == p.ID {
			f.projects[i].WorkspacePath = workspacePath
			f.projects[i].Color = color
			p = f.projects[i]
			break
		}
	}
	return p, nil
}

func (f *fakeBoard) StartProjectRun(projectID, runtime string) (model.ProjectRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.projectRuns = append(f.projectRuns, [2]string{projectID, runtime})
	return model.ProjectRun{ProjectID: projectID, Runtime: runtime, Status: "running"}, nil
}

func (f *fakeBoard) ProjectRun(projectID string) (model.ProjectRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, run := range f.projectRuns {
		if run[0] == projectID {
			return model.ProjectRun{ProjectID: projectID, Runtime: run[1], Status: "running"}, nil
		}
	}
	return model.ProjectRun{}, store.ErrNotFound
}

func (f *fakeBoard) StopProjectRun(projectID string) (model.ProjectRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.projectRuns {
		if f.projectRuns[i][0] == projectID {
			f.projectRuns[i][1] = "stopped"
		}
	}
	return model.ProjectRun{ProjectID: projectID, Status: "stopped"}, nil
}

// Dispatch records the request; tests assert on it via dispatches.
func (f *fakeBoard) Dispatch(taskID, runtime string) error {
	f.mu.Lock()
	f.dispatches = append(f.dispatches, [2]string{taskID, runtime})
	f.mu.Unlock()
	return nil
}

// UpdateProject edits the named project in place.
func (f *fakeBoard) UpdateProject(id, name, workspacePath string) (model.Project, error) {
	return f.UpdateProjectWithColor(id, name, workspacePath, "")
}

func (f *fakeBoard) UpdateProjectWithColor(id, name, workspacePath, color string) (model.Project, error) {
	f.wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.projectFail != nil {
		return model.Project{}, f.projectFail
	}
	for i := range f.projects {
		if f.projects[i].ID == id {
			f.projects[i].Name = name
			f.projects[i].WorkspacePath = workspacePath
			f.projects[i].Color = color
			return f.projects[i], nil
		}
	}
	return model.Project{}, store.ErrNotFound
}

// DeleteProject drops the named project from the fake's list; tasks are
// fake-local and not tracked per project here.
func (f *fakeBoard) DeleteProject(id string) error {
	f.wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.projectFail != nil {
		return f.projectFail
	}
	for i := range f.projects {
		if f.projects[i].ID == id {
			f.projects = append(f.projects[:i], f.projects[i+1:]...)
			return nil
		}
	}
	return store.ErrNotFound
}

func (f *fakeBoard) Subscribe(ctx context.Context, projectID string, onEvent func(model.Event), onState func(client.StreamState)) error {
	ch := make(chan model.Event, 32)
	f.mu.Lock()
	f.subs = append(f.subs, func(ev model.Event) {
		select {
		case ch <- ev:
		default:
		}
	})
	f.mu.Unlock()
	if onState != nil {
		onState(client.Online)
	}
	defer func() {
		if onState != nil {
			onState(client.Offline)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev := <-ch:
			if projectID != "" && ev.ProjectID != projectID {
				continue
			}
			onEvent(ev)
		}
	}
}

// emitLocked tells every subscriber about ev; the caller holds mu.
func (f *fakeBoard) emitLocked(ev model.Event) {
	f.seq++
	ev.Seq = f.seq
	for _, sub := range f.subs {
		sub(ev)
	}
}

// external writes t into the fake's store as another agent would, then
// pushes the event.
func (f *fakeBoard) external(t model.Task) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.tasks {
		if f.tasks[i].ID == t.ID {
			f.tasks[i] = t
		}
	}
	f.emitLocked(model.Event{Type: model.EventTaskUpdated, ProjectID: t.ProjectID, ID: t.ID, Version: t.Version, By: t.UpdatedBy, Task: &t})
}

// push sends an event as if another agent wrote it, for tests of in-place
// updates.
func (f *fakeBoard) push(ev model.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.emitLocked(ev)
}

func nextID(prefix string, n int64) string {
	return fmt.Sprintf("%s-%d", prefix, n+1)
}
