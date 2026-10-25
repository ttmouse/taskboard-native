// Package main is the taskboard's native window: a MyGo board whose only
// data path is the local HTTP service and its SSE stream.
//
// Every change the user makes is a request to the service; every change
// anyone makes arrives as an event and is applied in place, so the window
// never polls and never rebuilds the board wholesale.
package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"taskboard-native/internal/client"
	"taskboard-native/internal/model"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// api is the slice of the service the window uses. client.Client
// implements it; tests inject a fake.
type api interface {
	Projects() ([]model.Project, error)
	Board(projectID string) (client.Board, error)
	Task(id string) (model.Task, error)
	CreateTask(projectID, columnID, title, description, assignee string) (model.Task, error)
	Update(id string, version int, title, description, assignee *string) (model.Task, error)
	Move(id string, version int, toColumn, afterID string, atTop bool) (model.Task, error)
	Archive(id string, version int) (model.Task, error)
	Restore(id string, version int) (model.Task, error)
	Comments(taskID string) ([]model.Comment, error)
	AddComment(taskID, text string) (model.Comment, error)
	CreateProject(name string) (model.Project, error)
	Subscribe(ctx context.Context, projectID string, onEvent func(model.Event), onState func(client.StreamState)) error
}

// cardDrag is the value a dragged card carries between columns.
type cardDrag string

// curGlobal is the sidebar「全部项目」selection: tasks from every project.
const curGlobal = "__global__"

// cardMatch gives a card one identity across columns, so MyGo slides it
// from the source column into the target instead of exit plus enter.
type cardMatch struct{ ID string }

// detail is the open task detail, with the edits not yet saved.
type detail struct {
	open     bool
	id       string
	title    string
	body     string
	assignee string
	column   string // selected column name in the move select
	comment  string
	comments []model.Comment
	loading  bool // comments still arriving

	// snap is the task as it was opened; dirty() compares against it so a
	// pushed update cannot silently overwrite unsaved typing.
	snap   model.Task
	pushed *model.Task // an incoming update held back while dirty
	pushBy string
}

// dirty reports whether the dialog holds edits the user has not saved.
func (d *detail) dirty() bool {
	return strings.TrimSpace(d.title) != d.snap.Title || d.body != d.snap.Description
}

// stale reports whether an agent pushed a newer version while the dialog
// held unsaved edits, so saving must ask before overwriting either side.
func (d *detail) stale() bool { return d.dirty() && d.pushed != nil }

// form is a small dialog: a new task or a new project.
type form struct {
	open     bool
	isTask   bool // true → 新建任务；false → 新建项目
	name     string
	columnID string // task form only: target column
}

// settings is the small preferences sheet (one toggle for now).
type settings struct {
	open bool
}

// conflict shows the 409 the service answered, with the server's copy.
type conflict struct {
	open    bool
	id      string
	title   string
	message string
	retry   func(version int)
}

// board is the window's state. Only the view builds and the messages it
// drains touch it; background requests send messages instead.
type board struct {
	api api
	win *mygo.Window

	msgs   chan func()
	cancel context.CancelFunc

	projects []model.Project
	cur      string
	columns  []model.Column
	tasks    []model.Task

	status     client.StreamState
	loading    bool // first board in flight
	switching  bool // another project's board in flight
	saving     bool
	savingID   string // task whose text is being saved
	commenting bool
	archiving  bool
	loadErr    string
	pulseTill  time.Time

	detail   *detail
	form     *form
	conflict *conflict
	settings *settings // nil when the settings sheet is closed

	prefs uiPrefs // persisted window preferences (title-only cards, …)

	searchQuery string // top-bar filter: title + description, all columns

	// colScrolls tracks per-column (and board) scroll offsets so the
	// scrollbar theme can stay transparent until the user scrolls.
	colScrolls map[string]*colScroll

	frameCtx *ui.Context
	curTheme *ui.Theme // the theme the current frame builds with

	drafts map[string]string // retained for tests; column quick-add UI removed

	agent       string
	streaming   bool                 // the SSE reader is running
	recent      map[string]time.Time // task ID → until when it shows as changed
	bounds      map[string]ui.Rect
	motionState // accepted motion shared with the Playground
}

