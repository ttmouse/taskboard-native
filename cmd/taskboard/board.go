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
	"sync"
	"time"

	"taskboard-native/internal/client"
	"taskboard-native/internal/executor"
	"taskboard-native/internal/model"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// api is the slice of the service the window uses. client.Client
// implements it; tests inject a fake.
type api interface {
	Projects() ([]model.Project, error)
	Board(projectID string) (client.Board, error)
	Task(id string) (model.Task, error)
	AddRelation(id string, version int, kind, relatedID string) (model.RelationResult, error)
	RemoveRelation(id string, version int, kind, relatedID string) (model.RelationResult, error)
	HandoffLog(taskID string) (executor.HandoffLogView, error)
	CreateTask(projectID, columnID, title, description, assignee string) (model.Task, error)
	CreateTaskAndDispatch(projectID, columnID, title, description, assignee, runtime string) (model.Task, error)
	Update(id string, version int, title, description, assignee *string) (model.Task, error)
	Move(id string, version int, toColumn, afterID string, atTop bool) (model.Task, error)
	Archive(id string, version int) (model.Task, error)
	Restore(id string, version int) (model.Task, error)
	Comments(taskID string) ([]model.Comment, error)
	AddComment(taskID, text string) (model.Comment, error)
	AddCommentTo(taskID, text, mention string) (model.Comment, error)
	AddCommentWithParent(taskID, text, mention, parentCommentID string) (model.Comment, error)
	UpdateComment(taskID, commentID, text string) (model.Comment, error)
	DeleteComment(taskID, commentID string) error
	ContinueComment(taskID, commentID string) error
	CreateProject(name string) (model.Project, error)
	CreateProjectWithPath(name, workspacePath string) (model.Project, error)
	CreateProjectWithColor(name, workspacePath, color string) (model.Project, error)
	UpdateProject(id, name, workspacePath string) (model.Project, error)
	UpdateProjectWithColor(id, name, workspacePath, color string) (model.Project, error)
	DeleteProject(id string) error
	Dispatch(taskID, runtime string) error
	StartProjectRun(projectID, runtime string) (model.ProjectRun, error)
	ProjectRun(projectID string) (model.ProjectRun, error)
	StopProjectRun(projectID string) (model.ProjectRun, error)
	Subscribe(ctx context.Context, projectID string, onEvent func(model.Event), onState func(client.StreamState)) error
}

// agentSetter is what a client exposes when its identity can change while
// it runs, which is what editing 显示名 does. Tests inject fakes without
// it, so the board only signs under a new name when there is a client to
// tell.
type agentSetter interface{ SetAgent(string) }

// cardDrag is the value a dragged card carries between columns.
type cardDrag string

// curGlobal is the sidebar「全部项目」selection: tasks from every project.
const curGlobal = "__global__"

// cardMatch gives a card one identity across columns, so MyGo slides it
// from the source column into the target instead of exit plus enter.
type cardMatch struct{ ID string }

// detail is the open task detail, with the edits not yet saved.
type detail struct {
	open                bool
	id                  string
	title               string
	body                string
	assignee            string
	column              string // selected column name in the move select
	comment             string
	commentEditID       string
	commentEditText     string
	commentParentID     string
	commentParentAuthor string
	commentMention      string // execution channel addressed with @ in the draft
	fileMentionTargets  []workspaceMentionTarget
	fileMentionIndex    int
	mentionOpen         bool // the @ picker is showing, waiting for a channel
	mentionIndex        int  // keyboard-highlighted channel while the picker is open
	mentionSearchBase   string
	mentionSearch       string
	commentFocus        int  // frames left to focus the composer on open or hand focus back after picking
	commentCaretStart   bool // the caret sat at the start when the field was built
	comments            []model.Comment
	relationExpanded    bool   // 关系模块；默认折叠
	relationPickKind    string // "" | sub | blocks | blocker | create-sub | create-blocks | create-blocker
	relationNewTitle    string
	handoffLog          executor.HandoffLogView
	handoffLogLoading   bool
	handoffLogFailed    bool
	handoffLogExpanded  bool
	loading             bool // comments still arriving
	bodyImage           imageEdit
	commentImage        imageEdit
	commentImages       []string
	commentFiles        []string
	commentThumbs       map[string]ui.ImageSource
	commentThumbLoading map[string]bool
	markdown            *markdown.Options
	markdownImages      sync.Map // URL → *markdownImageLoad

	// editTitle / editBody keep the field in input mode while focused
	// (even after the pointer leaves); cleared on blur when not hovered.
	editTitle bool
	editBody  bool
	// bodyEditSeenFocus becomes true once the body TextArea has taken
	// focus this edit session, so the first frame after click (before
	// focus lands) does not immediately fall back to Markdown.
	bodyEditSeenFocus bool

	// editTable is true while one Markdown block (table or inter-table
	// plain segment) is open as source; tableStart/tableEnd index that
	// range in body, tableEdit holds the draft. Named for the table case
	// that motivated Typora-style editing; plain segments reuse it.
	editTable          bool
	tableStart         int
	tableEnd           int
	tableEdit          string
	tableEditSeenFocus bool
	// tableTop places the caret at the head once when the block editor opens
	// (fresh TextArea defaults to the end, which hides long pipe tables).
	tableTop bool

	// commentBlock* mirrors body block-edit for one comment's table (or
	// plain segment between tables). Write-back updates the comment via API.
	commentBlockID        string
	commentBlockBase      string // full comment text while a block is open
	commentBlockStart     int
	commentBlockEnd       int
	commentBlockEdit      string
	commentBlockSeenFocus bool
	commentBlockTop       bool

	// bodyTop scrolls the body to its first line once, when the dialog
	// opens: a fresh TextArea puts the caret at the end of the text and
	// reveals it, which for a long description leaves the head hidden.
	bodyTop bool

	// snap is the task as it was opened; dirty() compares against it so a
	// pushed update cannot silently overwrite unsaved typing.
	snap   model.Task
	pushed *model.Task // an incoming update held back while dirty
	pushBy string
}

