package store

import (
	"testing"

	"taskboard-native/internal/model"
)

func TestCompletionAttributionSurvivesManualMove(t *testing.T) {
	for _, channel := range []struct{ author, runtime, label string }{
		{"Codex", "codex", "Codex"}, {"DSH", "dsh", "DSH"}, {"Pi Agent", "pi", "Pi"},
		{"gui", "codex", "Codex"}, {"gui", "pi", "Pi"}, {"豆皮", "dsh", "DSH"},
	} {
		t.Run(channel.runtime, func(t *testing.T) {
			s := openTemp(t)
			project, err := s.Project("local")
			if err != nil {
				t.Fatal(err)
			}
			task, err := s.CreateTask(project.ID, project.Columns[0].ID, "归属", "", "", "gui")
			if err != nil {
				t.Fatal(err)
			}
			add := func(author, text string) {
				t.Helper()
				if _, err := s.AddComment(task.ID, text, author); err != nil {
					t.Fatal(err)
				}
			}
			add(channel.author, "派发 "+channel.runtime+" 执行…")
			add(channel.author, channel.runtime+" 执行失败（exit 1）。")
			current, err := s.Task(task.ID)
			if err != nil || current.CompletedBy != "" {
				t.Fatalf("failed run: %+v, %v", current, err)
			}
			// A casual completion comment lacks the structured result header.
			add("gui", channel.runtime+" 执行完成（1s）。输出：伪结果")
			current, _ = s.Task(task.ID)
			if current.CompletedBy != "" {
				t.Fatal("ordinary comment counted as completion")
			}
			add(channel.author, channel.runtime+" 执行完成（1s）。输出：\n\n结果")
			add("gui", "验收通过")
			var moved model.Task
			moved, err = s.MoveTask(task.ID, task.Version, project.Columns[3].ID, "", "gui")
			if err != nil || moved.CompletedBy != channel.label || moved.UpdatedBy != "gui" {
				t.Fatalf("manual move: %+v, %v", moved, err)
			}
			board, err := s.Board(project.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range board.Tasks {
				if item.ID == task.ID && item.CompletedBy != channel.label {
					t.Fatalf("board lost attribution: %+v", item)
				}
			}
			// A subsequent successful channel becomes the latest completing Agent.
			add("DSH", "dsh 执行完成（2s）。输出：\n\n新结果")
			current, err = s.Task(task.ID)
			if err != nil || current.CompletedBy != "DSH" {
				t.Fatalf("latest success: %+v, %v", current, err)
			}
		})
	}
}