func newBoard(a api) *board {
	b := &board{
		api:    a,
		msgs:   make(chan func(), 64),
		drafts: map[string]string{},
		status: client.Connecting,
		agent:  "gui",
		recent: map[string]time.Time{},
		bounds: map[string]ui.Rect{},
		prefs:  loadUIPrefs(),
		// Launch into「全部项目」, not a concrete board like local.
		cur: curGlobal,
	}
	b.load()
	b.startStream()
	return b
}

// --- threading ---

// post hands fn to the view, which runs it on the main thread.
func (b *board) post(fn func()) {
	select {
	case b.msgs <- fn:
	default: // a full queue means a frame is long overdue; drop nothing important
		go func() { b.msgs <- fn }()
	}
	b.invalidate()
}

func (b *board) invalidate() {
	if b.win != nil {
		b.win.Invalidate()
	}
}

// pump runs what background work sent since the last frame.
func (b *board) pump() {
	for {
		select {
		case fn := <-b.msgs:
			fn()
		default:
			return
		}
	}
}

// --- loading ---

// load reads the projects, then the selected project's board.
func (b *board) load() {
	b.loading = len(b.tasks) == 0
	b.loadErr = ""
	go func() {
		ps, err := b.api.Projects()
		if err != nil {
			b.post(func() {
				b.loading = false
				b.loadErr = "无法读取项目：" + err.Error()
			})
			return
		}
		target := b.cur
		wantGlobal := target == curGlobal
		if target == "" {
			// Empty selection → launch/reload into「全部项目」.
			wantGlobal = true
			target = curGlobal
		} else if !wantGlobal && !hasProject(ps, target) {
			target = defaultProject(ps)
		}
		if target == "" && !wantGlobal {
			b.post(func() {
				b.projects, b.cur, b.columns, b.tasks = ps, "", nil, nil
				b.loading = false
			})
			return
		}
		if wantGlobal {
			b.post(func() {
				b.projects = ps
				b.loading = false
				b.switching = false
				b.cur = "" // force openGlobal to run
				b.openGlobal()
			})
			return
		}
		bd, err := b.api.Board(target)
		b.post(func() {
			b.projects = ps
			b.loading = false
			b.switching = false
			if err != nil {
				b.loadErr = "无法读取看板：" + err.Error()
				return
			}
			b.cur = target
			b.columns = bd.Columns
			b.tasks = bd.Tasks
			if len(b.columns) == 0 {
				b.columns = bd.Project.Columns
			}
			b.sortTasks()
		})
	}()
}


// openGlobal loads every project's board and merges cards by column name
// onto the first board's column set (usually local / primary).
func (b *board) openGlobal() {
	if b.cur == curGlobal {
		return
	}
	b.switching = true
	b.loadErr = ""
	ps := append([]model.Project(nil), b.projects...)
	go func() {
		var cols []model.Column
		var all []model.Task
		byName := map[string]string{} // column name → dest id
		for _, p := range ps {
			bd, err := b.api.Board(p.ID)
			if err != nil {
				continue
			}
			src := bd.Columns
			if len(src) == 0 {
				src = bd.Project.Columns
			}
			if cols == nil && len(src) > 0 {
				cols = append([]model.Column(nil), src...)
				for _, c := range cols {
					byName[c.Name] = c.ID
				}
			}
			idMap := map[string]string{}
			for _, c := range src {
				if dest, ok := byName[c.Name]; ok {
					idMap[c.ID] = dest
				}
			}
			for _, task := range bd.Tasks {
				if dest, ok := idMap[task.ColumnID]; ok {
					task.ColumnID = dest
				}
				all = append(all, task)
			}
		}
		b.post(func() {
			b.switching = false
			if cols == nil {
				b.loadErr = "没有可合并的项目看板"
				return
			}
			b.cur = curGlobal
			b.columns = cols
			b.tasks = all
			b.detail = nil
			b.clearMotion()
			b.sortTasks()
		})
	}()
}

