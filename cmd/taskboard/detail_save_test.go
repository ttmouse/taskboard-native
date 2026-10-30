package main

import (
	"errors"
	"testing"

	"taskboard-native/internal/client"
	"taskboard-native/internal/model"
)

func TestDetailSaveWithoutChangesCloses(t *testing.T) {
	b := newBoard(seedBoard())
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return len(b.tasks) > 0 })
	b.openDetail(*b.task("t-1"))
	waitFor(t, tt, "详情载入", func() bool { return !b.detail.loading })
	tt.Frame()
	if err := tt.Click("关闭详情"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if b.detail != nil {
		t.Fatal("未修改内容时关闭详情，弹层仍然打开")
	}
}

type gatedDetailSave struct {
	*fakeBoard
	gate     chan struct{}
	err      error
	conflict bool
}

func (f *gatedDetailSave) Update(id string, version int, title, description, assignee *string) (model.Task, error) {
	<-f.gate
	if f.err != nil {
		return model.Task{}, f.err
	}
	if f.conflict {
		f.conflict = false
		f.mu.Lock()
		f.tasks[0].Version++
		fresh := f.tasks[0]
		f.mu.Unlock()
		return fresh, &client.ConflictError{Current: fresh}
	}
	return f.fakeBoard.Update(id, version, title, description, assignee)
}

func TestDetailSaveFailureKeepsDraft(t *testing.T) {
	f := &gatedDetailSave{fakeBoard: seedBoard(), gate: make(chan struct{}), err: errors.New("保存失败")}
	close(f.gate)
	b := newBoard(f)
	tt := newBoardTester(b)
	waitFor(t, tt, "载入", func() bool { return len(b.tasks) > 0 })
	b.openDetail(*b.task("t-1"))
	waitFor(t, tt, "详情", func() bool { return !b.detail.loading })
	d := b.detail
	d.body = "未保存的正文"
	b.saveDetail()
	waitFor(t, tt, "失败返回", func() bool { return !b.saving })
	if b.detail != d || !d.open || d.body != "未保存的正文" {
		t.Fatal("保存失败关闭详情或丢失草稿")
	}
}

func TestDetailConflictRetryCloses(t *testing.T) {
	f := &gatedDetailSave{fakeBoard: seedBoard(), gate: make(chan struct{}), conflict: true}
	close(f.gate)
	b := newBoard(f)
	tt := newBoardTester(b)
	waitFor(t, tt, "载入", func() bool { return len(b.tasks) > 0 })
	b.openDetail(*b.task("t-1"))
	waitFor(t, tt, "详情", func() bool { return !b.detail.loading })
	b.detail.body = "保留我的正文"
	b.saveDetail()
	waitFor(t, tt, "版本冲突", func() bool { return b.conflict != nil })
	if err := tt.Click("保留我的修改"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, "重试保存", func() bool {
		saved, err := f.Task("t-1")
		return err == nil && saved.Description == "保留我的正文" && !b.saving
	})
	if b.detail != nil {
		t.Fatal("冲突重试保存成功后，详情仍然打开")
	}
}

func TestDetailSaveDoesNotCloseAnotherTask(t *testing.T) {
	f := &gatedDetailSave{fakeBoard: seedBoard(), gate: make(chan struct{})}
	b := newBoard(f)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return len(b.tasks) > 0 })
	b.openDetail(*b.task("t-1"))
	waitFor(t, tt, "详情载入", func() bool { return !b.detail.loading })
	b.detail.title += "（修改）"
	b.saveDetail()
	b.openDetail(*b.task("t-2"))
	next := b.detail
	close(f.gate)
	waitFor(t, tt, "旧详情保存完成", func() bool { return !b.saving })
	if b.detail != next || !next.open {
		t.Fatal("旧任务保存完成，错误地关闭了后来打开的任务详情")
	}
}