// dirty reports whether the dialog holds edits the user has not saved.
func (d *detail) dirty() bool {
	return d.bodyImage.busy() || strings.TrimSpace(d.title) != d.snap.Title || d.liveBody() != d.snap.Description
}

// liveBody is the description including an in-progress table-block draft.
func (d *detail) liveBody() string {
	if d == nil {
		return ""
	}
	if d.editTable {
		return spliceBodyRange(d.body, d.tableStart, d.tableEnd, d.tableEdit)
	}
	return d.body
}

// commitTableEdit writes the table-block draft back into body and leaves
// block-edit mode.
func (d *detail) commitTableEdit() {
	if d == nil || !d.editTable {
		return
	}
	d.body = spliceBodyRange(d.body, d.tableStart, d.tableEnd, d.tableEdit)
	d.editTable = false
	d.tableEditSeenFocus = false
	d.tableTop = false
	d.tableStart, d.tableEnd = 0, 0
	d.tableEdit = ""
}

// beginTableEdit opens source editing for body[start:end], committing any
// prior table draft first and shifting offsets when that draft changed length.
func (d *detail) beginTableEdit(start, end int) {
	if d == nil {
		return
	}
	d.editBody = false
	d.bodyEditSeenFocus = false
	if d.editTable {
		oldStart, oldEnd := d.tableStart, d.tableEnd
		oldLen := oldEnd - oldStart
		newLen := len(d.tableEdit)
		d.body = spliceBodyRange(d.body, oldStart, oldEnd, d.tableEdit)
		d.editTable = false
		d.tableEditSeenFocus = false
		delta := newLen - oldLen
		if start >= oldEnd {
			start += delta
			end += delta
		} else if end <= oldStart {
			// earlier table: offsets unchanged
		} else {
			// overlapped the table just committed; edit the spliced range
			start, end = oldStart, oldStart+newLen
		}
	}
	if start < 0 {
		start = 0
	}
	if end > len(d.body) {
		end = len(d.body)
	}
	if start > end {
		start = end
	}
	d.editTable = true
	d.tableStart = start
	d.tableEnd = end
	d.tableEdit = d.body[start:end]
	d.tableEditSeenFocus = false
	d.tableTop = true
}

// beginCommentBlockEdit opens source editing for comment text[start:end].
func (d *detail) beginCommentBlockEdit(id, text string, start, end int) {
	if d == nil || id == "" {
		return
	}
	d.commentEditID, d.commentEditText = "", ""
	if d.commentBlockID != "" && d.commentBlockID != id {
		d.clearCommentBlockEdit()
	}
	base := text
	if d.commentBlockID == id {
		oldStart, oldEnd := d.commentBlockStart, d.commentBlockEnd
		oldLen := oldEnd - oldStart
		newLen := len(d.commentBlockEdit)
		base = spliceBodyRange(d.commentBlockBase, oldStart, oldEnd, d.commentBlockEdit)
		delta := newLen - oldLen
		if start >= oldEnd {
			start += delta
			end += delta
		} else if end <= oldStart {
			// earlier range
		} else {
			start, end = oldStart, oldStart+newLen
		}
	}
	if start < 0 {
		start = 0
	}
	if end > len(base) {
		end = len(base)
	}
	if start > end {
		start = end
	}
	d.commentBlockID = id
	d.commentBlockBase = base
	d.commentBlockStart = start
	d.commentBlockEnd = end
	d.commentBlockEdit = base[start:end]
	d.commentBlockSeenFocus = false
	d.commentBlockTop = true
}

func (d *detail) clearCommentBlockEdit() {
	if d == nil {
		return
	}
	d.commentBlockID = ""
	d.commentBlockBase = ""
	d.commentBlockStart, d.commentBlockEnd = 0, 0
	d.commentBlockEdit = ""
	d.commentBlockSeenFocus = false
	d.commentBlockTop = false
}

// liveCommentText returns comment text with an in-progress block draft spliced in.
func (d *detail) liveCommentText(id, base string) string {
	if d == nil || d.commentBlockID != id {
		return base
	}
	return spliceBodyRange(d.commentBlockBase, d.commentBlockStart, d.commentBlockEnd, d.commentBlockEdit)
}

// stale reports whether an agent pushed a newer version while the dialog
// held unsaved edits, so saving must ask before overwriting either side.
func (d *detail) stale() bool { return d.dirty() && d.pushed != nil }

