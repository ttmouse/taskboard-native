package main

import (
	"errors"
	"github.com/egoist/mygo/ui"
	"image/png"
	"os"
	"testing"
	"time"

	"taskboard-native/internal/model"
)

func (f *fakeBoard) ContinueComment(taskID, commentID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dispatches = append(f.dispatches, [2]string{taskID, "continue:" + commentID})
	return nil
}

type failedFollowupBoard struct{ *fakeBoard }

func (f *failedFollowupBoard) ContinueComment(taskID, commentID string) error {
	return errors.New("原会话仍在执行")
}

func TestSendAndFollowupKeepsSavedCommentOnFailure(t *testing.T) {
	f := seedBoard()
	f.seq = 5 // Seeded comments already use m-1.
	b := newBoard(&failedFollowupBoard{f})
	tt := newBoardTester(b)
	waitFor(t, tt, "载入", func() bool { return len(b.tasks) > 0 })
	b.openDetail(*b.task("t-1"))
	tt.Frame()
	waitFor(t, tt, "评论载入", func() bool { return !b.detail.loading })
	if err := tt.Click("写评论"); err != nil {
		t.Fatal(err)
	}
	tt.Type("补充：请沿用原始会话继续处理")
	tt.Key(0, ui.KeyEnter)
	waitFor(t, tt, "评论已保存并返回列表", func() bool { return !b.commenting && b.detail == nil })
	b.openDetail(*b.task("t-1"))
	waitFor(t, tt, "重新打开详情", func() bool { return b.detail != nil && !b.detail.loading })
	cm := b.detail.comments[len(b.detail.comments)-1]
	if err := tt.Click("跟进评论：" + cm.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, "评论保留", func() bool { return !b.commenting && len(b.detail.comments) == 2 && b.detail.comment == "" })
	if len(b.detail.comments) != 2 || b.detail.comments[1].Text != "补充：请沿用原始会话继续处理" {
		t.Fatalf("comment duplicated: %+v", b.detail.comments)
	}
	if path := os.Getenv("TASKBOARD_FOLLOWUP_REVIEW"); path != "" {
		settle(t, tt)
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := png.Encode(file, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCommentExplicitFollowup(t *testing.T) {
	f := seedBoard()
	f.seq = 5
	b := newBoard(f)
	tt := newBoardTester(b)
	waitFor(t, tt, "载入", func() bool { return len(b.tasks) > 0 })
	b.openDetail(*b.task("t-1"))
	tt.Frame()
	waitFor(t, tt, "评论载入", func() bool { return !b.detail.loading })
	if err := tt.Click("写评论"); err != nil {
		t.Fatal(err)
	}
	tt.Type("补充要求")
	tt.Key(0, ui.KeyEnter)
	waitFor(t, tt, "评论保存并返回列表", func() bool { return !b.commenting && b.detail == nil })
	b.openDetail(*b.task("t-1"))
	waitFor(t, tt, "重新打开详情", func() bool { return b.detail != nil && !b.detail.loading })
	f.mu.Lock()
	count := len(f.dispatches)
	f.mu.Unlock()
	if count != 0 {
		t.Fatal("普通评论触发了执行")
	}
	cm := b.detail.comments[len(b.detail.comments)-1]
	if err := tt.Click("跟进评论：" + cm.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, "明确跟进", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return len(f.dispatches) == 1 })
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dispatches[0] != [2]string{"t-1", "continue:" + cm.ID} {
		t.Fatal(f.dispatches)
	}
}

func TestPlainCommentAutomaticallyContinuesReturnedSession(t *testing.T) {
	f := seedBoard()
	f.tasks[0].Execution = &model.Execution{
		TaskID: "t-1", SessionID: "herdr:terminal-1", Status: "returned", Runtime: "codex",
	}
	b := newBoard(f)
	tt := newBoardTester(b)
	waitFor(t, tt, "载入", func() bool { return len(b.tasks) > 0 })
	b.openDetail(*b.task("t-1"))
	tt.Frame()
	waitFor(t, tt, "评论载入", func() bool { return !b.detail.loading })
	if err := tt.Click("写评论"); err != nil {
		t.Fatal(err)
	}
	tt.Type("请按这条补充意见修改报告")
	tt.Key(0, ui.KeyEnter)
	waitFor(t, tt, "原会话自动跟进", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.dispatches) == 1 && len(f.tasks[0].Comments) == 2
	})
	f.mu.Lock()
	defer f.mu.Unlock()
	cm := f.tasks[0].Comments[1]
	if cm.Text != "请按这条补充意见修改报告" || cm.Mention != "" {
		t.Fatalf("comment = %+v", cm)
	}
	if f.dispatches[0] != [2]string{"t-1", "continue:" + cm.ID} {
		t.Fatalf("follow-up = %v, want original session for %s", f.dispatches, cm.ID)
	}
}

func TestFollowupButtonOnlyShownOnUserComments(t *testing.T) {
	f := seedBoard()
	comments := f.tasks[0].Comments
	comments[0].ID = "agent-comment"
	comments[0].Author = "DSH"
	comments = append(comments, model.Comment{
		ID: "user-comment", Author: "豆爸", Text: "请继续处理", CreatedAt: time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC),
	})
	f.tasks[0].Comments = comments
	b := newBoard(f)
	b.prefs.DisplayName = "豆爸"
	b.agent = b.prefs.windowAgent()
	tt := newBoardTester(b)
	waitFor(t, tt, "载入", func() bool { return len(b.tasks) > 0 })
	b.openDetail(*b.task("t-1"))
	tt.Frame()
	waitFor(t, tt, "评论载入", func() bool { return !b.detail.loading })
	if _, ok := tt.Find("跟进评论：agent-comment"); ok {
		t.Fatal("agent-authored status comment shows a follow-up button")
	}
	if _, ok := tt.Find("跟进评论：user-comment"); !ok {
		t.Fatal("user-authored comment is missing its follow-up button")
	}
}