// openProject switches boards, keeping the old one visible underneath.
func (b *board) openProject(id string) {
	if id == "" || id == b.cur {
		return
	}
	b.switching = true
	b.loadErr = ""
	go func() {
		bd, err := b.api.Board(id)
		b.post(func() {
			b.switching = false
			if err != nil {
				b.loadErr = "无法读取看板：" + err.Error()
				return
			}
			b.cur = id
			b.columns = bd.Columns
			b.tasks = bd.Tasks
			if len(b.columns) == 0 {
				b.columns = bd.Project.Columns
			}
			b.detail = nil
			b.clearMotion()
			b.sortTasks()
		})
	}()
}

// startStream keeps one SSE subscription open, applying events in place.
// It starts the reader at most once.
func (b *board) startStream() {
	if b.streaming {
		return
	}
	b.streaming = true
	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	go func() {
		_ = b.api.Subscribe(ctx, "", func(ev model.Event) {
			b.post(func() { b.applyEvent(ev) })
		}, func(st client.StreamState) {
			b.post(func() { b.status = st })
		})
	}()
}

func (b *board) stop() {
	if b.cancel != nil {
		b.cancel()
	}
}

// applyEvent merges one pushed change.
func (b *board) applyEvent(ev model.Event) {
	// Global view listens to every project; single-project view stays scoped.
	if ev.ProjectID != "" && b.cur != "" && b.cur != curGlobal && ev.ProjectID != b.cur {
		return
	}
	switch ev.Type {
	case model.EventTaskCreated, model.EventTaskUpdated, model.EventTaskMoved:
		if ev.Task == nil {
			return
		}
		task := *ev.Task
		b.normalizeTaskColumns(&task)
		if !b.upsert(task) {
			return
		}
		b.touchDetail(task)
		if ev.Type == model.EventTaskCreated {
			playCreateCue()
		}
		if ev.By != "" && ev.By != b.agent {
			b.recent[ev.Task.ID] = time.Now().Add(600 * time.Millisecond)
		}
		b.flash()
	case model.EventTaskArchived:
		if b.remove(ev.ID) {
			b.flash()
		}
	case model.EventCommentAdded:
		if ev.Comment != nil && b.appendComment(ev.ID, *ev.Comment) {
			b.flash()
		}
	case model.EventProjectChange:
		go func() {
			ps, err := b.api.Projects()
			if err != nil {
				return
			}
			b.post(func() { b.projects = ps })
		}()
	}
}

func (b *board) flash() { b.pulseTill = time.Now().Add(900 * time.Millisecond) }

// --- state helpers ---

func hasProject(ps []model.Project, id string) bool {
	for _, p := range ps {
		if p.ID == id {
			return true
		}
	}
	return false
}

// defaultProject picks a concrete board when the current id vanished
// (deleted project). Launch itself defaults to curGlobal, not this.
func defaultProject(ps []model.Project) string {
	for _, p := range ps {
		if p.Name == "local" {
			return p.ID
		}
	}
	if len(ps) > 0 {
		return ps[0].ID
	}
	return ""
}

func (b *board) project() *model.Project {
	for i := range b.projects {
		if b.projects[i].ID == b.cur {
			return &b.projects[i]
		}
	}
	return nil
}

func (b *board) projectName() string {
	if p := b.project(); p != nil {
		return p.Name
	}
	if b.cur == curGlobal {
		return "全部项目"
	}
	return "项目"
}

func (b *board) projectNameByID(id string) string {
	for _, p := range b.projects {
		if p.ID == id {
			return p.Name
		}
	}
	return ""
}

func (b *board) task(id string) *model.Task {
	for i := range b.tasks {
		if b.tasks[i].ID == id {
			return &b.tasks[i]
		}
	}
	return nil
}


// colScroll remembers a scroll container's place and when its bar should show.
type colScroll struct {
	state         ui.ScrollState
	lastX, lastY  float32
	armed         bool
	showUntil     time.Time
}

func (b *board) columnScroll(id string) *colScroll {
	if b.colScrolls == nil {
		b.colScrolls = map[string]*colScroll{}
	}
	s, ok := b.colScrolls[id]
	if !ok {
		s = &colScroll{}
		b.colScrolls[id] = s
	}
	return s
}

