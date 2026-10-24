package store

import (
	"errors"
	"path/filepath"
	"testing"

	"taskboard-native/internal/model"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "taskboard.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	})
	return s
}

func TestSeed(t *testing.T) {
	s := openTemp(t)

	projects, err := s.Projects()
	if err != nil {
		t.Fatalf("projects: %v", err)
	}
	if len(projects) != 1 || projects[0].ID != "local" {
		t.Fatalf("projects = %+v, want one local project", projects)
	}
	names := []string{}
	for _, c := range projects[0].Columns {
		names = append(names, c.Name)
	}
	want := []string{"待办", "进行中", "评审", "完成"}
	if len(names) != len(want) {
		t.Fatalf("columns = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("columns = %v, want %v", names, want)
		}
	}

	tasks, err := s.TasksByProject("local", false)
	if err != nil {
		t.Fatalf("tasks: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("seeded tasks = %d, want 3", len(tasks))
	}
	for _, task := range tasks {
		if task.Version != 1 || task.Archived {
			t.Fatalf("seeded task %s = %+v, want v1 live", task.ID, task)
		}
	}
}

func TestSeedIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "taskboard.db")
	first, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	firstTasks, _ := first.TasksByProject("local", false)
	if err := first.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	second, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer second.Close()
	secondTasks, _ := second.TasksByProject("local", false)
	if len(firstTasks) != len(secondTasks) {
		t.Fatalf("reopen changed task count: %d -> %d", len(firstTasks), len(secondTasks))
	}
	projects, _ := second.Projects()
	if len(projects) != 1 {
		t.Fatalf("reopen seeded another project: %d", len(projects))
	}
}

func TestCreateTaskAndComments(t *testing.T) {
	s := openTemp(t)
	var events []model.Event
	s.OnEvent = func(ev model.Event) { events = append(events, ev) }

	board, err := s.Board("local")
	if err != nil {
		t.Fatalf("board: %v", err)
	}
	column := board.Columns[1].ID

	task, err := s.CreateTask("local", column, "写测试", "细节", "linus", "cli")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if task.ID == "" || len(task.ID) != 32 {
		t.Fatalf("id = %q, want 32 hex chars", task.ID)
	}
	if task.ColumnID != column || task.Version != 1 || task.UpdatedBy != "cli" {
		t.Fatalf("created task = %+v", task)
	}
	if len(events) != 1 || events[0].Type != model.EventTaskCreated || events[0].By != "cli" {
		t.Fatalf("events = %+v, want one task.created by cli", events)
	}

	comment, err := s.AddComment(task.ID, "第一行", "mcp")
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	if comment.Author != "mcp" || comment.Text != "第一行" {
		t.Fatalf("comment = %+v", comment)
	}
	if events[len(events)-1].Type != model.EventCommentAdded {
		t.Fatalf("event type = %s, want comment.added", events[len(events)-1].Type)
	}

	got, err := s.Task(task.ID)
	if err != nil {
		t.Fatalf("task: %v", err)
	}
	if len(got.Comments) != 1 || got.Comments[0].ID != comment.ID {
		t.Fatalf("task comments = %+v", got.Comments)
	}
	// A comment must not bump the task version.
	if got.Version != 1 {
		t.Fatalf("version after comment = %d, want 1", got.Version)
	}
}

