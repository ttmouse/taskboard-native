// Package model defines the taskboard domain types shared by the store,
// the HTTP service, the CLI, the MCP server and the GUI.
package model

import (
	"strings"
	"time"
)

// IDs are short random hex strings, prefixed by kind for readability.
type Project struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	WorkspacePath string `json:"workspacePath,omitempty"`
	// Color is the project's card accent color in the global view, a "#rrggbb"
	// hex string, or empty to let the window assign a stable palette slot.
	Color     string    `json:"color,omitempty"`
	Columns   []Column  `json:"columns"`
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt is the project record's own last edit time.
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
	// ActiveAt is the newest updated_at among the project's tasks ("" when
	// the project has no tasks). The sidebar's「按更新时间排序」orders by it,
	// so recently touched projects float to the top.
	ActiveAt time.Time `json:"activeAt,omitempty"`
}

type Column struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Role names the workflow step this column plays, so tools never have
	// to recognize a column by its (renamable) display name. Empty means
	// the column carries no meaning.
	Role string `json:"role"`
}

// Column roles: the four fixed workflow steps of a board.
const (
	ColumnRoleIntake = "intake"
	ColumnRoleDoing  = "doing"
	ColumnRoleReview = "review"
	ColumnRoleDone   = "done"
)

// ColumnRoles lists every valid role in board order. The empty string is
// valid too ("no meaning") but is not a role and can be held many times.
var ColumnRoles = []string{ColumnRoleIntake, ColumnRoleDoing, ColumnRoleReview, ColumnRoleDone}

// ValidColumnRole reports whether role is one of ColumnRoles or empty.
func ValidColumnRole(role string) bool {
	if role == "" {
		return true
	}
	for _, r := range ColumnRoles {
		if r == role {
			return true
		}
	}
	return false
}

// legacyColumnNames is the fixed Chinese name each role used to be found by
// before roles existed. It is the compatibility fallback for databases that
// were never backfilled and for projects renamed before the upgrade.
var legacyColumnNames = map[string]string{
	ColumnRoleIntake: "待办",
	ColumnRoleDoing:  "进行中",
	ColumnRoleReview: "评审",
	ColumnRoleDone:   "完成",
}

// LegacyColumnName returns the name older boards hardcoded for role, or ""
// when the role has no legacy name.
func LegacyColumnName(role string) string { return legacyColumnNames[role] }

// ColumnByRole resolves the column playing role. An explicit role always
// wins; when the board carries none, the legacy Chinese name keeps old
// databases and pre-upgrade renamed projects behaving exactly as before.
func ColumnByRole(columns []Column, role string) (Column, bool) {
	if role != "" {
		for _, c := range columns {
			if c.Role == role {
				return c, true
			}
		}
	}
	name, ok := legacyColumnNames[role]
	if !ok {
		return Column{}, false
	}
	for _, c := range columns {
		if c.Name == name {
			return c, true
		}
	}
	return Column{}, false
}

// ColumnByRole resolves the project's column playing role.
func (p Project) ColumnByRole(role string) (Column, bool) {
	return ColumnByRole(p.Columns, role)
}

// HasColumn reports whether the project defines the column ID.
func (p Project) HasColumn(id string) bool {
	for _, c := range p.Columns {
		if c.ID == id {
			return true
		}
	}
	return false
}

type Task struct {
	ID          string     `json:"id"`
	Number      int        `json:"number"`
	ProjectID   string     `json:"projectId"`
	ColumnID    string     `json:"columnId"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Assignee    string     `json:"assignee,omitempty"`
	Position    float64    `json:"position"`
	Version     int        `json:"version"`
	Archived    bool       `json:"archived"`
	Comments    []Comment  `json:"comments,omitempty"`
	Execution   *Execution `json:"execution,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	UpdatedBy   string     `json:"updatedBy,omitempty"`
	CompletedBy string     `json:"completedBy,omitempty"`
	// Relations holds parent/sub-issue and blocking links (DSH-compatible).
	// Always populated on GetTask/List/Board when the store enriches the task.
	Relations TaskRelations `json:"relations"`
}

