package executor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// DefaultHandoffLogLimit caps events returned for one task (oldest→newest).
const DefaultHandoffLogLimit = 100

// HandoffEvent is one automatic-handoff JSONL record. Fields match the
// writer in auto_handoff_log.go; title/body are intentionally absent.
type HandoffEvent struct {
	Timestamp      time.Time `json:"timestamp"`
	Event          string    `json:"event"`
	ProjectID      string    `json:"projectId,omitempty"`
	Runtime        string    `json:"runtime,omitempty"`
	Actor          string    `json:"actor,omitempty"`
	RunStatus      string    `json:"runStatus,omitempty"`
	TaskID         string    `json:"taskId,omitempty"`
	TaskIDs        []string  `json:"taskIds,omitempty"`
	Cursor         int       `json:"cursor,omitempty"`
	ColumnID       string    `json:"columnId,omitempty"`
	SessionID      string    `json:"sessionId,omitempty"`
	ExecutionState string    `json:"executionStatus,omitempty"`
	Reason         string    `json:"reason,omitempty"`
	ErrorType      string    `json:"errorType,omitempty"`
}

// HandoffSummary is a compact roll-up for the task detail UI.
type HandoffSummary struct {
	Runtime   string     `json:"runtime,omitempty"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
	// DurationMs is EndedAt−StartedAt in milliseconds when both are set.
	DurationMs int64  `json:"durationMs,omitempty"`
	LastEvent  string `json:"lastEvent,omitempty"`
	LastLabel  string `json:"lastLabel,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Outcome    string `json:"outcome,omitempty"`
}

// HandoffLogView is the GET /api/tasks/{id}/handoff-log body.
type HandoffLogView struct {
	TaskID  string         `json:"taskId"`
	Summary HandoffSummary `json:"summary"`
	Events  []HandoffEvent `json:"events"`
}

// ReadTaskHandoffEvents scans a JSONL handoff log and returns events that
// mention taskID (as taskId or inside taskIds), oldest→newest, capped at
// limit (DefaultHandoffLogLimit when limit ≤ 0). Missing files yield an
// empty slice, not an error.
func ReadTaskHandoffEvents(path, taskID string, limit int) ([]HandoffEvent, error) {
	taskID = strings.TrimSpace(taskID)
	if path == "" || taskID == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = DefaultHandoffLogLimit
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open handoff log: %w", err)
	}
	defer f.Close()

	var matched []HandoffEvent
	sc := bufio.NewScanner(f)
	// Some lines can be long (taskIds arrays); raise the default token size.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev HandoffEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			// Skip corrupt lines so a partial write does not break the viewer.
			continue
		}
		if !eventMentionsTask(ev, taskID) {
			continue
		}
		matched = append(matched, ev)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan handoff log: %w", err)
	}
	if len(matched) > limit {
		matched = matched[len(matched)-limit:]
	}
	return matched, nil
}

func eventMentionsTask(ev HandoffEvent, taskID string) bool {
	if ev.TaskID == taskID {
		return true
	}
	for _, id := range ev.TaskIDs {
		if id == taskID {
			return true
		}
	}
	return false
}

// SummarizeHandoff derives channel / start / end / duration / last label
// from a task's events (already filtered, oldest→newest).
func SummarizeHandoff(events []HandoffEvent) HandoffSummary {
	var sum HandoffSummary
	if len(events) == 0 {
		return sum
	}
	last := events[len(events)-1]
	sum.LastEvent = last.Event
	sum.LastLabel = HandoffEventLabel(last.Event)
	if last.Reason != "" {
		sum.Reason = last.Reason
	}

	for _, ev := range events {
		if sum.Runtime == "" && ev.Runtime != "" {
			sum.Runtime = ev.Runtime
		}
		if sum.StartedAt == nil && isHandoffStart(ev.Event) {
			t := ev.Timestamp
			sum.StartedAt = &t
		}
		if isHandoffEnd(ev.Event) {
			t := ev.Timestamp
			sum.EndedAt = &t
			sum.Outcome = HandoffEventLabel(ev.Event)
			if ev.Reason != "" {
				sum.Reason = ev.Reason
			}
		}
	}
	if sum.StartedAt != nil && sum.EndedAt != nil && !sum.EndedAt.Before(*sum.StartedAt) {
		sum.DurationMs = sum.EndedAt.Sub(*sum.StartedAt).Milliseconds()
	}
	return sum
}

func isHandoffStart(event string) bool {
	switch event {
	case "task.dispatch_started", "task.claimed", "task.claimed_existing_execution":
		return true
	default:
		return false
	}
}

func isHandoffEnd(event string) bool {
	switch event {
	case "task.moved_to_review", "task.dispatch_failed", "task.execution_converged":
		return true
	default:
		return false
	}
}

// HandoffEventLabel maps a machine event name to a short Chinese UI label.
func HandoffEventLabel(event string) string {
	switch event {
	case "task.claimed":
		return "认领"
	case "task.claimed_existing_execution":
		return "认领已有会话"
	case "task.claim_failed":
		return "认领失败"
	case "task.dispatch_started":
		return "开始派发"
	case "task.dispatch_accepted":
		return "已接受"
	case "task.dispatch_failed":
		return "派发失败"
	case "task.execution_converged":
		return "执行收敛"
	case "task.execution_not_converged":
		return "未收敛"
	case "task.moved_to_review":
		return "移入等你"
	case "task.left_queue":
		return "离开队列"
	case "task.approval_parked":
		return "待审批"
	case "task.approval_waiting":
		return "等审批列"
	case "task.approval_move_failed":
		return "审批移列失败"
	case "task.review_move_failed":
		return "移入等你失败"
	case "run.recovery_observed":
		return "恢复观察"
	case "run.recovery_session_active":
		return "恢复会话中"
	case "run.recovery_session_ended":
		return "恢复会话结束"
	case "run.queue_scanned":
		return "扫描队列"
	case "run.waiting":
		return "等待"
	default:
		if event == "" {
			return ""
		}
		// Fall back to the trailing segment so unknown events stay short.
		if i := strings.LastIndex(event, "."); i >= 0 && i+1 < len(event) {
			return event[i+1:]
		}
		return event
	}
}

// FormatHandoffDuration renders a millisecond duration for the detail UI.
func FormatHandoffDuration(ms int64) string {
	if ms <= 0 {
		return ""
	}
	d := time.Duration(ms) * time.Millisecond
	if d < time.Second {
		return fmt.Sprintf("%dms", ms)
	}
	if d < time.Minute {
		sec := d / time.Second
		return fmt.Sprintf("%ds", sec)
	}
	min := d / time.Minute
	sec := (d % time.Minute) / time.Second
	if sec == 0 {
		return fmt.Sprintf("%dm", min)
	}
	return fmt.Sprintf("%dm%ds", min, sec)
}
