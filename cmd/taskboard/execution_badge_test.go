package main

import (
	"strings"
	"testing"

	"taskboard-native/internal/executor"
	"taskboard-native/internal/model"
)

func TestHerdrExecutionIconsCoverKnownStatuses(t *testing.T) {
	for _, status := range []string{
		"submitting", "running", "waiting_approval", "returned", "succeeded",
		"failed", "disconnected", "unknown", "detached", "released",
	} {
		if herdrExecutionIcon(status) == nil {
			t.Errorf("status %q has no icon", status)
		}
	}
	if herdrExecutionIcon("future_status") != nil {
		t.Fatal("unknown status unexpectedly has an icon")
	}
}

// Control for the suppression below: an unfinished card still shows it.
func TestActiveCardKeepsExecutionBadge(t *testing.T) {
	isolatePrefs(t)
	fake := seedBoard()
	for _, task := range fake.tasks {
		if task.ID != "t-1" {
			continue
		}
		task.Execution = &model.Execution{
			TaskID: task.ID, SessionID: "herdr:gone", Runtime: executor.RuntimePi, Status: "disconnected",
		}
		fake.tasks[0] = task
	}
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "连接异常 胶囊", func() bool { return tt.HasText("连接异常") })
}

// The workflow column and Herdr session report different facts. Keep the
// session badge visible after completion so the card does not become blank.
func TestDoneCardKeepsActiveExecutionBadge(t *testing.T) {
	isolatePrefs(t)
	fake := seedBoard()
	for _, task := range fake.tasks {
		if task.ID != "t-1" {
			continue
		}
		task.ColumnID = "c-done"
		task.Execution = &model.Execution{
			TaskID: task.ID, SessionID: "herdr:gone", Runtime: executor.RuntimePi, Status: "disconnected",
		}
		fake.tasks[0] = task
	}
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "连接异常 胶囊", func() bool { return tt.HasText("连接异常") })
}

// The terminal outcomes stay readable: 已释放 is the convergence result and is
// what tells a person the card can be dispatched again.
func TestDoneCardKeepsReleasedBadge(t *testing.T) {
	isolatePrefs(t)
	fake := seedBoard()
	for _, task := range fake.tasks {
		if task.ID != "t-1" {
			continue
		}
		task.ColumnID = "c-done"
		task.Execution = &model.Execution{
			TaskID: task.ID, SessionID: "herdr:gone", Runtime: executor.RuntimePi, Status: "released",
		}
		fake.tasks[0] = task
	}
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "已释放 胶囊", func() bool { return tt.HasText("已释放") })
}

func TestReturnedExecutionBadgeOnlyInReviewColumn(t *testing.T) {
	for _, tc := range []struct {
		name     string
		columnID string
		visible  bool
	}{
		{name: "doing", columnID: "c-doing"},
		{name: "review", columnID: "c-review", visible: true},
		{name: "done", columnID: "c-done"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolatePrefs(t)
			fake := seedBoard()
			for i := range fake.tasks {
				if fake.tasks[i].ID == "t-3" {
					fake.tasks[i].ColumnID = tc.columnID
					fake.tasks[i].Execution = &model.Execution{
						TaskID: "t-3", SessionID: "herdr:done", Runtime: executor.RuntimePi, Status: "returned",
					}
				}
			}
			b := newBoard(fake)
			tt := newBoardTester(b)
			waitFor(t, tt, "卡片渲染", func() bool { return tt.HasText("原生窗口渲染验收") })
			if tc.visible && !tt.HasText("执行已返回") {
				t.Fatal("returned badge is missing from review column")
			}
			if !tc.visible && tt.HasText("执行已返回") {
				t.Fatalf("returned badge unexpectedly visible in %s column", tc.name)
			}
		})
	}
}

func TestHerdrExecutionDetailMentionsWriteLock(t *testing.T) {
	got := herdrExecutionDetail("disconnected", "连接异常")
	for _, want := range []string{"连接异常", "写锁", "不可重派", "2 分钟"} {
		if !strings.Contains(got, want) {
			t.Fatalf("disconnected detail %q missing %q", got, want)
		}
	}
	released := herdrExecutionDetail("released", "已释放")
	for _, want := range []string{"已释放写锁", "重新派发"} {
		if !strings.Contains(released, want) {
			t.Fatalf("released detail %q missing %q", released, want)
		}
	}
	if running := herdrExecutionDetail("running", "运行中"); running != "Herdr 执行状态：运行中" {
		t.Fatalf("running detail = %q", running)
	}
}

// Badge a11y / Tooltip surface the write-lock copy, not only the short capsule.
func TestDisconnectedBadgeTooltipMentionsWriteLock(t *testing.T) {
	isolatePrefs(t)
	fake := seedBoard()
	for i := range fake.tasks {
		if fake.tasks[i].ID != "t-1" {
			continue
		}
		fake.tasks[i].Execution = &model.Execution{
			TaskID: fake.tasks[i].ID, SessionID: "herdr:gone", Runtime: executor.RuntimePi, Status: "disconnected",
		}
	}
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "连接异常写锁说明", func() bool {
		return tt.HasText("写锁仍占用") || tt.HasText("暂不可重派")
	})
}