// noteScroll marks the scrollbar visible for a short time after the offset moves.
func (b *board) noteScroll(s *colScroll) {
	if s == nil {
		return
	}
	if !s.armed {
		s.lastX, s.lastY, s.armed = s.state.X, s.state.Y, true
		return
	}
	if s.state.X == s.lastX && s.state.Y == s.lastY {
		return
	}
	s.lastX, s.lastY = s.state.X, s.state.Y
	until := time.Now().Add(850 * time.Millisecond)
	s.showUntil = until
	go func() {
		time.Sleep(time.Until(until) + 20*time.Millisecond)
		b.post(func() {}) // redraw so the bar can hide again
	}()
}

func (b *board) scrollbarActive() bool {
	now := time.Now()
	for _, s := range b.colScrolls {
		if now.Before(s.showUntil) {
			return true
		}
	}
	return false
}

// tasksIn returns the column's tasks in board order, honoring searchQuery.
func (b *board) tasksIn(columnID string) []model.Task {
	var out []model.Task
	q := strings.ToLower(strings.TrimSpace(b.searchQuery))
	for _, t := range b.tasks {
		if t.ColumnID != columnID {
			continue
		}
		if q != "" {
			hay := strings.ToLower(t.Title + "\n" + t.Description)
			if !strings.Contains(hay, q) {
				continue
			}
		}
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// taskMatchesSearch reports whether a task matches the current query
// (empty query matches everything).
func (b *board) taskMatchesSearch(t model.Task) bool {
	q := strings.ToLower(strings.TrimSpace(b.searchQuery))
	if q == "" {
		return true
	}
	return strings.Contains(strings.ToLower(t.Title+"\n"+t.Description), q)
}

func (b *board) sortTasks() {
	rank := map[string]int{}
	for i, c := range b.columns {
		rank[c.ID] = i
	}
	sort.SliceStable(b.tasks, func(i, j int) bool {
		ri, rj := rank[b.tasks[i].ColumnID], rank[b.tasks[j].ColumnID]
		if ri != rj {
			return ri < rj
		}
		return b.tasks[i].Position < b.tasks[j].Position
	})
}

func (b *board) columnID(name string) string {
	for _, c := range b.columns {
		if c.Name == name || c.ID == name {
			return c.ID
		}
	}
	return ""
}

func (b *board) columnName(id string) string {
	for _, c := range b.columns {
		if c.ID == id {
			return c.Name
		}
	}
	return id
}

// mapToProjectColumnID turns a board (possibly merged) column id into the
// column id that belongs to projectID. Needed so Move hits the right
// project in 全部项目 view.
func (b *board) mapToProjectColumnID(boardColumnID, projectID string) string {
	if b.cur != curGlobal || boardColumnID == "" || projectID == "" {
		return boardColumnID
	}
	name := b.columnName(boardColumnID)
	if name == "" || name == boardColumnID {
		// columnName falls back to id when unknown; try projects directly.
		for _, p := range b.projects {
			for _, c := range p.Columns {
				if c.ID == boardColumnID {
					name = c.Name
					break
				}
			}
		}
	}
	if name == "" || name == boardColumnID {
		return boardColumnID
	}
	for _, p := range b.projects {
		if p.ID != projectID {
			continue
		}
		for _, c := range p.Columns {
			if c.Name == name {
				return c.ID
			}
		}
	}
	return boardColumnID
}

// mapToBoardColumnID remaps a project-local column id onto the merged
// board columns used while cur == curGlobal.
func (b *board) mapToBoardColumnID(projectColumnID, projectID string) string {
	if b.cur != curGlobal || projectColumnID == "" {
		return projectColumnID
	}
	var name string
	for _, p := range b.projects {
		if p.ID != projectID {
			continue
		}
		for _, c := range p.Columns {
			if c.ID == projectColumnID {
				name = c.Name
				break
			}
		}
	}
	if name == "" {
		return projectColumnID
	}
	if id := b.columnID(name); id != "" {
		return id
	}
	return projectColumnID
}

func (b *board) normalizeTaskColumns(t *model.Task) {
	if t == nil || b.cur != curGlobal {
		return
	}
	t.ColumnID = b.mapToBoardColumnID(t.ColumnID, t.ProjectID)
}

func (b *board) columnNames() []string {
	out := make([]string, len(b.columns))
	for i, c := range b.columns {
		out[i] = c.Name
	}
	return out
}

// upsert merges a pushed task; a stale copy loses.
func (b *board) upsert(t model.Task) bool {
	if t.Archived {
		return b.remove(t.ID)
	}
	for i := range b.tasks {
		if b.tasks[i].ID != t.ID {
			continue
		}
		if t.Version < b.tasks[i].Version {
			return false
		}
		b.reconcilePlacement(t)
		b.noteMove(b.tasks[i], t)
		b.tasks[i] = t
		b.sortTasks()
		return true
	}
	if b.cur != "" && b.cur != curGlobal && t.ProjectID != b.cur {
		return false
	}
	b.reconcilePlacement(t)
	b.tasks = append(b.tasks, t)
	b.sortTasks()
	return true
}

func (b *board) remove(id string) bool {
	for i := range b.tasks {
		if b.tasks[i].ID != id {
			continue
		}
		columnID := b.tasks[i].ColumnID
		b.tasks = append(b.tasks[:i], b.tasks[i+1:]...)
		b.compactColumn(columnID, "")
		b.forgetMotion(id)
		if b.detail != nil && b.detail.id == id {
			b.detail = nil
		}
		return true
	}
	return false
}

// The service renumbers both affected columns but pushes only the moved
// card. Reconstruct those sibling positions without changing versions or
// treating passive neighbors as actively moving cards.
func (b *board) reconcilePlacement(task model.Task) {
	if from := b.task(task.ID); from != nil && from.ColumnID != task.ColumnID {
		b.compactColumn(from.ColumnID, task.ID)
	}
	if task.Position <= 0 {
		return
	}
	var others []model.Task
	for _, item := range b.tasksIn(task.ColumnID) {
		if item.ID != task.ID {
			others = append(others, item)
		}
	}
	at := min(max(int(task.Position/1000)-1, 0), len(others))
	for i, item := range others {
		position := i + 1
		if i >= at {
			position++
		}
		b.task(item.ID).Position = float64(position * 1000)
	}
}

func (b *board) compactColumn(columnID, excludeID string) {
	position := 0
	for _, item := range b.tasksIn(columnID) {
		if item.ID == excludeID {
			continue
		}
		position++
		b.task(item.ID).Position = float64(position * 1000)
	}
}

func (b *board) appendComment(taskID string, cm model.Comment) bool {
	for i := range b.tasks {
		if b.tasks[i].ID != taskID {
			continue
		}
		for _, have := range b.tasks[i].Comments {
			if have.ID == cm.ID {
				return false
			}
		}
		b.tasks[i].Comments = append(b.tasks[i].Comments, cm)
		if b.detail != nil && b.detail.id == taskID {
			for _, have := range b.detail.comments {
				if have.ID == cm.ID {
					return true
				}
			}
			b.detail.comments = append(b.detail.comments, cm)
		}
		return true
	}
	return false
}

// touchDetail refreshes the open detail from a pushed copy. It never
// overwrites text the user has typed but not saved: a dirty dialog keeps
// its input and shows what the push would have changed.
func (b *board) touchDetail(t model.Task) {
	if b.detail == nil || b.detail.id != t.ID || b.saving {
		return
	}
	if b.detail.dirty() {
		b.detail.pushed = &t
		b.detail.pushBy = t.UpdatedBy
		return
	}
	b.detail.title = t.Title
	b.detail.body = t.Description
	b.detail.assignee = t.Assignee
	b.detail.column = b.columnName(t.ColumnID)
	b.detail.snap = t
	b.detail.pushed = nil
}

// --- actions ---

// toast reports something the window could not do.
func (b *board) toast(title, desc string) {
	b.post(func() {
		if b.ctx() == nil {
			return
		}
		b.ctx().AddToast(ui.Toast{Title: title, Description: desc, Type: "error", Timeout: 6 * time.Second})
	})
}

// ctx is the last frame's context, kept so background work can show a
// toast without owning a view.
func (b *board) ctx() *ui.Context { return b.frameCtx }

func (b *board) openDetail(t model.Task) {
	d := &detail{
		open:     true,
		id:       t.ID,
		title:    t.Title,
		body:     t.Description,
		assignee: t.Assignee,
		column:   b.columnName(t.ColumnID),
		loading:  true,
		snap:     t,
	}
	b.detail = d
	go func() {
		cs, err := b.api.Comments(t.ID)
		b.post(func() {
			if b.detail == nil || b.detail.id != t.ID {
				return
			}
			b.detail.loading = false
			if err == nil {
				b.detail.comments = cs
			}
		})
	}()
}

// saveDetail patches the task's text and closes the dialog.
func (b *board) saveDetail() {
	d := b.detail
	if d == nil || b.saving {
		return
	}
	t := b.task(d.id)
	if t == nil {
		b.detail = nil
		return
	}
	title := strings.TrimSpace(d.title)
	if title == "" {
		b.toast("无法保存", "标题不能为空。")
		return
	}
	body := d.body
	cur := *t
	if !d.dirty() && title == cur.Title && body == cur.Description {
		b.detail.open = false
		return
	}
	// Another agent pushed a newer version after this dialog opened: the
	// base version is stale, so asking is the only safe save. The choice
	// "keep mine" rewrites against the fresh version.
	if d.stale() {
		b.conflict = &conflict{
			open:    true,
			id:      d.id,
			title:   "任务已被修改",
			message: fmt.Sprintf("「%s」已被 %s 更新到 v%d。", clip(cur.Title, 20), agentName(cur.UpdatedBy), cur.Version),
			retry: func(version int) {
				go func() {
					if _, err := b.api.Update(cur.ID, version, &title, &body, nil); err == nil {
						b.post(func() {
							if b.detail != nil && b.detail.id == cur.ID {
								b.detail.open = false
								b.detail = nil
							}
						})
					}
				}()
			},
		}
		return
	}
	b.saving = true
	b.savingID = cur.ID
	optimistic := cur
	optimistic.Title, optimistic.Description = title, body
	optimistic.Version++
	b.replace(optimistic)
	go func() {
		_, err := b.api.Update(cur.ID, cur.Version, &title, &body, nil)
		b.post(func() {
			b.saving = false
			b.savingID = ""
			if err == nil {
				if b.detail != nil {
					b.detail.open = false
				}
				return
			}
			b.recover(cur.ID, err, func(version int) {
				_, _ = b.api.Update(cur.ID, version, &title, &body, nil)
			})
		})
	}()
}

// moveTask relocates a card optimistically, then asks the service.
func (b *board) moveTask(id, toColumn, afterID string, atTop bool) {
	t := b.task(id)
	if t == nil {
		return
	}
	// Completing (bare drop into「完成」) always lands at the column top.
	if !atTop && afterID == "" && b.columnName(toColumn) == "完成" {
		atTop = true
	}
	if t.ColumnID == toColumn && !atTop && afterID == "" {
		return
	}
	cur := *t
	// Board columns may be a merged view; the API needs this task's project column id.
	apiColumn := b.mapToProjectColumnID(toColumn, cur.ProjectID)
	optimistic := cur
	optimistic.ColumnID = toColumn
	optimistic.Version++
	b.previewMovePosition(&optimistic, afterID, atTop)
	b.replace(optimistic)
	if b.detail != nil && b.detail.id == id {
		b.detail.column = b.columnName(toColumn)
	}
	go func() {
		_, err := b.api.Move(cur.ID, cur.Version, apiColumn, afterID, atTop)
		if err != nil {
			b.post(func() {
				if fresh, ferr := b.api.Task(cur.ID); ferr == nil {
					b.replace(fresh)
				}
			})
			b.toast("移动失败", err.Error())
		}
	}()
}

func (b *board) archiveTask(id string) {
	t := b.task(id)
	if t == nil {
		return
	}
	cur := *t
	b.remove(id)
	b.archiving = true
	go func() {
		_, err := b.api.Archive(cur.ID, cur.Version)
		b.post(func() {
			b.archiving = false
			if err != nil {
				b.replace(cur)
				b.toast("归档失败", err.Error())
				return
			}
			c := b.ctx()
			if c == nil {
				return
			}
			c.ToastAction("已归档「"+clip(cur.Title, 24)+"」", "撤销", func() {
				b.restore(cur)
			})
		})
	}()
}

// restore undoes an archive: the service clears the flag and puts the card
// back at the bottom of its own column, and the pushed event redraws it.
func (b *board) restore(t model.Task) {
	go func() {
		if _, err := b.api.Restore(t.ID, 0); err != nil {
			b.toast("撤销失败", err.Error())
		}
	}()
}

func (b *board) sendComment() {
	d := b.detail
	if d == nil || b.commenting {
		return
	}
	text := strings.TrimSpace(d.comment)
	if text == "" {
		return
	}
	d.comment = ""
	b.commenting = true
	go func() {
		_, err := b.api.AddComment(d.id, text)
		b.post(func() {
			b.commenting = false
			if err != nil {
				if b.detail == d && d.comment == "" {
					d.comment = text
				}
				b.toast("评论失败", err.Error())
			}
		})
	}()
}


// playColumnChangeCue soft-clicks on a real column change; a landing in
// 「完成」gets the done chime instead. Load/openProject never call this.
func (b *board) playColumnChangeCue(toColumnID string) {
	if b.columnName(toColumnID) == "完成" {
		playDoneCue()
		return
	}
	playMoveCue()
}

func (b *board) createProject() {
	f := b.form
	if f == nil {
		return
	}
	name := strings.TrimSpace(f.name)
	if name == "" {
		b.toast("无法创建", "项目名不能为空。")
		return
	}
	f.open = false
	go func() {
		p, err := b.api.CreateProject(name)
		if err != nil {
			b.toast("创建失败", err.Error())
			return
		}
		ps, perr := b.api.Projects()
		b.post(func() {
			if perr == nil {
				b.projects = ps
			} else {
				b.projects = append(b.projects, p)
			}
			b.openProject(p.ID)
		})
	}()
}

// replace swaps a task by ID, ignoring copies older than the stored one.
func (b *board) replace(t model.Task) {
	b.normalizeTaskColumns(&t)
	for i := range b.tasks {
		if b.tasks[i].ID == t.ID {
			if t.Version < b.tasks[i].Version {
				return
			}
			b.reconcilePlacement(t)
			b.noteMove(b.tasks[i], t)
			b.tasks[i] = t
			b.sortTasks()
			return
		}
	}
	b.reconcilePlacement(t)
	b.tasks = append(b.tasks, t)
	b.sortTasks()
}

// recover handles a failed write: 409 aligns with the server, anything
// else says so. retry resends with the server's version.
func (b *board) recover(id string, err error, retry func(version int)) {
	var ce *client.ConflictError
	if !errors.As(err, &ce) {
		b.toast("操作失败", err.Error())
		if fresh, ferr := b.api.Task(id); ferr == nil {
			b.post(func() { b.replace(fresh) })
		}
		return
	}
	fresh := ce.Current
	b.replace(fresh)
	if b.detail != nil && b.detail.id == id {
		b.touchDetail(fresh)
	}
	if b.detail == nil {
		c := b.ctx()
		if c != nil {
			c.Toast("任务已被其他代理更新，已同步最新版本")
		}
		return
	}
	by := agentName(fresh.UpdatedBy)
	b.conflict = &conflict{
		open:    true,
		id:      id,
		title:   "任务已被修改",
		message: fmt.Sprintf("「%s」已被 %s 更新到 v%d。", clip(fresh.Title, 20), by, fresh.Version),
		retry:   retry,
	}
}

// agentName names whoever last wrote a task, for dialogs and toasts.
func agentName(by string) string {
	if by == "" || by == "local" {
		return "其他代理"
	}
	return by
}

// clip shortens s for a label, counting runes.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
