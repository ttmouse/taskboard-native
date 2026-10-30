package main

import (
	"strings"
	"testing"

	"taskboard-native/internal/model"

	"github.com/egoist/mygo/ui"
)

// Details occupy the full board pane while leaving the project sidebar
// visible. Long descriptions and histories must still fit at short heights.
func TestDetailFillsRightPaneAndKeepsSidebar(t *testing.T) {
	cases := []struct {
		name    string
		w, h    int
		body    string
		history int
	}{
		{"minimum", 1280, 560, "", 1},
		{"minimum-long-body", 1280, 560, strings.Repeat("一段很长的任务正文，用来把详情撑高。\n\n", 12), 1},
		{"default", 1280, 800, "", 1},
		{"default-long-body", 1280, 800, strings.Repeat("一段很长的任务正文。\n\n", 12), 1},
		{"tall", 1280, 1000, "", 1},
		{"minimum-long-history", 1280, 560, "", 12},
		{"no-comments", 1280, 560, "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolatePrefs(t)
			b := newBoard(seedBoard())
			tt := ui.NewTester(b.view, tc.w, tc.h)
			waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
			b.openDetail(*b.task("t-1"))
			waitFor(t, tt, "评论载入", func() bool { return !b.detail.loading })
			if tc.body != "" {
				b.detail.body = tc.body
			}
			b.detail.comments = b.detail.comments[:0]
			for i := 0; i < tc.history; i++ {
				b.detail.comments = append(b.detail.comments, model.Comment{
					ID: "c-" + string(rune('a'+i)), Author: "agent",
					Text: "一条评论，用来把对话撑长。",
				})
			}
			settle(t, tt)

			header, ok := tt.Find("任务详情")
			if !ok {
				t.Fatalf("detail header missing; texts: %v", tt.Texts())
			}
			if header.X < 208 || header.Y > 40 {
				t.Fatalf("detail does not start in the right pane at the top: %+v", header)
			}
			if !tt.HasText("全部项目") {
				t.Fatal("project sidebar disappeared while details were open")
			}
			input, ok := tt.Find("写评论")
			if !ok || input.Y < 0 || input.Y+input.H > float32(tc.h) {
				t.Fatalf("comment composer is outside the right pane: %+v (found %v)", input, ok)
			}
			for _, removed := range []string{"保存任务", "归档任务", "发送评论", "发送评论并让 AI 跟进"} {
				if tt.HasText(removed) {
					t.Errorf("detail footer still shows %q", removed)
				}
			}
		})
	}
}

// The task fields keep usable room: fitting the footer by collapsing the
// content would hide what the reader opened the sheet for.
func TestDetailContentKeepsUsableHeight(t *testing.T) {
	isolatePrefs(t)
	b := newBoard(seedBoard())
	tt := ui.NewTester(b.view, 1280, 800)
	waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
	b.openDetail(*b.task("t-1"))
	waitFor(t, tt, "评论载入", func() bool { return !b.detail.loading })
	if err := tt.Click("编辑任务正文"); err != nil {
		t.Fatal(err)
	}
	settle(t, tt)
	body, ok := tt.Find("正文编辑器")
	if !ok {
		t.Fatal("body text area missing")
	}
	if body.H < 100 {
		t.Fatalf("content area collapsed to %v tall", body.H)
	}
}

func TestEscapeClosesDetailFromRightPane(t *testing.T) {
	isolatePrefs(t)
	b := newBoard(seedBoard())
	tt := ui.NewTester(b.view, 1280, 800)
	waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
	b.openDetail(*b.task("t-1"))
	waitFor(t, tt, "评论载入", func() bool { return !b.detail.loading })
	tt.Key(0, ui.KeyEscape)
	waitFor(t, tt, "详情关闭", func() bool { return b.detail == nil })
}