func TestProjectScopedQueries(t *testing.T) {
	s := openTemp(t)
	board, err := s.Board("local")
	if err != nil {
		t.Fatalf("board: %v", err)
	}
	other := board.Columns[0].ID
	if _, err := s.CreateTask("local", other, "本地任务", "", "", "local"); err != nil {
		t.Fatalf("create: %v", err)
	}

	// A second project is not creatable through the API, so simulate one by
	// inserting rows directly: scoping must still hold.
	if _, err := s.db.Execute(t.Context(),
		`INSERT INTO projects (id, name, position, created_at, updated_at) VALUES ('other', 'other', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := s.db.Execute(t.Context(),
		`INSERT INTO "columns" (id, project_id, name, position) VALUES ('othercol', 'other', '待办', 1000)`); err != nil {
		t.Fatalf("insert column: %v", err)
	}
	foreign, err := s.CreateTask("other", "othercol", "别处任务", "", "", "local")
	if err != nil {
		t.Fatalf("create foreign: %v", err)
	}

	local, err := s.TasksByProject("local", false)
	if err != nil {
		t.Fatalf("local tasks: %v", err)
	}
	for _, task := range local {
		if task.ProjectID != "local" {
			t.Fatalf("local scope leaked task from %s", task.ProjectID)
		}
	}
	scoped, err := s.TasksByProject("other", false)
	if err != nil {
		t.Fatalf("other tasks: %v", err)
	}
	if len(scoped) != 1 || scoped[0].ID != foreign.ID {
		t.Fatalf("other scope = %+v, want only the foreign task", scoped)
	}

	if _, err := s.TasksByProject("missing", false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing project err = %v, want ErrNotFound", err)
	}
	if _, err := s.CreateTask("missing", other, "x", "", "", "local"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("create into missing project err = %v, want ErrNotFound", err)
	}
}

func TestVersionConflict(t *testing.T) {
	s := openTemp(t)
	task, err := s.CreateTask("local", "", "冲突", "", "", "a")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	title := "改一次"
	updated, err := s.PatchTask(task.ID, task.Version, TaskPatch{Title: &title}, "a")
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if updated.Version != 2 {
		t.Fatalf("version = %d, want 2", updated.Version)
	}

	_, err = s.PatchTask(task.ID, 1, TaskPatch{Title: &title}, "b")
	var conflict *VersionError
	if !errors.As(err, &conflict) {
		t.Fatalf("stale patch err = %v, want VersionError", err)
	}
	if conflict.Current.Version != 2 || conflict.Current.ID != task.ID {
		t.Fatalf("conflict carries %+v, want current task v2", conflict.Current)
	}

	_, err = s.ArchiveTask(task.ID, 1, "b")
	if !errors.As(err, &conflict) {
		t.Fatalf("stale archive err = %v, want VersionError", err)
	}
	_, err = s.MoveTask(task.ID, 1, "", "", "b")
	if !errors.As(err, &conflict) {
		t.Fatalf("stale move err = %v, want VersionError", err)
	}
}

func TestMoveEventAndCompaction(t *testing.T) {
	s := openTemp(t)
	var events []model.Event
	s.OnEvent = func(ev model.Event) { events = append(events, ev) }

	board, err := s.Board("local")
	if err != nil {
		t.Fatalf("board: %v", err)
	}
	todo, doing := board.Columns[0].ID, board.Columns[1].ID
	a, err := s.CreateTask("local", todo, "A", "", "", "cli")
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	b, err := s.CreateTask("local", todo, "B", "", "", "cli")
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	c, err := s.CreateTask("local", todo, "C", "", "", "cli")
	if err != nil {
		t.Fatalf("create c: %v", err)
	}

	events = nil
	moved, err := s.MoveTask(b.ID, b.Version, doing, "", "mcp")
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if moved.ColumnID != doing || moved.Version != b.Version+1 {
		t.Fatalf("moved = %+v", moved)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	ev := events[0]
	if ev.Type != model.EventTaskMoved || ev.Column == nil || ev.Column.FromID != todo || ev.Column.ToID != doing {
		t.Fatalf("move event = %+v, want task.moved todo->doing", ev)
	}
	if ev.By != "mcp" || ev.Task == nil || ev.Task.ID != b.ID {
		t.Fatalf("move event identity = %+v", ev)
	}

	// Positions in the source column are compacted to 1000, 2000.
	todoTasks, err := s.TasksByProject("local", false)
	if err != nil {
		t.Fatalf("tasks: %v", err)
	}
	// The seeded card keeps the top slot; A and C follow, compacted.
	var order []string
	var positions []float64
	for _, task := range todoTasks {
		if task.ColumnID != todo {
			continue
		}
		order = append(order, task.Title)
		positions = append(positions, task.Position)
	}
	if len(order) != 3 || order[1] != "A" || order[2] != "C" {
		t.Fatalf("todo order = %v, want seeded card then [A C]", order)
	}
	for i, pos := range positions {
		if want := float64((i + 1) * 1000); pos != want {
			t.Fatalf("positions = %v, want compacted 1000/2000/3000", positions)
		}
	}
	_ = a

	// atTop puts the card first; afterID puts it right after a card.
	toTop, err := s.MoveTaskToTop(c.ID, c.Version, todo, "cli")
	if err != nil {
		t.Fatalf("move to top: %v", err)
	}
	if toTop.Position != 1000 {
		t.Fatalf("top position = %v, want 1000", toTop.Position)
	}
	afterA, err := s.PatchTask(c.ID, toTop.Version, TaskPatch{ColumnID: &todo, AfterID: a.ID}, "cli")
	if err != nil {
		t.Fatalf("move after: %v", err)
	}
	// After the at-top move the column is [C, seeded, A], so landing after A
	// is the last slot.
	if afterA.Position != 3000 {
		t.Fatalf("after-position = %v, want 3000", afterA.Position)
	}
	listed, err := s.TasksByProject("local", false)
	if err != nil {
		t.Fatalf("tasks: %v", err)
	}
	var sequence []string
	for _, task := range listed {
		if task.ColumnID == todo {
			sequence = append(sequence, task.Title)
		}
	}
	if len(sequence) != 3 || sequence[1] != "A" || sequence[2] != "C" {
		t.Fatalf("sequence after move = %v, want seeded/A/C", sequence)
	}
}

func TestArchiveHidesFromBoard(t *testing.T) {
	s := openTemp(t)
	var events []model.Event
	s.OnEvent = func(ev model.Event) { events = append(events, ev) }

	tasks, err := s.TasksByProject("local", false)
	if err != nil {
		t.Fatalf("tasks: %v", err)
	}
	target := tasks[0]
	archived, err := s.ArchiveTask(target.ID, target.Version, "cli")
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if !archived.Archived || archived.Version != target.Version+1 {
		t.Fatalf("archived = %+v", archived)
	}
	if events[len(events)-1].Type != model.EventTaskArchived {
		t.Fatalf("event = %s, want task.archived", events[len(events)-1].Type)
	}

	board, err := s.Board("local")
	if err != nil {
		t.Fatalf("board: %v", err)
	}
	for _, task := range board.Tasks {
		if task.ID == target.ID {
			t.Fatalf("archived task still on board")
		}
	}
	withArchived, err := s.TasksByProject("local", true)
	if err != nil {
		t.Fatalf("tasks incl archived: %v", err)
	}
	found := false
	for _, task := range withArchived {
		if task.ID == target.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("archived task missing from includeArchived listing")
	}

	// Archived cards cannot be patched or archived twice.
	title := "x"
	if _, err := s.PatchTask(target.ID, archived.Version, TaskPatch{Title: &title}, "cli"); err == nil {
		t.Fatalf("patch archived task succeeded")
	}
	if _, err := s.ArchiveTask(target.ID, archived.Version, "cli"); err == nil {
		t.Fatalf("double archive succeeded")
	}

	// archived:false restores the card to the bottom of its column.
	restore := false
	restored, err := s.PatchTask(target.ID, archived.Version, TaskPatch{Archived: &restore}, "gui")
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.Archived || restored.Version != archived.Version+1 || restored.UpdatedBy != "gui" {
		t.Fatalf("restored = %+v", restored)
	}
	if ev := events[len(events)-1]; ev.Type != model.EventTaskUpdated || ev.Task == nil || ev.Task.Archived {
		t.Fatalf("restore event = %+v", ev)
	}
	board, err = s.Board("local")
	if err != nil {
		t.Fatalf("board after restore: %v", err)
	}
	var column []model.Task
	for _, task := range board.Tasks {
		if task.ColumnID == restored.ColumnID {
			column = append(column, task)
		}
	}
	last := column[len(column)-1]
	if last.ID != target.ID || last.Position != float64(len(column)*1000) {
		t.Fatalf("restored card sits at %v of %d, want bottom %d", last.Position, len(column), len(column)*1000)
	}

	// archived:false on a live card changes nothing but the version.
	noop := false
	unchanged, err := s.PatchTask(target.ID, restored.Version, TaskPatch{Archived: &noop}, "gui")
	if err != nil {
		t.Fatalf("archived:false on live card: %v", err)
	}
	if unchanged.Position != restored.Position || unchanged.Version != restored.Version+1 {
		t.Fatalf("no-op restore moved the card: %+v (was %+v)", unchanged, restored)
	}

	// archived:true goes through the same patch path and compacts the column.
	archiveAgain := true
	reArchived, err := s.PatchTask(target.ID, unchanged.Version, TaskPatch{Archived: &archiveAgain}, "cli")
	if err != nil {
		t.Fatalf("archive via patch: %v", err)
	}
	if !reArchived.Archived || reArchived.Version != unchanged.Version+1 {
		t.Fatalf("re-archived = %+v", reArchived)
	}
	if ev := events[len(events)-1]; ev.Type != model.EventTaskArchived {
		t.Fatalf("archive-via-patch event = %s, want task.archived", ev.Type)
	}
	board, err = s.Board("local")
	if err != nil {
		t.Fatalf("board: %v", err)
	}
	for _, task := range board.Tasks {
		if task.ID == target.ID {
			t.Fatalf("re-archived card still on board")
		}
	}
}

func TestInvalidInputs(t *testing.T) {
	s := openTemp(t)
	if _, err := s.CreateTask("local", "", "", "", "", "cli"); err == nil {
		t.Fatalf("empty title accepted")
	} else {
		var invalid *InvalidError
		if !errors.As(err, &invalid) {
			t.Fatalf("empty title err = %v, want InvalidError", err)
		}
	}
	if _, err := s.CreateTask("local", "nope", "x", "", "", "cli"); err == nil {
		t.Fatalf("unknown column accepted")
	}
	if _, err := s.AddComment("missing", "x", "cli"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("comment on missing task err = %v", err)
	}
	if _, err := s.Task("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing task err = %v", err)
	}
}

func TestCreateProjectAndOrdering(t *testing.T) {
	s := openTemp(t)
	var events []model.Event
	s.OnEvent = func(ev model.Event) { events = append(events, ev) }

	created, err := s.CreateProject("工作台", "gui")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if created.Name != "工作台" || len(created.Columns) != 4 {
		t.Fatalf("created project = %+v", created)
	}
	if created.Columns[0].Name != "待办" || created.Columns[3].Name != "完成" {
		t.Fatalf("columns = %+v", created.Columns)
	}
	if len(events) != 1 || events[0].Type != model.EventProjectChange || events[0].ProjectID != created.ID || events[0].By != "gui" {
		t.Fatalf("events = %+v", events)
	}

	second, err := s.CreateProject("第二个", "cli")
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	projects, err := s.Projects()
	if err != nil {
		t.Fatalf("projects: %v", err)
	}
	var ids []string
	for _, p := range projects {
		ids = append(ids, p.ID)
	}
	if len(ids) != 3 || ids[0] != "local" || ids[1] != created.ID || ids[2] != second.ID {
		t.Fatalf("project order = %v, want creation order (position)", ids)
	}

	// The new project is isolated: it starts with no cards.
	tasks, err := s.TasksByProject(created.ID, false)
	if err != nil {
		t.Fatalf("tasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("new project tasks = %d, want 0", len(tasks))
	}
	if _, err := s.CreateTask(created.ID, created.Columns[2].ID, "新项目的卡", "", "", "cli"); err != nil {
		t.Fatalf("create task in new project: %v", err)
	}
	local, _ := s.TasksByProject("local", false)
	for _, task := range local {
		if task.ProjectID != "local" {
			t.Fatalf("local scope leaked %+v", task)
		}
	}

	if _, err := s.CreateProject("  ", "cli"); err == nil {
		t.Fatalf("blank project name accepted")
	}
}
