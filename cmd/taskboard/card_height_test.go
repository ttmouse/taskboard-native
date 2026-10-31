package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"taskboard-native/internal/model"
)

// Full cards may grow as they reveal their descriptions, but must never
// become shorter than the same cards in title-only mode.
func TestFullCardsAreNotShorterThanTitleOnlyCards(t *testing.T) {
	isolatePrefs(t)
	fake := seedBoard()
	long := strings.Repeat("较长的正文内容用来验证展开卡片高度", 8)
	fake.tasks = append(fake.tasks,
		model.Task{ID: "short-body", ProjectID: "local", ColumnID: "c-todo", Title: "短标题", Description: "一行正文"},
		model.Task{ID: "long-body", ProjectID: "local", ColumnID: "c-todo", Title: "较长标题内容重复测试标题宽度", Description: long},
		model.Task{ID: "review-empty", ProjectID: "local", ColumnID: "c-review", Title: "无正文的评审卡片"},
		model.Task{ID: "execution-empty", ProjectID: "local", ColumnID: "c-doing", Title: "无正文的执行卡片", Execution: &model.Execution{TaskID: "execution-empty", SessionID: "herdr:terminal-1", Runtime: "codex", Status: "running"}},
	)
	b := newBoard(fake)
	b.prefs.TitleOnlyCards = true
	tt := ui.NewTester(b.view, renderW, renderH)
	waitFor(t, tt, "卡片载入", func() bool { return tt.HasText("短标题") && tt.HasText("无正文的执行卡片") })
	settle(t, tt)
	heights := make(map[string]float32, len(fake.tasks))
	for _, task := range fake.tasks {
		card, ok := tt.Find("打开任务：" + task.Title)
		if !ok {
			t.Fatalf("title-only card %q missing", task.Title)
		}
		heights[task.ID] = card.H
	}

	b.prefs.TitleOnlyCards = false
	tt.Frame()
	settle(t, tt)
	for _, task := range fake.tasks {
		card, ok := tt.Find("打开任务：" + task.Title)
		if !ok {
			t.Fatalf("full card %q missing", task.Title)
		}
		if card.H+1 < heights[task.ID] {
			t.Errorf("full card %q is shorter: full %.1f, title-only %.1f", task.Title, card.H, heights[task.ID])
		}
	}
}
