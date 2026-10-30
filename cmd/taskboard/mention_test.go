package main

import (
	"image/png"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"

	"taskboard-native/internal/executor"
)

// An @ in the comment box addresses one agent: the picker opens, the @ never
// reaches the text, and sending the comment also wakes that agent.
func TestCommentMentionPicksChannelAndFollowsUp(t *testing.T) {
	f := seedBoard()
	b := newBoard(f)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return len(b.tasks) > 0 })
	b.openDetail(*b.task("t-1"))
	tt.Frame()
	waitFor(t, tt, "评论载入", func() bool { return !b.detail.loading })

	if err := tt.Click("写评论"); err != nil {
		t.Fatal(err)
	}
	tt.Type("请复核这处改动 @")
	tt.Frame()
	if !b.detail.mentionOpen {
		t.Fatal("typing @ did not open the channel picker")
	}
	if got := b.detail.comment; got != "请复核这处改动 " {
		t.Fatalf("the @ leaked into the text: %q", got)
	}
	if path := os.Getenv("TASKBOARD_MENTION_REVIEW"); path != "" {
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
	if err := tt.Click("评论提醒 Claude Code"); err != nil {
		t.Fatal(err)
	}
	if b.detail.commentMention != executor.RuntimeClaude {
		t.Fatalf("mention = %q", b.detail.commentMention)
	}

	before := len(b.detail.comments)
	// Picking a channel must not steal the keyboard from the draft.
	tt.Frame()
	tt.Type("，谢谢")
	if want := "请复核这处改动 ，谢谢"; b.detail.comment != want {
		t.Fatalf("draft after picking = %q, want %q", b.detail.comment, want)
	}
	tt.Key(0, ui.KeyEnter)
	waitFor(t, tt, "评论发出并跟进", func() bool {
		return !b.commenting && b.detail == nil
	})
	if before != 1 {
		t.Fatalf("unexpected initial comment count: %d", before)
	}
	if path := os.Getenv("TASKBOARD_MENTION_SENT"); path != "" {
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
	comments, err := f.Comments("t-1")
	if err != nil || len(comments) != before+1 {
		t.Fatalf("comments = %+v, err = %v", comments, err)
	}
	cm := comments[len(comments)-1]
	if cm.Mention != executor.RuntimeClaude || cm.Text != "请复核这处改动 ，谢谢" {
		t.Fatalf("comment = %+v", cm)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	want := "continue:" + cm.ID
	if len(f.dispatches) != 1 || f.dispatches[0][1] != want {
		t.Fatalf("followed up with %v, want %s", f.dispatches, want)
	}
}

// The menu is a floating list over the sheet, not a row that pushes the draft
// down: opening it must leave the comment box exactly where it was, and a press
// outside closes it without touching the text or the address.
func TestMentionMenuFloatsAboveComposer(t *testing.T) {
	b := newBoard(seedBoard())
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return len(b.tasks) > 0 })
	b.openDetail(*b.task("t-1"))
	tt.Frame()
	waitFor(t, tt, "评论载入", func() bool { return !b.detail.loading })
	if err := tt.Click("写评论"); err != nil {
		t.Fatal(err)
	}
	tt.Type("请复核 @")
	tt.Frame()
	closed, ok := tt.Find("写评论")
	if !ok {
		t.Fatal("composer not found")
	}
	if !b.detail.mentionOpen {
		t.Fatal("menu did not open")
	}
	// The draft keeps its place: a row inside the box would move it up.
	if err := tt.Click("任务详情"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if b.detail.mentionOpen {
		t.Fatal("a press outside the menu did not close it")
	}
	open, _ := tt.Find("写评论")
	if open.Y != closed.Y {
		t.Fatalf("the composer moved: %+v → %+v", closed, open)
	}
	if b.detail.commentMention != "" || b.detail.comment != "请复核 " {
		t.Fatalf("dismissing changed the draft: %+v", b.detail)
	}
}

func TestMentionMenuKeyboardSelection(t *testing.T) {
	b := newBoard(seedBoard())
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return len(b.tasks) > 0 })
	b.openDetail(*b.task("t-1"))
	tt.Frame()
	waitFor(t, tt, "评论载入", func() bool { return !b.detail.loading })
	if err := tt.Click("写评论"); err != nil {
		t.Fatal(err)
	}
	tt.Type("@")
	tt.Frame()
	if !b.detail.mentionOpen || b.detail.mentionIndex != 0 {
		t.Fatalf("picker state = open:%v index:%d, want open at first channel", b.detail.mentionOpen, b.detail.mentionIndex)
	}

	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if b.detail.mentionIndex != 1 {
		t.Fatalf("Down selected index %d, want 1", b.detail.mentionIndex)
	}
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if b.detail.mentionIndex != 1 {
		t.Fatalf("Up selected index %d, want 1", b.detail.mentionIndex)
	}

	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if b.detail.mentionOpen || b.detail.commentMention != executor.RuntimeCodex {
		t.Fatalf("keyboard selection left picker open:%v address:%q", b.detail.mentionOpen, b.detail.commentMention)
	}
	if b.detail.comment != "" {
		t.Fatalf("keyboard selection changed draft: %q", b.detail.comment)
	}
}

