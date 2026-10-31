package main

import (
	"testing"

	"taskboard-native/internal/executor"

	"github.com/egoist/mygo/ui"
)

func TestDetailHandoffHeaderVisibleWhenEmpty(t *testing.T) {
	isolatePrefs(t)
	b := newBoard(seedBoard())
	tt := ui.NewTester(b.view, 1280, 800)
	waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
	b.openDetail(*b.task("t-1"))
	waitFor(t, tt, "详情数据载入", func() bool {
		return b.detail != nil && !b.detail.loading && !b.detail.handoffLogLoading
	})
	b.detail.handoffLog = executor.HandoffLogView{}
	b.detail.handoffLogFailed = false
	settle(t, tt)
	if !tt.HasText("自动承接") {
		t.Fatalf("empty handoff section header missing; texts: %v", tt.Texts())
	}
	if tt.HasText("查看") {
		t.Fatalf("empty handoff should not offer expand toggle; texts: %v", tt.Texts())
	}
}

func TestDetailHandoffHistoryStartsCollapsedAndCanExpand(t *testing.T) {
	isolatePrefs(t)
	b := newBoard(seedBoard())
	tt := ui.NewTester(b.view, 1280, 800)
	waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
	b.openDetail(*b.task("t-1"))
	waitFor(t, tt, "详情数据载入", func() bool {
		return b.detail != nil && !b.detail.loading && !b.detail.handoffLogLoading
	})
	b.detail.handoffLog = executor.HandoffLogView{
		Events: []executor.HandoffEvent{
			{Event: "task.dispatch_started"},
			{Event: "task.dispatch_accepted"},
		},
	}
	settle(t, tt)
	if tt.HasText("开始派发") {
		t.Fatal("handoff event details should start collapsed")
	}
	if err := tt.Click("查看 2 条记录"); err != nil {
		t.Fatal(err)
	}
	settle(t, tt)
	if !tt.HasText("开始派发") || !tt.HasText("已接受") {
		t.Fatalf("expanded handoff events missing; texts: %v", tt.Texts())
	}
	if err := tt.Click("收起记录"); err != nil {
		t.Fatal(err)
	}
	settle(t, tt)
	if tt.HasText("开始派发") {
		t.Fatal("handoff event details remained visible after collapse")
	}
}
