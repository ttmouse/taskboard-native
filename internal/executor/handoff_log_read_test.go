package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadTaskHandoffEventsFiltersByTaskID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "taskboard.db.auto-handoff.jsonl")
	lines := "" +
		`{"timestamp":"2026-10-31T12:00:00.000000Z","event":"run.queue_scanned","projectId":"p1","runtime":"codex","taskIds":["other","t-keep"],"reason":"stalled_doing=0 ready=1 blocked=false"}` + "\n" +
		`{"timestamp":"2026-10-31T12:00:01.000000Z","event":"task.claimed","projectId":"p1","runtime":"codex","taskId":"t-keep","reason":"moved_to_doing"}` + "\n" +
		`{"timestamp":"2026-10-31T12:00:02.000000Z","event":"task.dispatch_started","projectId":"p1","runtime":"codex","taskId":"t-keep"}` + "\n" +
		`{"timestamp":"2026-10-31T12:00:03.000000Z","event":"task.dispatch_accepted","projectId":"p1","runtime":"codex","taskId":"t-other"}` + "\n" +
		`{"timestamp":"2026-10-31T12:01:00.000000Z","event":"task.moved_to_review","projectId":"p1","runtime":"codex","taskId":"t-keep","reason":"execution_ended_human_acceptance_required"}` + "\n" +
		`not-json` + "\n"
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ReadTaskHandoffEvents(path, "t-keep", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4 (taskId + taskIds match; skip other task + corrupt)", len(got))
	}
	if got[0].Event != "run.queue_scanned" || got[1].Event != "task.claimed" || got[3].Event != "task.moved_to_review" {
		t.Fatalf("order = %#v", got)
	}

	empty, err := ReadTaskHandoffEvents(path, "missing", 100)
	if err != nil || len(empty) != 0 {
		t.Fatalf("missing task: %v %#v", err, empty)
	}
	missingFile, err := ReadTaskHandoffEvents(filepath.Join(dir, "nope.jsonl"), "t-keep", 100)
	if err != nil || missingFile != nil {
		t.Fatalf("missing file: %v %#v", err, missingFile)
	}
}

func TestReadTaskHandoffEventsCapsToLimitOldestToNewest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	var raw []byte
	for i := 0; i < 5; i++ {
		line := fmt.Sprintf(`{"timestamp":"2026-10-31T12:00:%02d.000000Z","event":"task.claimed","taskId":"t1"}`+"\n", i)
		raw = append(raw, line...)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadTaskHandoffEvents(path, "t1", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Timestamp.Second() != 3 || got[1].Timestamp.Second() != 4 {
		t.Fatalf("window = %v %v", got[0].Timestamp, got[1].Timestamp)
	}
}

func TestSummarizeHandoffDuration(t *testing.T) {
	start := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	mid := start.Add(30 * time.Second)
	end := start.Add(2*time.Minute + 5*time.Second)
	events := []HandoffEvent{
		{Timestamp: start, Event: "task.claimed", Runtime: "codex", TaskID: "t1", Reason: "moved_to_doing"},
		{Timestamp: mid, Event: "task.dispatch_started", Runtime: "codex", TaskID: "t1"},
		{Timestamp: mid.Add(time.Second), Event: "task.dispatch_accepted", Runtime: "codex", TaskID: "t1"},
		{Timestamp: end, Event: "task.moved_to_review", Runtime: "codex", TaskID: "t1", Reason: "execution_ended_human_acceptance_required"},
	}
	sum := SummarizeHandoff(events)
	if sum.Runtime != "codex" {
		t.Fatalf("runtime = %q", sum.Runtime)
	}
	if sum.StartedAt == nil || !sum.StartedAt.Equal(start) {
		t.Fatalf("startedAt = %v, want first claimed", sum.StartedAt)
	}
	if sum.EndedAt == nil || !sum.EndedAt.Equal(end) {
		t.Fatalf("endedAt = %v", sum.EndedAt)
	}
	wantMs := end.Sub(start).Milliseconds()
	if sum.DurationMs != wantMs {
		t.Fatalf("durationMs = %d, want %d", sum.DurationMs, wantMs)
	}
	if sum.LastLabel != "移入等你" || sum.Outcome != "移入等你" {
		t.Fatalf("labels = %+v", sum)
	}
	if sum.Reason != "execution_ended_human_acceptance_required" {
		t.Fatalf("reason = %q", sum.Reason)
	}
	if got := FormatHandoffDuration(sum.DurationMs); got != "2m5s" {
		t.Fatalf("FormatHandoffDuration = %q", got)
	}

	failed := SummarizeHandoff([]HandoffEvent{
		{Timestamp: start, Event: "task.dispatch_started", Runtime: "pi", TaskID: "t2"},
		{Timestamp: start.Add(time.Second), Event: "task.dispatch_failed", Runtime: "pi", TaskID: "t2", Reason: "dispatch_error"},
	})
	if failed.Outcome != "派发失败" || failed.DurationMs != 1000 {
		t.Fatalf("failed summary = %+v", failed)
	}
}