// form is a small dialog: a new task or a new project.
type form struct {
	open             bool
	isTask           bool // true → 新建任务；false → 新建项目
	name             string
	workspacePath    string // project form: local working directory
	color            string // project form: accent color; "" → automatic slot
	gitCommitOnDone  bool   // project edit: 验收完成时自动 Git 提交
	pickingWorkspace bool   // one native directory picker at a time
	columnID         string // task form only: target column
	projectID        string // task form only: which project receives the card
	runtimeLabel     string // selected execution channel for Cmd+Enter
	creating         bool   // form write is awaiting the service
	// composeImages holds standalone image URLs pasted while composing the
	// task; they are embedded in the task body when it is created.
	composeImages        []string
	fileMentionTargets   []workspaceMentionTarget
	fileMentionIndex     int
	fileMentionOpen      bool
	fileMentionProjectID string
	fileMentions         []string
	composeImage         imageEdit
	// thumbs caches decoded bitmaps for the small preview strip so each
	// image is fetched once, not every frame.
	thumbs       map[string]ui.ImageSource
	thumbLoading map[string]bool
	// markdown renders pasted-image markdown previews in the composer.
	markdown *markdown.Options
	// editingProjectID non-empty turns the project form into an edit sheet
	// for that project (name pre-filled, save instead of create).
	editingProjectID string
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

	// timeFilter narrows the board to tasks updated within a window:
	// "" (all) / "today" / "yesterday" / "7d" / "30d". Column counts and
	// card lists honor it, so「今天完成了哪些」reads straight off the
	// 完成 column.
	timeFilter string

	// projectRunRuntime remembers the channel handling continuous handoff for
	// each open project. The service remains the source of truth.
	projectRunRuntime map[string]string

	// projectSort orders the sidebar's project list: "" (server order,
	// position + created) / "updated" (ActiveAt desc) / "created"
	// (CreatedAt desc).
	projectSort string

	// confirmDelete holds the project pending deletion; the sidebar asks,
	// the dialog answers, the API keeps no undo.
	confirmDelete        *model.Project
	confirmDeleteComment *model.Comment

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

	// hotkeyErr is why the global ⌘⇧N shortcut could not be bound ("" when
	// none). It survives the launch-time frame gap that swallows toasts, so
	// the settings sheet can show the failure until the user sees it.
	hotkeyErr string

	// openDialog is mygo's native open panel unless a test stands in a fake.
	openDialog func(mygo.OpenDialogOptions) ([]string, error)

	prefs           uiPrefs // persisted window preferences (title-only cards, …)
	sidebarW        float32 // live sidebar width; 0 until first layout
	sidebarResizing bool    // dragging the divider tracks the rail directly, without its open/close tween

	// The sidebar slides open and shut (railWidth); the board pane takes the
	// room it leaves. While the rail moves, the columns' and cards' own
	// Position+Size transitions would lag behind the pane and got clipped at
	// its left edge, so paneResize asks them to follow the pane exactly
	// instead of tweening toward it. paneW is last frame's pane width.
	paneW                  float32
	frameH                 float32 // the window height this frame laid out at
	paneResize             bool
	railArmed              bool      // the rail is on screen, so a change can animate
	railNow                float32   // the rail's width as this frame shows it
	railAt                 time.Time // when the current slide began
	railFrom               float32   // the width it began at
	railTo                 float32   // the width it heads for
	sidebarPerfToggleAt    time.Time
	sidebarPerfFirstChange time.Duration
	sidebarPerfCase        string

	// taskViewCache keeps per-column card slices ready for each frame. The
	// board is rebuilt during sidebar motion, so scanning and sorting every
	// task from each column renderer on every animation frame was needless
	// main-thread work.
	taskViewCache       map[string][]model.Task
	taskViewCacheFilter string

	searchQuery               string // filter: title + description, all columns
	searchOpen                bool   // sidebar search button → immersive centered search overlay
	searchSelection           int    // highlighted search hit; -1 means no keyboard selection
	searchSelectionQuery      string // query the current keyboard selection belongs to
	quickTitle                string // vertical-list top field; Enter creates in 待办
	quickFileMentions         []string
	quickCreating             bool // the quick-add request is awaiting the service
	quickFileMentionOpen      bool
	quickFileMentionIndex     int
	quickFileMentionProjectID string
	quickFileMentionTargets   []workspaceMentionTarget

	// colScrolls tracks per-column (and board) scroll offsets so the
	// scrollbar theme can stay transparent until the user scrolls.
	colScrolls map[string]*colScroll

	frameCtx *ui.Context
	// toastLog records emitToast calls for tests (nil in production).
	toastLog []toastEntry
	curTheme *ui.Theme // the theme the current frame builds with

	drafts map[string]string // retained for tests; column quick-add UI removed

	agent        string
	streaming    bool                 // the SSE reader is running
	recent       map[string]time.Time // task ID → until when it shows as changed
	bounds       map[string]ui.Rect
	cardHeights  map[string]float32   // persist row height for virtual list placeholders
	sfxMuteUntil map[string]time.Time // per-task debounce for column-change cues
	motionState                       // accepted motion shared with the Playground
}

