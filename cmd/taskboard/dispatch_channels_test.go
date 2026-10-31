package main

import (
	"image/png"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
	"taskboard-native/internal/executor"
)

func TestDispatchChannelMenu(t *testing.T) {
	for _, channel := range executor.Runtimes() {
		t.Run(channel.ID, func(t *testing.T) {
			f := seedBoard()
			f.projects[0].WorkspacePath = t.TempDir()
			// Seed IDs t-1..t-5 already exist; keep generated IDs unique.
			f.seq = 5
			b := newBoard(f)
			tt := newBoardTester(b)
			waitFor(t, tt, "看板载入", func() bool { return len(b.tasks) > 0 })
			settle(t, tt)
			if err := tt.RightClick("打开任务：把看板接上 SSE"); err != nil {
				t.Fatal(err)
			}
			if err := tt.ChooseMenuItem("派发任务", channel.Label); err != nil {
				t.Fatal(err)
			}
			waitFor(t, tt, "渠道派发", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return len(f.dispatches) == 1 })
			f.mu.Lock()
			got := f.dispatches[0]
			f.mu.Unlock()
			if got != [2]string{"t-1", channel.ID} {
				t.Fatalf("dispatch=%v", got)
			}
		})
	}
}

func TestComposeCmdEnterDispatchesSelectedChannel(t *testing.T) {
	for _, channel := range executor.Runtimes() {
		t.Run(channel.ID, func(t *testing.T) {
			f := seedBoard()
			f.projects[0].WorkspacePath = t.TempDir()
			f.seq = 5 // The seeded tasks already occupy t-1 through t-5.
			b := newBoard(f)
			tt := newBoardTester(b)
			waitFor(t, tt, "看板载入", func() bool { return len(b.tasks) > 0 })
			b.openTaskFormIn("local", "c-todo")
			tt.Frame()
			if err := tt.Click("执行渠道"); err != nil {
				t.Fatal(err)
			}
			if err := tt.ChooseMenuItem(channel.Label); err != nil {
				t.Fatal(err)
			}
			if err := tt.Click("任务标题"); err != nil {
				t.Fatal(err)
			}
			if path := os.Getenv("TASKBOARD_CHANNEL_REVIEW"); path != "" && channel.ID == executor.RuntimeCodex {
				file, err := os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := png.Encode(file, tt.Image()); err != nil {
					file.Close()
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
			}
			tt.Type("快捷创建并派发")
			tt.Key(ui.Cmd, ui.KeyEnter)
			waitFor(t, tt, "建卡和派发", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return len(f.createAndDispatches) == 1 })
			if b.form != nil {
				t.Fatal("Cmd+Enter did not close compose")
			}
			f.mu.Lock()
			request := f.createAndDispatches[0]
			f.mu.Unlock()
			created, err := f.Task(request[0])
			if err != nil || created.Title != "快捷创建并派发" || request[1] != channel.ID {
				t.Fatalf("request=%v task=%+v err=%v", request, created, err)
			}
		})
	}
}

func TestComposeCmdEnterWithoutWorkspaceKeepsDraft(t *testing.T) {
	f := seedBoard()
	b := newBoard(f)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return len(b.tasks) > 0 })
	b.openTaskFormIn("local", "c-todo")
	tt.Frame()
	tt.Type("保留未派发的任务")
	tt.Key(ui.Cmd, ui.KeyEnter)
	tt.Frame()
	if b.form == nil || b.form.name != "保留未派发的任务" {
		t.Fatal("missing workspace discarded draft")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.dispatches) != 0 || len(f.tasks) != 5 {
		t.Fatal("missing workspace created or dispatched a task")
	}
}
