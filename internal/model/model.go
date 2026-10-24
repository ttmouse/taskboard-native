// Package model defines the taskboard domain types shared by the store,
// the HTTP service, the CLI, the MCP server and the GUI.
package model

import "time"

// IDs are short random hex strings, prefixed by kind for readability.
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Columns   []Column  `json:"columns"`
	CreatedAt time.Time `json:"createdAt"`
}

type Column struct {
	ID   string `json:"id"`
	Name string `json:"name"`
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
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	ColumnID    string    `json:"columnId"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Assignee    string    `json:"assignee,omitempty"`
	Position    float64   `json:"position"`
	Version     int       `json:"version"`
	Archived    bool      `json:"archived"`
	Comments    []Comment `json:"comments,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	UpdatedBy   string    `json:"updatedBy,omitempty"`
}

type Comment struct {
	ID        string    `json:"id"`
	Author    string    `json:"author"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"createdAt"`
}

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
	EventTaskCreated   = "task.created"
	EventTaskUpdated   = "task.updated"
	EventTaskMoved     = "task.moved"
	EventTaskArchived  = "task.archived"
	EventCommentAdded  = "comment.added"
	EventProjectChange = "project.changed"
	EventBubbleChange  = "bubble.changed"
)

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
}