// The address is a token before the draft: Backspace at the start of an empty
// draft removes it, and Backspace with text before the caret still edits text.
func TestMentionChipIsDeletedByBackspace(t *testing.T) {
	b := newBoard(seedBoard())
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return len(b.tasks) > 0 })
	b.openDetail(*b.task("t-1"))
	tt.Frame()
	waitFor(t, tt, "评论载入", func() bool { return !b.detail.loading })
	if err := tt.Click("写评论"); err != nil {
		t.Fatal(err)
	}
	tt.Type("@")
	tt.Frame()
	if err := tt.Click("评论提醒 Pi Agent"); err != nil {
		t.Fatal(err)
	}
	if b.detail.commentMention != executor.RuntimePi || b.detail.comment != "" {
		t.Fatalf("address = %q draft = %q", b.detail.commentMention, b.detail.comment)
	}
	tt.Frame()
	tt.Key(0, ui.KeyBackspace)
	tt.Frame()
	if b.detail.commentMention != "" {
		t.Fatal("Backspace did not delete the address")
	}
	if b.detail.comment != "" {
		t.Fatalf("Backspace changed the draft: %q", b.detail.comment)
	}

	// With text to the left of the caret, Backspace is an ordinary edit.
	if err := tt.Click("写评论"); err != nil {
		t.Fatal(err)
	}
	tt.Type("@")
	tt.Frame()
	if err := tt.Click("评论提醒 Claude Code"); err != nil {
		t.Fatal(err)
	}
	tt.Type("补充")
	tt.Frame()
	tt.Key(0, ui.KeyBackspace)
	tt.Frame()
	if b.detail.commentMention != executor.RuntimeClaude {
		t.Fatalf("address was deleted while editing text: %q", b.detail.commentMention)
	}
	if b.detail.comment != "补" {
		t.Fatalf("draft = %q", b.detail.comment)
	}
}

// The @ only means a mention at a word start; an email address must stay text.
func TestMentionOnlyTriggersAtWordStart(t *testing.T) {
	for _, tc := range []struct {
		draft, want string
		typed       bool
	}{
		{"@", "", true},
		{"请看看 @", "请看看 ", true},
		{"第一行\n@", "第一行\n", true},
		{"douba@", "douba@", false},
		{"a@b", "a@b", false},
		{"没有提醒", "没有提醒", false},
	} {
		if got := justTypedMention(tc.draft); got != tc.typed {
			t.Fatalf("%q: justTypedMention = %v, want %v", tc.draft, got, tc.typed)
		}
	}
}