// TaskRelationSummary is the compact card pointer used inside Relations.
type TaskRelationSummary struct {
	ID         string `json:"id"`
	Number     int    `json:"number"`
	Title      string `json:"title"`
	ColumnID   string `json:"columnId"`
	ColumnRole string `json:"columnRole,omitempty"`
	Archived   bool   `json:"archived,omitempty"`
}

// TaskRelations mirrors dsh-taskboard's relations object on a Task.
// parent: the single parent of this card; subIssues: children whose parent is this card.
// blockedBy / blocks: directed blocking edges stored as relation_type=blocks.
// related: undirected related links.
type TaskRelations struct {
	Parent    *TaskRelationSummary  `json:"parent,omitempty"`
	SubIssues []TaskRelationSummary `json:"subIssues,omitempty"`
	BlockedBy []TaskRelationSummary `json:"blockedBy,omitempty"`
	Blocks    []TaskRelationSummary `json:"blocks,omitempty"`
	Related   []TaskRelationSummary `json:"related,omitempty"`
}


// RelationResult is returned by add/remove relation endpoints.
type RelationResult struct {
	Task        Task `json:"task"`
	RelatedTask Task `json:"relatedTask"`
}
// CompletingAgent recognizes the dispatcher's successful result comments.
// Ordinary comments, failures and dispatch requests do not establish completion.
func CompletingAgent(c Comment) string {
	// Older dispatcher versions attributed comments to the initiating user
	// (often gui). The structured result header carries the actual channel.
	if !strings.Contains(c.Text, "）。输出：\n\n") {
		return ""
	}
	for _, channel := range []struct{ runtime, label string }{
		{"codex", "Codex"}, {"dsh", "DSH"}, {"pi", "Pi"},
	} {
		if strings.HasPrefix(c.Text, channel.runtime+" 执行完成（") {
			return channel.label
		}
	}
	return ""
}

