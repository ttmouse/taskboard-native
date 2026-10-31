package main

import (
	"testing"

	"taskboard-native/internal/executor"
	"taskboard-native/internal/model"
)

func TestTaskDetailShowsHerdrConversationOnAgentReply(t *testing.T) {
	isolatePrefs(t)
	fake := seedBoard()
	task := fake.tasks[0]
	task.Execution = &model.Execution{
		TaskID: task.ID, SessionID: "herdr:terminal-1", Runtime: executor.RuntimeCodex, Status: "running",
	}
	task.Comments = []model.Comment{{
		ID: "c-ai", Author: "Codex", Text: "已完成改动。",
	}}
	fake.tasks[0] = task
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText(task.Title) })
	b.openDetail(task)
	tt.Frame()
	waitFor(t, tt, "回复底部的 Herdr 对话入口", func() bool {
		return tt.HasText("在 Herdr 中打开此回复的对话")
	})
	if tt.HasText("在 Herdr 中打开此任务的 Codex 对话") {
		t.Fatal("detail title still showed the old Herdr conversation action")
	}
	if b.detail == nil || b.detail.id != task.ID {
		t.Fatal("conversation action did not render in the opened task detail")
	}
}

func TestTaskDetailHidesUnavailableHerdrConversationAction(t *testing.T) {
	isolatePrefs(t)
	fake := seedBoard()
	task := fake.tasks[0]
	task.Execution = &model.Execution{
		TaskID: task.ID, SessionID: "herdr:terminal-1", Runtime: executor.RuntimeCodex, Status: "running",
		Detail: executor.HerdrInitialTurnPendingDetail,
	}
	task.Comments = []model.Comment{{
		ID: "c-ai", Author: "Codex", Text: " booting ",
	}}
	fake.tasks[0] = task
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText(task.Title) })
	b.openDetail(task)
	tt.Frame()
	if _, ok := tt.Find("在 Herdr 中打开此回复的对话"); ok {
		t.Fatal("conversation action appeared before the initial agent turn")
	}
	if _, ok := tt.Find("在 Herdr 中打开此任务的 Codex 对话"); ok {
		t.Fatal("title-bar conversation action should stay removed")
	}
}

func TestTaskDetailHidesHerdrConversationOnHumanReply(t *testing.T) {
	isolatePrefs(t)
	fake := seedBoard()
	task := fake.tasks[0]
	task.Execution = &model.Execution{
		TaskID: task.ID, SessionID: "herdr:terminal-1", Runtime: executor.RuntimeCodex, Status: "running",
	}
	task.Comments = []model.Comment{{
		ID: "c-human", Author: "豆爸", Text: "人写的评论",
	}}
	fake.tasks[0] = task
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText(task.Title) })
	b.openDetail(task)
	tt.Frame()
	if _, ok := tt.Find("在 Herdr 中打开此回复的对话"); ok {
		t.Fatal("human reply should not get a Herdr conversation bubble")
	}
}