func newBoard(a api) *board {
	prefs := loadUIPrefs()
	b := &board{
		api:         a,
		msgs:        make(chan func(), 64),
		drafts:      map[string]string{},
		status:      client.Connecting,
		agent:       prefs.windowAgent(),
		projectSort: prefs.ProjectSort,
		recent:      map[string]time.Time{},
		bounds:      map[string]ui.Rect{},
		prefs:       prefs,
		// Launch into「全部项目」, not a concrete board like local.
		cur: curGlobal,
	}
	// A board built around a live client signs its writes with the saved
	// display name from its first request on, whatever the caller passed.
	if s, ok := a.(agentSetter); ok {
		s.SetAgent(b.agent)
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
			b.loadProjectRun(id)
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
	if ev.Type == model.EventBoardSnapshot {
		b.applySnapshot(ev.Snapshot)
		return
	}
	// Global view listens to every project; single-project view stays scoped.
	if ev.ProjectID != "" && b.cur != "" && b.cur != curGlobal && ev.ProjectID != b.cur {
		return
	}
	b.invalidateTaskViewCache()
	switch ev.Type {
	case model.EventTaskCreated, model.EventTaskUpdated, model.EventTaskMoved:
		if ev.Task == nil {
			return
		}
		task := *ev.Task
		b.normalizeTaskColumns(&task)
		applied, inserted := b.upsert(task)
		if !applied {
			return
		}
		b.touchDetail(task)
		// Create cue only when the card is new locally — updates that arrive
		// tagged as created (or a create that races an optimistic insert)
		// must not re-tick.
		if ev.Type == model.EventTaskCreated && inserted {
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
		if ev.Comment != nil {
			if completedBy := model.CompletingAgent(*ev.Comment); completedBy != "" {
				if task := b.task(ev.ID); task != nil {
					task.CompletedBy = completedBy
				}
				if b.detail != nil && b.detail.id == ev.ID {
					b.detail.snap.CompletedBy = completedBy
				}
			}
		}
		if ev.Comment != nil && b.appendComment(ev.ID, *ev.Comment) {
			b.flash()
		}
	case model.EventCommentUpdated, model.EventCommentDeleted:
		if ev.Comment == nil {
			return
		}
		for i := range b.tasks {
			if b.tasks[i].ID != ev.ID {
				continue
			}
			b.tasks[i].Comments = applyCommentMutation(b.tasks[i].Comments, ev.Type, *ev.Comment)
			break
		}
		if b.detail != nil && b.detail.id == ev.ID {
			d := b.detail
			d.comments = applyCommentMutation(d.comments, ev.Type, *ev.Comment)
			if ev.Type == model.EventCommentDeleted && d.commentParentID == ev.Comment.ID {
				d.commentParentID, d.commentParentAuthor = "", ""
			}
			if d.commentEditID == ev.Comment.ID {
				d.commentEditID = ""
				d.commentEditText = ""
			}
		}
		b.flash()
	case model.EventProjectChange:
		go func() {
			ps, err := b.api.Projects()
			if err != nil {
				return
			}
			b.post(func() {
				b.projects = ps
				// 列的角色可能刚被改动：当前项目的列直接换成服务端最新快照，
				// 让「按角色认列」的行为不用重开窗口就生效。
				if b.cur != "" && b.cur != curGlobal {
					for _, p := range ps {
						if p.ID == b.cur && len(p.Columns) > 0 {
							b.columns = p.Columns
						}
					}
				}
				// 项目可能刚被删除：当前项目不在最新列表里就退回全局视角。
				if b.cur != "" && b.cur != curGlobal {
					found := false
					for _, p := range ps {
						if p.ID == b.cur {
							found = true
							break
						}
					}
					if !found {
						go b.openGlobal()
					}
				}
			})
		}()
	}
}

func applyCommentMutation(comments []model.Comment, eventType string, comment model.Comment) []model.Comment {
	for i := range comments {
		if comments[i].ID != comment.ID {
			continue
		}
		if eventType == model.EventCommentDeleted {
			return append(comments[:i], comments[i+1:]...)
		}
		comments[i] = comment
		return comments
	}
	if eventType == model.EventCommentUpdated {
		return append(comments, comment)
	}
	return comments
}

// applySnapshot replaces the local board with the consistent snapshot sent
// at the start of each SSE connection. It repairs changes missed while the
// stream was disconnected; subsequent queued events are newer than the
// snapshot sequence and continue to apply incrementally.
func (b *board) applySnapshot(snapshot *model.BoardSnapshot) {
	if snapshot == nil {
		return
	}
	b.projects = append([]model.Project(nil), snapshot.Projects...)
	var columns []model.Column
	var tasks []model.Task
	if b.cur == curGlobal {
		byName := map[string]string{}
		for _, board := range snapshot.Boards {
			if columns == nil && len(board.Columns) > 0 {
				columns = append([]model.Column(nil), board.Columns...)
				for _, column := range columns {
					byName[column.Name] = column.ID
				}
			}
			idMap := map[string]string{}
			for _, column := range board.Columns {
				if dest, ok := byName[column.Name]; ok {
					idMap[column.ID] = dest
				}
			}
			for _, task := range board.Tasks {
				if dest, ok := idMap[task.ColumnID]; ok {
					task.ColumnID = dest
				}
				tasks = append(tasks, task)
			}
		}
	} else {
		for _, board := range snapshot.Boards {
			if board.Project.ID == b.cur {
				columns = append([]model.Column(nil), board.Columns...)
				tasks = append([]model.Task(nil), board.Tasks...)
				break
			}
		}
	}
	if b.cur != "" && b.cur != curGlobal && columns == nil {
		return
	}
	b.columns = columns
	b.tasks = tasks
	b.invalidateTaskViewCache()
	b.clearMotion()
	b.sortTasks()
	if b.detail != nil {
		if task := b.task(b.detail.id); task != nil && task.Version > b.detail.snap.Version {
			b.touchDetail(*task)
		}
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

// sortedProjects returns the sidebar's project list in the order the active
// projectSort asks for, without mutating b.projects: "" keeps the server's
// position/created order, "updated" puts the most recently active project
// first, "created" the newest project first.
func (b *board) sortedProjects() []model.Project {
	out := make([]model.Project, len(b.projects))
	copy(out, b.projects)
	switch b.projectSort {
	case "updated":
		sort.SliceStable(out, func(i, j int) bool {
			return out[i].ActiveAt.After(out[j].ActiveAt)
		})
	case "created":
		sort.SliceStable(out, func(i, j int) bool {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		})
	}
	return out
}

func (b *board) setProjectSort(mode string) {
	if mode != "" && mode != "updated" && mode != "created" {
		return
	}
	b.projectSort = mode
	b.prefs.ProjectSort = mode
	_ = saveUIPrefs(b.prefs)
}

// projectByID returns the project the id names, for the color the card's
// global-view accent wears.
func (b *board) projectByID(id string) (model.Project, bool) {
	for _, p := range b.projects {
		if p.ID == id {
			return p, true
		}
	}
	return model.Project{}, false
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
	state        ui.ScrollState
	lastX, lastY float32
	armed        bool
	showUntil    time.Time
	// lastViewH caches the scroll viewport height for columnCards culling
	// when Element.Bounds is still zero inside Children.
	lastViewH float32
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

// tasksIn returns the column's tasks in board order, honoring the active time
// filter. Search is scoped to the search overlay and must not change the board
// behind it.
func (b *board) tasksIn(columnID string) []model.Task {
	if b.taskViewCache == nil || b.taskViewCacheFilter != b.timeFilter {
		b.taskViewCache = make(map[string][]model.Task)
		b.taskViewCacheFilter = b.timeFilter
		for _, task := range b.tasks {
			if !b.timeFilterHits(task) {
				continue
			}
			b.taskViewCache[task.ColumnID] = append(b.taskViewCache[task.ColumnID], task)
		}
		for id := range b.taskViewCache {
			sort.SliceStable(b.taskViewCache[id], func(i, j int) bool {
				a, c := b.taskViewCache[id][i], b.taskViewCache[id][j]
				if a.Position != c.Position {
					return a.Position < c.Position
				}
				return a.CreatedAt.Before(c.CreatedAt)
			})
		}
	}
	return b.taskViewCache[columnID]
}

func (b *board) invalidateTaskViewCache() {
	b.taskViewCache = nil
}

// timeFilterHits reports whether a task passes the active time filter.
// Windows are local calendar days: 今天 / 昨天 are single days, 近7天 and
// 近30天 count back from today inclusive. The updated time is the last
// mutation (moving a card to 完成 bumps it), so「今天完成了哪些」filters on
// UpdatedAt.
func (b *board) timeFilterHits(t model.Task) bool {
	if b.timeFilter == "" {
		return true
	}
	now := time.Now()
	loc := now.Location()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	switch b.timeFilter {
	case "today":
		return !t.UpdatedAt.Before(start) && t.UpdatedAt.Before(start.AddDate(0, 0, 1))
	case "yesterday":
		y := start.AddDate(0, 0, -1)
		return !t.UpdatedAt.Before(y) && t.UpdatedAt.Before(start)
	case "7d":
		return !t.UpdatedAt.Before(start.AddDate(0, 0, -6))
	case "30d":
		return !t.UpdatedAt.Before(start.AddDate(0, 0, -29))
	}
	return true
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

// searchHits returns tasks matching searchQuery (title + description).
// Empty query yields no hits — the overlay lists matches as the user types.
func (b *board) searchHits() []model.Task {
	q := strings.TrimSpace(b.searchQuery)
	if q == "" {
		return nil
	}
	var out []model.Task
	for _, t := range b.tasks {
		if b.taskMatchesSearch(t) {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Title != out[j].Title {
			return out[i].Title < out[j].Title
		}
		return out[i].ID < out[j].ID
	})
	return out
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

// columnIDByRole resolves a workflow role on the current board view. The
// resolver falls back to the legacy Chinese name, so projects created before
// roles existed (or renamed before the upgrade) keep working.
func (b *board) columnIDByRole(role string) string {
	if c, ok := model.ColumnByRole(b.columns, role); ok {
		return c.ID
	}
	return ""
}

// columnHasRole reports whether columnID currently plays role.
func (b *board) columnHasRole(columnID, role string) bool {
	c, ok := model.ColumnByRole(b.columns, role)
	return ok && c.ID == columnID
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
// applied is false when the event was ignored; inserted is true only when
// a new card joined the local board (create cue uses this).
func (b *board) upsert(t model.Task) (applied, inserted bool) {
	if t.Archived {
		return b.remove(t.ID), false
	}
	for i := range b.tasks {
		if b.tasks[i].ID != t.ID {
			continue
		}
		if t.Version < b.tasks[i].Version {
			return false, false
		}
		b.reconcilePlacement(t)
		b.noteMove(b.tasks[i], t)
		b.tasks[i] = t
		b.invalidateTaskViewCache()
		b.sortTasks()
		return true, false
	}
	if b.cur != "" && b.cur != curGlobal && t.ProjectID != b.cur {
		return false, false
	}
	b.reconcilePlacement(t)
	b.tasks = append(b.tasks, t)
	b.invalidateTaskViewCache()
	b.sortTasks()
	return true, true
}

func (b *board) remove(id string) bool {
	for i := range b.tasks {
		if b.tasks[i].ID != id {
			continue
		}
		columnID := b.tasks[i].ColumnID
		b.tasks = append(b.tasks[:i], b.tasks[i+1:]...)
		b.invalidateTaskViewCache()
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
		b.invalidateTaskViewCache()
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

// toastEntry is one emitToast call, kept for tests.
type toastEntry struct {
	Kind, Title, Desc string
}

// --- actions ---

// toast reports a failure the window could not complete.
func (b *board) toast(title, desc string) {
	b.emitToast("error", title, desc)
}

// infoToast is a soft, non-error notice (skip / neutral nudge, …).
func (b *board) infoToast(title, desc string) {
	b.emitToast("info", title, desc)
}

// okToast is a success notice (created / dispatched / saved / …).
func (b *board) okToast(title, desc string) {
	b.emitToast("success", title, desc)
}

func (b *board) emitToast(kind, title, desc string) {
	if b.toastLog != nil {
		b.toastLog = append(b.toastLog, toastEntry{Kind: kind, Title: title, Desc: desc})
	}
	b.post(func() {
		if b.ctx() == nil {
			return
		}
		b.ctx().AddToast(ui.Toast{Title: title, Description: desc, Type: kind, Timeout: 6 * time.Second})
	})
}

// ctx is the last frame's context, kept so background work can show a
// toast without owning a view.
func (b *board) ctx() *ui.Context { return b.frameCtx }

func (b *board) openDetail(t model.Task) {
	d := &detail{
		open:              true,
		id:                t.ID,
		title:             t.Title,
		body:              t.Description,
		assignee:          t.Assignee,
		column:            b.columnName(t.ColumnID),
		loading:           true,
		handoffLogLoading: true,
		commentFocus:      1,
		snap:              t,
		bodyTop:           true,
		relationExpanded:  false, // 关系默认折叠
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
	go func() {
		view, err := b.api.HandoffLog(t.ID)
		b.post(func() {
			if b.detail == nil || b.detail.id != t.ID {
				return
			}
			b.detail.handoffLogLoading = false
			b.detail.handoffLogFailed = err != nil
			if err == nil {
				b.detail.handoffLog = view
			}
		})
	}()
}

// saveDetail patches the task's text and closes the dialog.
func (b *board) saveDetail() {
	d := b.detail
	if d == nil || b.saving || d.bodyImage.busy() || d.commentImage.busy() {
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
	d.commitTableEdit()
	body := d.body
	cur := *t
	if !d.dirty() && title == cur.Title && body == cur.Description {
		d.open = false
		b.detail = nil
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
				b.saveDetailVersion(d, title, body, version)
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
	b.saveDetailVersion(d, title, body, cur.Version)
}

// Both the initial save and a confirmed conflict retry have the same
// completion semantics: close only the detail that submitted the write.
func (b *board) saveDetailVersion(d *detail, title, body string, version int) {
	b.saving = true
	b.savingID = d.id
	go func() {
		_, err := b.api.Update(d.id, version, &title, &body, nil)
		b.post(func() {
			b.saving = false
			b.savingID = ""
			if err == nil {
				if b.detail == d {
					d.open = false
					b.detail = nil
				}
				return
			}
			b.recover(d.id, err, func(version int) {
				b.saveDetailVersion(d, title, body, version)
			})
		})
	}()
}

// moveTask relocates a card optimistically, then asks the service.
func (b *board) moveTask(id, toColumn, afterID string, atTop bool) {
	b.moveTaskWithSuccess(id, toColumn, afterID, atTop, nil)
}

func (b *board) moveTaskWithSuccess(id, toColumn, afterID string, atTop bool, onSuccess func()) {
	t := b.task(id)
	if t == nil {
		return
	}
	// Completing (bare drop into the done column) always lands at the top.
	if !atTop && afterID == "" && b.columnHasRole(toColumn, model.ColumnRoleDone) {
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
		b.post(func() {
			if err != nil {
				if fresh, ferr := b.api.Task(cur.ID); ferr == nil {
					b.replace(fresh)
				}
				b.toast("移动失败", err.Error())
				return
			}
			if onSuccess != nil {
				onSuccess()
			}
			if b.columnHasRole(toColumn, model.ColumnRoleDone) {
				b.maybeGitCommitOnDone(cur)
			}
		})
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

func (b *board) changeTaskRelation(taskID string, version int, kind, relatedID string, remove bool) {
	go func() {
		var task, related model.Task
		var err error
		var result model.RelationResult
		if remove {
			result, err = b.api.RemoveRelation(taskID, version, kind, relatedID)
		} else {
			result, err = b.api.AddRelation(taskID, version, kind, relatedID)
		}
		task, related = result.Task, result.RelatedTask
		b.post(func() {
			if err != nil {
				b.toast("关系更新失败", err.Error())
				return
			}
			b.replace(task)
			b.replace(related)
		})
	}()
}

// restore undoes an archive: the service clears the flag and puts the card
// back at the bottom of its own column, and the pushed event redraws it.

// createRelatedTask creates an intake card then links it under kind
// (sub → parent of new card is current; blocks / blocker as relation API kinds).
func (b *board) createRelatedTask(anchorID, kind, title string) {
	title = strings.TrimSpace(title)
	if title == "" {
		b.toast("无法创建", "标题不能为空")
		return
	}
	anchor := b.task(anchorID)
	if anchor == nil {
		return
	}
	intake := b.projectColumnIDByRole(anchor.ProjectID, model.ColumnRoleIntake)
	if intake == "" {
		b.toast("无法创建", "项目没有入口列")
		return
	}
	if d := b.detail; d != nil && d.id == anchorID {
		d.relationNewTitle = ""
		d.relationPickKind = ""
	}
	go func() {
		created, err := b.api.CreateTask(anchor.ProjectID, intake, title, "", "")
		if err != nil {
			b.post(func() { b.toast("创建失败", err.Error()) })
			return
		}
		var result model.RelationResult
		switch kind {
		case "sub":
			result, err = b.api.AddRelation(created.ID, created.Version, "parent", anchorID)
		case "blocks":
			result, err = b.api.AddRelation(anchorID, anchor.Version, "blocks", created.ID)
		case "blocker":
			result, err = b.api.AddRelation(anchorID, anchor.Version, "blocked_by", created.ID)
		default:
			b.post(func() {
				b.upsert(created)
				b.okToast("已创建", fmt.Sprintf("#%d %s", created.Number, clip(created.Title, 30)))
			})
			return
		}
		b.post(func() {
			if err != nil {
				b.upsert(created)
				b.toast("已创建，关联失败", err.Error())
				return
			}
			b.replace(result.Task)
			b.replace(result.RelatedTask)
			b.okToast("已创建并关联", fmt.Sprintf("#%d %s", created.Number, clip(created.Title, 30)))
		})
	}()
}

func (b *board) createSubtask(parentID, title string) {
	b.createRelatedTask(parentID, "sub", title)
}

// relationCandidateTasks lists same-project non-archived cards for relation pickers,
// excluding IDs in exclude and applying DSH-style cycle guards for parent/child.
func (b *board) relationCandidateTasks(current model.Task, kind string) []model.Task {
	exclude := map[string]bool{current.ID: true}
	switch kind {
	case "parent":
		// cannot set a descendant as parent
		queue := make([]string, 0, len(current.Relations.SubIssues))
		for _, c := range current.Relations.SubIssues {
			queue = append(queue, c.ID)
		}
		for len(queue) > 0 {
			id := queue[0]
			queue = queue[1:]
			if exclude[id] {
				continue
			}
			exclude[id] = true
			if child := b.task(id); child != nil {
				for _, gc := range child.Relations.SubIssues {
					queue = append(queue, gc.ID)
				}
			}
		}
		if current.Relations.Parent != nil {
			exclude[current.Relations.Parent.ID] = true
		}
	case "sub":
		for _, c := range current.Relations.SubIssues {
			exclude[c.ID] = true
		}
		// cannot attach an ancestor as child
		p := current.Relations.Parent
		for p != nil {
			if exclude[p.ID] {
				break
			}
			exclude[p.ID] = true
			if parent := b.task(p.ID); parent != nil {
				p = parent.Relations.Parent
			} else {
				break
			}
		}
	case "blocks":
		for _, c := range current.Relations.Blocks {
			exclude[c.ID] = true
		}
	case "blocker":
		for _, c := range current.Relations.BlockedBy {
			exclude[c.ID] = true
		}
	}
	out := make([]model.Task, 0)
	for _, candidate := range b.tasks {
		if candidate.ProjectID != current.ProjectID || candidate.Archived || exclude[candidate.ID] {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

func relationTaskLabel(t model.Task) string {
	return fmt.Sprintf("#%d %s", t.Number, clip(t.Title, 36))
}

func relationSummaryLabel(s model.TaskRelationSummary) string {
	return fmt.Sprintf("#%d %s", s.Number, clip(s.Title, 36))
}

func (b *board) restore(t model.Task) {
	go func() {
		if _, err := b.api.Restore(t.ID, 0); err != nil {
			b.toast("撤销失败", err.Error())
		}
	}()
}

// dispatchTask sends the card to the selected runtime. The service runs
// the CLI inside the project's workspace directory and writes
// progress back as comments and column moves; the GUI just watches the
// stream, so there is nothing to update optimistically here.
func (b *board) dispatchTask(id, runtime string) {
	t := b.task(id)
	if t == nil {
		return
	}
	label := ""
	if runtime == "" {
		runtime = executor.RuntimeDSH
	}
	for _, channel := range executor.Runtimes() {
		if channel.ID == runtime {
			label = channel.Label
			break
		}
	}
	if label == "" {
		b.toast("无法派发", "未知执行渠道："+runtime)
		return
	}
	var ws string
	for _, p := range b.projects {
		if p.ID == t.ProjectID {
			ws = strings.TrimSpace(p.WorkspacePath)
			break
		}
	}
	if ws == "" {
		b.toast("无法派发", "项目未设置工作路径，先给项目设置工作目录")
		return
	}
	go func() {
		err := b.api.Dispatch(id, runtime)
		b.post(func() {
			if err != nil {
				b.toast("派发失败", err.Error())
				return
			}
			b.okToast("已派发 "+label+" 执行", clip(t.Title, 24))
		})
	}()
}

func (b *board) sendComment() {
	b.submitComment(false)
}

func (b *board) beginCommentEdit(cm model.Comment) {
	if b.detail == nil || cm.Author != b.agent || b.commenting {
		return
	}
	b.detail.clearCommentBlockEdit()
	b.detail.commentEditID = cm.ID
	b.detail.commentEditText = cm.Text
}

// commitCommentBlockEdit splices the in-place comment block draft and saves.
func (b *board) commitCommentBlockEdit() {
	d := b.detail
	if d == nil || d.commentBlockID == "" || b.commenting {
		return
	}
	base := d.commentBlockBase
	text := strings.TrimSpace(d.liveCommentText(d.commentBlockID, base))
	id, taskID := d.commentBlockID, d.id
	orig := ""
	for _, cm := range d.comments {
		if cm.ID == id {
			orig = cm.Text
			break
		}
	}
	d.clearCommentBlockEdit()
	if text == "" {
		b.toast("无法保存评论", "评论内容不能为空")
		return
	}
	if text == strings.TrimSpace(orig) {
		return
	}
	// Optimistic local update so switching blocks sees the new text immediately.
	for i := range d.comments {
		if d.comments[i].ID == id {
			d.comments[i].Text = text
			break
		}
	}
	b.commenting = true
	go func() {
		cm, err := b.api.UpdateComment(taskID, id, text)
		b.post(func() {
			b.commenting = false
			if err != nil {
				b.toast("编辑评论失败", err.Error())
				return
			}
			if b.detail == d {
				d.comments = applyCommentMutation(d.comments, model.EventCommentUpdated, cm)
			}
			for i := range b.tasks {
				if b.tasks[i].ID == taskID {
					b.tasks[i].Comments = applyCommentMutation(b.tasks[i].Comments, model.EventCommentUpdated, cm)
				}
			}
		})
	}()
}

func (b *board) beginCommentReply(cm model.Comment) {
	if b.detail == nil || b.commenting {
		return
	}
	b.detail.commentParentID = cm.ID
	b.detail.commentParentAuthor = cm.Author
	b.detail.commentFocus = 1
}

func (b *board) saveCommentEdit() {
	d := b.detail
	if d == nil || d.commentEditID == "" || b.commenting {
		return
	}
	text := strings.TrimSpace(d.commentEditText)
	if text == "" {
		b.toast("无法保存评论", "评论内容不能为空")
		return
	}
	id, taskID := d.commentEditID, d.id
	b.commenting = true
	go func() {
		cm, err := b.api.UpdateComment(taskID, id, text)
		b.post(func() {
			b.commenting = false
			if err != nil {
				b.toast("编辑评论失败", err.Error())
				return
			}
			if b.detail == d {
				d.comments = applyCommentMutation(d.comments, model.EventCommentUpdated, cm)
				d.commentEditID, d.commentEditText = "", ""
			}
			for i := range b.tasks {
				if b.tasks[i].ID == taskID {
					b.tasks[i].Comments = applyCommentMutation(b.tasks[i].Comments, model.EventCommentUpdated, cm)
				}
			}
		})
	}()
}

func (b *board) deleteComment(cm model.Comment) {
	d := b.detail
	if d == nil || cm.Author != b.agent || b.commenting {
		return
	}
	taskID := d.id
	b.commenting = true
	go func() {
		err := b.api.DeleteComment(taskID, cm.ID)
		b.post(func() {
			b.commenting = false
			if err != nil {
				b.toast("删除评论失败", err.Error())
				return
			}
			if b.detail == d {
				d.comments = applyCommentMutation(d.comments, model.EventCommentDeleted, cm)
				if d.commentEditID == cm.ID {
					d.commentEditID, d.commentEditText = "", ""
				}
			}
			for i := range b.tasks {
				if b.tasks[i].ID == taskID {
					b.tasks[i].Comments = applyCommentMutation(b.tasks[i].Comments, model.EventCommentDeleted, cm)
				}
			}
		})
	}()
}

func (b *board) submitComment(followup bool) {
	d := b.detail
	if d == nil || b.commenting || d.commentImage.busy() {
		return
	}
	draft := d.comment
	files := append([]string(nil), d.commentFiles...)
	images := append([]string(nil), d.commentImages...)
	text := commentWithImages(withWorkspaceMentions(draft, files), images)
	if text == "" {
		return
	}
	// An @mention is an explicit address, so sending it also wakes that agent;
	// a plain comment on a task with a returned original session wakes that
	// session too, so the user does not have to address the agent again.
	mention := d.commentMention
	parentID, parentAuthor := d.commentParentID, d.commentParentAuthor
	autoFollowup := followup || mention != "" || taskHasReturnedSession(d.snap)
	d.comment = ""
	d.commentFiles = nil
	d.commentMention = ""
	d.commentParentID, d.commentParentAuthor = "", ""
	d.mentionOpen = false
	b.commenting = true
	go func() {
		cm, err := b.api.AddCommentWithParent(d.id, text, mention, parentID)
		b.post(func() {
			b.commenting = false
			if err != nil {
				if b.detail == d && d.comment == "" {
					d.comment = draft
					d.commentFiles = files
					d.commentMention = mention
					d.commentParentID, d.commentParentAuthor = parentID, parentAuthor
				}
				b.toast("评论失败", err.Error())
				return
			}
			if b.detail == d {
				d.commentImages = nil
				d.commentFiles = nil
				// Preserve the normal close behavior for any title or body edits
				// made before the comment was submitted.
				b.saveDetail()
			}
		})
		if err == nil && autoFollowup {
			if followupErr := b.api.ContinueComment(d.id, cm.ID); followupErr != nil {
				b.post(func() { b.toast("评论已保存，AI 跟进失败", followupErr.Error()) })
			}
		}
	}()
}

func taskHasReturnedSession(task model.Task) bool {
	run := task.Execution
	if run == nil || strings.TrimSpace(run.SessionID) == "" {
		return false
	}
	switch run.Status {
	case "returned", "succeeded", "failed", "detached":
		return true
	default:
		return false
	}
}

func (b *board) continueComment(taskID, commentID string) {
	if b.commenting {
		return
	}
	b.commenting = true
	go func() {
		err := b.api.ContinueComment(taskID, commentID)
		b.post(func() {
			b.commenting = false
			if err != nil {
				b.toast("AI 跟进失败", err.Error())
			}
		})
	}()
}

// playColumnChangeCue soft-clicks on a real column change; a landing in the
// done column gets the done chime instead. Load/openProject never call this.
// Per-task debounce suppresses the SSE echo after an optimistic replace
// (and any rapid remaps) so one move never rings twice.
func (b *board) playColumnChangeCue(taskID, toColumnID string) {
	now := time.Now()
	if b.sfxMuteUntil != nil {
		if until, ok := b.sfxMuteUntil[taskID]; ok && now.Before(until) {
			return
		}
	} else {
		b.sfxMuteUntil = map[string]time.Time{}
	}
	b.sfxMuteUntil[taskID] = now.Add(500 * time.Millisecond)
	if b.columnHasRole(toColumnID, model.ColumnRoleDone) {
		playDoneCue()
		return
	}
	playMoveCue()
}

// openProjectEdit opens the project form pre-filled for editing p: same
// sheet as create, but 回车/保存 writes the change back instead.
// deleteProject calls the API in the background; the project.changed event
// that comes back refreshes the sidebar and drops a deleted current project
// back to the global view.
func (b *board) deleteProject(p model.Project) {
	go func() {
		if err := b.api.DeleteProject(p.ID); err != nil {
			b.post(func() { b.toast("删除失败", err.Error()) })
		}
	}()
}

func (b *board) openProjectEdit(p model.Project) {
	gitOn := b.prefs.ProjectGitCommitOnDone != nil && b.prefs.ProjectGitCommitOnDone[p.ID]
	b.form = &form{
		open:             true,
		isTask:           false,
		name:             p.Name,
		workspacePath:    p.WorkspacePath,
		color:            p.Color,
		gitCommitOnDone:  gitOn,
		editingProjectID: p.ID,
	}
}

// saveProjectEdit writes an edited project's name and workspace path back.
// The form closes on success; a failure keeps it open with the input
// intact so the user can correct it.
func (b *board) saveProjectEdit() {
	f := b.form
	if f == nil || f.editingProjectID == "" || f.creating {
		return
	}
	name := strings.TrimSpace(f.name)
	if name == "" {
		b.toast("无法保存", "项目名不能为空。")
		return
	}
	path := strings.TrimSpace(f.workspacePath)
	color := strings.TrimSpace(f.color)
	f.creating = true
	go func() {
		p, err := b.api.UpdateProjectWithColor(f.editingProjectID, name, path, color)
		if err != nil {
			b.post(func() {
				f.creating = false
				b.toast("保存失败", err.Error())
			})
			return
		}
		ps, perr := b.api.Projects()
		b.post(func() {
			if perr == nil {
				b.projects = ps
			} else {
				for i := range b.projects {
					if b.projects[i].ID == p.ID {
						b.projects[i] = p
						break
					}
				}
			}
			if b.form == f {
				f.creating = false
				f.open = false
			}
			b.applyProjectGitCommitOnDone(p.ID, f.gitCommitOnDone)
			b.okToast("已保存", "项目「"+clip(p.Name, 24)+"」已更新")
		})
	}()
}

// applyProjectGitCommitOnDone writes or clears the per-project
// 「验收完成时自动 Git 提交」flag in uiPrefs.
func (b *board) applyProjectGitCommitOnDone(projectID string, on bool) {
	if projectID == "" {
		return
	}
	if on {
		if b.prefs.ProjectGitCommitOnDone == nil {
			b.prefs.ProjectGitCommitOnDone = map[string]bool{}
		}
		b.prefs.ProjectGitCommitOnDone[projectID] = true
	} else if b.prefs.ProjectGitCommitOnDone != nil {
		delete(b.prefs.ProjectGitCommitOnDone, projectID)
		if len(b.prefs.ProjectGitCommitOnDone) == 0 {
			b.prefs.ProjectGitCommitOnDone = nil
		}
	}
	_ = saveUIPrefs(b.prefs)
}

func (b *board) createProject(stayOpen bool) {
	f := b.form
	if f == nil || f.creating {
		return
	}
	name := strings.TrimSpace(f.name)
	if name == "" {
		b.toast("无法创建", "项目名不能为空。")
		return
	}
	path := strings.TrimSpace(f.workspacePath)
	color := strings.TrimSpace(f.color)
	f.creating = true
	go func() {
		p, err := b.api.CreateProjectWithColor(name, path, color)
		if err != nil {
			b.post(func() {
				f.creating = false
				b.toast("创建失败", err.Error())
			})
			return
		}
		ps, perr := b.api.Projects()
		b.post(func() {
			if perr == nil {
				b.projects = ps
			} else {
				b.projects = append(b.projects, p)
			}
			if b.form != f {
				return
			}
			f.creating = false
			if stayOpen {
				f.name = ""
				f.workspacePath = ""
				return
			}
			f.open = false
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
			b.invalidateTaskViewCache()
			b.sortTasks()
			return
		}
	}
	b.reconcilePlacement(t)
	b.tasks = append(b.tasks, t)
	b.invalidateTaskViewCache()
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