type Comment struct {
	ID     string `json:"id"`
	Author string `json:"author"`
	Text   string `json:"text"`
	// Mention is the execution channel the author addressed with @ (dsh,
	// codex, pi, claude). Empty means "no address": the follow-up goes back
	// to whatever session already owns the card. It is stored as a field and
	// never parsed out of Text, so an agent writing an @ in prose is not an
	// instruction.
	Mention         string    `json:"mention,omitempty"`
	ParentCommentID string    `json:"parentCommentId,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
}

// Execution is independent of the card's workflow column. An uncertain or
// disconnected run keeps its identity so a retry cannot start a second writer.
type Execution struct {
	TaskID         string `json:"taskId"`
	SessionID      string `json:"sessionId"`
	Status         string `json:"status"`
	Detail         string `json:"detail,omitempty"`
	Runtime        string `json:"runtime,omitempty"`
	FollowupID     string `json:"followupId,omitempty"`
	PreviousTurnID string `json:"previousTurnId,omitempty"`
}

func (e Execution) Channel() string {
	if e.Runtime == "" {
		return "dsh"
	}
	return e.Runtime
}

// endedStatuses end a run: the record stops holding the card's write lock, so
// the card may be dispatched or followed up again. released is the convergence
// result for a session that is confirmed unreachable; disconnected is only a
// temporary state and keeps the writer so a retry cannot start a second one.
// detached stays resumable — the desktop channel uses it for "tracking stopped,
// session still alive", so it must not release the lock.
var endedStatuses = map[string]bool{
	"succeeded": true,
	"returned":  true,
	"failed":    true,
	"released":  true,
}

func (e Execution) Active() bool {
	if endedStatuses[e.Status] {
		return false
	}
	// detached means "tracking stopped, session still alive" — a state only the
	// desktop channel can resume. A Herdr terminal cannot be resumed that way, so
	// a detached Herdr record is really a released one under an older spelling.
	if e.Status == "detached" && strings.HasPrefix(strings.TrimSpace(e.SessionID), "herdr:") {
		return false
	}
	return true
}

// Ended reports a converged session: whatever the outcome, the turn is over
// and the record no longer holds the card's write lock.
func (e Execution) Ended() bool { return endedStatuses[e.Status] }

// Releasable reports whether a human may converge this record to detached. A
// run that may still be writing has to be stopped in its own channel first;
// releasing it here would let a new dispatch start a second writer on the same
// work.
func (e Execution) Releasable() bool {
	switch e.Status {
	case "submitting", "running", "waiting_approval":
		return false
	}
	return true
}

// Stalled reports a CLI run whose process died with the service. The desktop
// channel keeps a live session behind "disconnected" and resumes tracking, but
// a codex/pi process is gone for good: nothing will ever move this record
// again, so it must not block a fresh dispatch or a comment follow-up.
func (e Execution) Stalled() bool {
	return e.Channel() != "dsh" && !strings.HasPrefix(e.SessionID, "herdr:") && e.Status == "disconnected"
}

// ProjectRun is one project-level handoff. TaskIDs is fixed when the run
// starts; Cursor is the next card, and CurrentTaskID is the one in flight.
// Status is running, stopped, completed or failed. Detail says why it stopped.
type ProjectRun struct {
	ProjectID     string    `json:"projectId"`
	Runtime       string    `json:"runtime"`
	Status        string    `json:"status"`
	TaskIDs       []string  `json:"taskIds"`
	Cursor        int       `json:"cursor"`
	CurrentTaskID string    `json:"currentTaskId,omitempty"`
	Detail        string    `json:"detail,omitempty"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

func (r ProjectRun) Running() bool { return r.Status == "running" }

// Bubble is one "execution host → deep link" mapping entry, display-only:
// the GUI renders the table; jumping is out of scope for this build.
type Bubble struct {
	Host  string `json:"host"`
	URL   string `json:"url"`
	Label string `json:"label,omitempty"`
}

// Data is the persisted document.
type Data struct {
	Projects []Project `json:"projects"`
	Tasks    []Task    `json:"tasks"`
	Bubbles  []Bubble  `json:"bubbles"`
	EventSeq int64     `json:"eventSeq"`
}

// Event types pushed over SSE.
const (
	EventTaskCreated    = "task.created"
	EventTaskUpdated    = "task.updated"
	EventTaskMoved      = "task.moved"
	EventTaskArchived   = "task.archived"
	EventCommentAdded   = "comment.added"
	EventCommentUpdated = "comment.updated"
	EventCommentDeleted = "comment.deleted"
	EventProjectChange  = "project.changed"
	EventBubbleChange   = "bubble.changed"
	EventBoardSnapshot  = "board.snapshot"
)

// ProjectBoard is a complete live-task snapshot for one project.
type ProjectBoard struct {
	Project Project  `json:"project"`
	Columns []Column `json:"columns"`
	Tasks   []Task   `json:"tasks"`
}

// BoardSnapshot is sent when an SSE connection starts. It closes any event
// gap from a previous connection before incremental events are applied.
type BoardSnapshot struct {
	Seq      int64          `json:"seq"`
	Projects []Project      `json:"projects"`
	Boards   []ProjectBoard `json:"boards"`
}

// Event is the SSE payload. Task is a full snapshot; clients merge by
// Version and, on conflict, re-fetch the single task.
type Event struct {
	Seq       int64    `json:"seq"`
	Type      string   `json:"type"`
	ProjectID string   `json:"projectId"`
	ID        string   `json:"id"` // task or project id
	Version   int      `json:"version,omitempty"`
	By        string   `json:"by,omitempty"`
	Task      *Task    `json:"task,omitempty"`
	Comment   *Comment `json:"comment,omitempty"`
	Column    *struct {
		FromID string `json:"fromId"`
		ToID   string `json:"toId"`
	} `json:"column,omitempty"`
	Snapshot *BoardSnapshot `json:"snapshot,omitempty"`
}
