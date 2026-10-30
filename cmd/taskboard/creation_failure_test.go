package main

import (
	"errors"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestQuickAddRetainsDraftWhenCreateFails(t *testing.T) {
	isolatePrefs(t)
	if err := saveUIPrefs(uiPrefs{BoardVertical: true}); err != nil {
		t.Fatal(err)
	}
	fake := seedBoard()
	fake.createFail = errors.New("写入失败")
	fake.delay = 50 * time.Millisecond
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)
	if err := tt.Click("切换到项目 local"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, "切换项目", func() bool { return b.cur == "local" })
	waitFor(t, tt, "看板列到达", func() bool { return len(b.columns) > 0 })

	const title = "服务失败后仍保留"
	focusQuickAdd(t, tt)
	tt.Type(title)
	tt.Key(0, ui.KeyEnter)
	if !b.quickCreating || b.quickTitle != title {
		t.Fatalf("pending create lost draft or busy state: creating=%v title=%q", b.quickCreating, b.quickTitle)
	}
	waitFor(t, tt, "创建失败提示", func() bool { return tt.HasText("创建失败") })
	if b.quickCreating {
		t.Fatal("failed create left quick add busy")
	}
	if b.quickTitle != title {
		t.Fatalf("failed create lost draft: got %q, want %q", b.quickTitle, title)
	}
}

func TestComposeRetainsDraftWhenCreateFails(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mods   ui.Modifiers
		button bool
	}{
		{name: "Enter"},
		{name: "Cmd+Enter", mods: ui.Cmd},
		{name: "Button", button: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolatePrefs(t)
			fake := seedBoard()
			fake.projects[0].WorkspacePath = t.TempDir()
			fake.createFail = errors.New("写入失败")
			// Keep the request pending across slow UI frames before asserting
			// that the compose form retains its busy state and draft.
			fake.delay = 300 * time.Millisecond
			b := newBoard(fake)
			tt := newBoardTester(b)
			waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
			settle(t, tt)
			b.openTaskFormIn("local", "c-todo")
			tt.Frame()
			tt.Type("服务失败后仍保留")
			if tc.button {
				if err := tt.Click("创建任务"); err != nil {
					t.Fatal(err)
				}
			} else {
				tt.Key(tc.mods, ui.KeyEnter)
			}
			if b.form == nil || !b.form.open || !b.form.creating || b.form.name != "服务失败后仍保留" {
				t.Fatalf("pending create lost draft or busy state: form=%+v", b.form)
			}

			waitFor(t, tt, "创建失败提示", func() bool { return tt.HasText("创建失败") })
			if b.form == nil || !b.form.open || b.form.creating || b.form.name != "服务失败后仍保留" {
				t.Fatalf("failed create lost compose draft: form=%+v", b.form)
			}
		})
	}
}
