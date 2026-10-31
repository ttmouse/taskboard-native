package executor

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"
)

// handoffEvent is a compact, machine-readable record of one automatic handoff
// decision. It intentionally excludes task text, workspace paths, and raw
// errors because those may contain private data or credentials.
type handoffEvent struct {
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
