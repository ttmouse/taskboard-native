package main

import (
	"strings"
	"testing"

	"taskboard-native/internal/model"
)

func TestLiveCompletionAttribution(t *testing.T) {
	f := seedBoard()
	b := newBoard(f)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return len(b.tasks) > 0 })
	for _, channel := range []struct{ author, runtime, label string }{
		{"Codex", "codex", "Codex"}, {"DSH", "dsh", "DSH"}, {"Pi Agent", "pi", "Pi"},
	} {
		b.applyEvent(model.Event{Type: model.EventCommentAdded, ProjectID: "local", ID: "t-1",
			Comment: &model.Comment{ID: channel.runtime, Author: channel.author, Text: channel.runtime + " 执行完成（1s）。输出：\n\n结果"}})
		task := b.task("t-1")
		if task.CompletedBy != channel.label {
			t.Fatalf("live completion: %+v", task)
		}
		task.UpdatedBy = "gui"
		line := provenance(task)
		if !strings.Contains(line, channel.label+" 完成") || strings.Contains(line, "gui") {
			t.Fatalf("provenance = %q", line)
		}
	}
	b.applyEvent(model.Event{Type: model.EventCommentAdded, ProjectID: "local", ID: "t-1",
		Comment: &model.Comment{ID: "failure", Author: "Codex", Text: "codex 执行失败（exit 1）。"}})
	if b.task("t-1").CompletedBy != "Pi" {
		t.Fatal("failed execution replaced successful attribution")
	}
}
