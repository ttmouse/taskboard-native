package main

import (
	"testing"
	"time"
)

func TestReviewDetailCanCompleteTask(t *testing.T) {
	f := seedBoard()
	f.mu.Lock()
	f.tasks[4].ColumnID = "c-review"
	f.mu.Unlock()
	b := newBoard(f)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return b.task("t-5") != nil })
	b.openDetail(*b.task("t-5"))
	waitFor(t, tt, "详情载入", func() bool { return b.detail != nil && !b.detail.loading })
	if !tt.HasText("结束任务") {
		t.Fatal("评审任务详情缺少结束任务按钮")
	}
	if err := tt.Click("结束任务"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, "任务移入完成", func() bool {
		return b.columnName(b.task("t-5").ColumnID) == "完成"
	})
	// Let the asynchronous API write finish before the test exits.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if task, err := f.Task("t-5"); err == nil && task.ColumnID == "c-done" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("完成状态未写入任务服务")
}
