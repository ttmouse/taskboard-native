package executor

import (
	"strings"
	"testing"

	"taskboard-native/internal/model"
)

func TestBuildDispatchTaskTextSelectsCodeReviewPipeline(t *testing.T) {
	task := model.Task{ID: "task-1", Title: "新增项目内搜索 API"}
	got := BuildDispatchTaskText(task, RuntimeCodex)
	if !strings.HasPrefix(got, "任务：新增项目内搜索 API\n$manage-taskboard-native 执行任务 task-1。") {
		t.Fatalf("Codex dispatch did not put title and skill first: %q", got)
	}
	if !strings.Contains(got, "Codex 实现 + Claude Code 评审") || !strings.Contains(got, "hc run") {
		t.Fatalf("code-generation dispatch did not include the review stages: %q", got)
	}
}

func TestBuildDispatchTaskTextKeepsSimpleAndAmbiguousTasksSingleAgent(t *testing.T) {
	for _, task := range []model.Task{
		{ID: "task-2", Title: "调整卡片间距"},
		{ID: "task-3", Title: "检查这个问题"},
	} {
		got := BuildDispatchTaskText(task, RuntimeCodex)
		if want := "任务：" + task.Title + "\n$manage-taskboard-native 执行任务 " + task.ID + "。"; got != want {
			t.Errorf("simple/ambiguous task %q unexpectedly selected a pipeline: %q", task.Title, got)
		}
	}
}

func TestBuildDispatchTaskTextHonorsAnExplicitNonCodexRuntime(t *testing.T) {
	task := model.Task{ID: "task-4", Title: "开发导出模块"}
	got := BuildDispatchTaskText(task, RuntimeClaude)
	if got != BuildTaskText(task) {
		t.Fatalf("explicit Claude runtime was changed into a multi-agent pipeline: %q", got)
	}
}
