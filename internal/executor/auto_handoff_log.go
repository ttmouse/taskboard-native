package executor

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"taskboard-native/internal/model"
)

// handoffEvent is a compact, machine-readable record of one dispatch or
// automatic handoff decision. It is the operator's side of a card's history:
// comments tell a person what happened, this log tells whoever debugs it why.
// It intentionally excludes task text and agent output, and Message carries
// only a clipped single-line diagnostic, never credentials.
type handoffEvent struct {
	Timestamp      time.Time `json:"timestamp"`
	Event          string    `json:"event"`
	ProjectID      string    `json:"projectId,omitempty"`
	Channel        string    `json:"channel,omitempty"`
	Runtime        string    `json:"runtime,omitempty"`
	Actor          string    `json:"actor,omitempty"`
	Message        string    `json:"message,omitempty"`
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

func errorType(err error) string {
	if err == nil {
		return ""
	}
	return reflect.TypeOf(err).String()
}

type autoHandoffLog struct {
	path string
	mu   sync.Mutex
}

func newAutoHandoffLog(path string) *autoHandoffLog {
	if path == "" {
		return nil
	}
	return &autoHandoffLog{path: path}
}

func (l *autoHandoffLog) append(event handoffEvent) error {
	if l == nil {
		return nil
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	line, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode automatic handoff event: %w", err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return fmt.Errorf("create automatic handoff log directory: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open automatic handoff log: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write automatic handoff log: %w", err)
	}
	return nil
}

func (d *Dispatcher) logHandoff(event handoffEvent) {
	if err := d.handoffLog.append(event); err != nil {
		log.Printf("taskboard: automatic handoff log: %v", err)
	}
}

// dispatchLogMaxMessage keeps one diagnostic readable in a single log line
// while leaving room for a CLI failure excerpt.
const dispatchLogMaxMessage = 800

// logDispatch records one dispatch decision for later diagnosis. Card comments
// stay user-facing; this is where channel failures, fallbacks, and status
// transitions live.
func (d *Dispatcher) logDispatch(event, taskID, channel, runtime, actor, reason, message string) {
	d.logHandoff(handoffEvent{
		Event:   event,
		TaskID:  taskID,
		Channel: channel,
		Runtime: runtime,
		Actor:   actor,
		Reason:  reason,
		Message: clipMessage(message),
	})
}

// recordTrackedStatus logs one status transition of a tracked execution. It is
// the only place intermediate states survive: the executions row keeps just the
// latest one.
func (d *Dispatcher) recordTrackedStatus(run *model.Execution, channel string, last *string) {
	if run.Status == *last {
		return
	}
	*last = run.Status
	d.logHandoff(handoffEvent{
		Event:          "dispatch.status",
		TaskID:         run.TaskID,
		Channel:        channel,
		Runtime:        run.Runtime,
		ExecutionState: run.Status,
		Message:        clipMessage(run.Detail),
	})
}

// clipMessage flattens a diagnostic to one bounded line. It counts runes, so a
// Chinese diagnostic never gets cut inside a character.
func clipMessage(message string) string {
	line := strings.Join(strings.Fields(message), " ")
	runes := []rune(line)
	if len(runes) > dispatchLogMaxMessage {
		return string(runes[:dispatchLogMaxMessage]) + "…"
	}
	return line
}
