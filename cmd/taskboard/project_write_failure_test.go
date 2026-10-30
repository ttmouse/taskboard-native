package main

import (
	"errors"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestProjectFormRetainsDraftWhenWriteFails(t *testing.T) {
	for _, tc := range []struct {
		name      string
		editing   bool
		button    bool
		fieldText string
	}{
		{name: "CreateEnter", fieldText: "新项目草稿"},
		{name: "CreateButton", button: true, fieldText: "新项目草稿"},
		{name: "EditEnter", editing: true, fieldText: "重命名草稿"},
		{name: "EditButton", editing: true, button: true, fieldText: "重命名草稿"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolatePrefs(t)
			fake := seedBoard()
			fake.projectFail = errors.New("项目写入失败")
			fake.delay = 50 * time.Millisecond
			b := newBoard(fake)
			tt := newBoardTester(b)
			waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
			settle(t, tt)
			if tc.editing {
				b.openProjectEdit(fake.projects[0])
			} else {
				b.form = &form{open: true}
			}
			tt.Frame()
			if tc.editing {
				tt.Key(ui.Cmd, ui.KeyA)
			}
			tt.Type(tc.fieldText)
			if tc.button {
				label := "创建"
				if tc.editing {
					label = "保存"
				}
				if err := tt.Click(label); err != nil {
					t.Fatal(err)
				}
			} else {
				tt.Key(0, ui.KeyEnter)
			}
			if b.form == nil || !b.form.open || !b.form.creating || b.form.name != tc.fieldText {
				t.Fatalf("pending project write lost draft or busy state: form=%+v", b.form)
			}

			waitFor(t, tt, "项目写入失败提示", func() bool {
				return tt.HasText("创建失败") || tt.HasText("保存失败")
			})
			if b.form == nil || !b.form.open || b.form.creating || b.form.name != tc.fieldText {
				t.Fatalf("failed project write lost draft: form=%+v", b.form)
			}
		})
	}
}
