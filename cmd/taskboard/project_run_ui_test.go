package main

import (
	"fmt"
	"path/filepath"
	"testing"

	"taskboard-native/internal/client"
	"taskboard-native/internal/executor"
	"taskboard-native/internal/model"
	"taskboard-native/internal/service"
)

func TestProjectRunButtonDispatchesTheOpenProject(t *testing.T) {
	f := seedBoard()
	f.projects[0].WorkspacePath = t.TempDir()
	b := newBoard(f)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return len(b.tasks) > 0 })
	if err := tt.Click("切换看板，当前 全部项目"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("local"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, "进入项目", func() bool { return b.cur == "local" })
	settle(t, tt)
	if err := tt.Click("开启自动承接"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(executor.Runtimes()[0].Label); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, "项目承接", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.projectRuns) == 1
	})
	channel := executor.Runtimes()[0]
	waitFor(t, tt, "渠道状态可见", func() bool { return tt.HasText(channel.Label + " 承接中") })
	f.mu.Lock()
	got := f.projectRuns[0]
	f.mu.Unlock()
	if got != [2]string{"local", channel.ID} {
		t.Fatalf("run=%v", got)
	}
}

func TestProjectRunStateLoadsAfterServiceRestartWhenOpeningProject(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "taskboard.db")
	srv, err := service.Start("127.0.0.1:0", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	cl := client.New(&client.Discover{Addr: srv.Addr}, "restart-regression")
	projects := make([]struct {
		project model.Project
		channel executor.Runtime
	}, 2)
	runtimes := executor.Runtimes()
	if len(runtimes) < len(projects) {
		t.Fatalf("got %d runtimes, want at least %d", len(runtimes), len(projects))
	}
	for i := range projects {
		project, err := cl.CreateProjectWithPath(fmt.Sprintf("restart-%d", i), t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		projects[i] = struct {
			project model.Project
			channel executor.Runtime
		}{project: project, channel: runtimes[i]}
		if _, err := cl.StartProjectRun(project.ID, runtimes[i].ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}

	srv, err = service.Start("127.0.0.1:0", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	b := newBoard(client.New(&client.Discover{Addr: srv.Addr}, "restart-regression"))
	defer b.stop()
	tt := newBoardTester(b)
	waitFor(t, tt, "重启后载入项目", func() bool { return len(b.projects) == len(projects) })
	for _, want := range projects {
		if err := tt.Click("切换到项目 " + want.project.Name); err != nil {
			t.Fatal(err)
		}
		waitFor(t, tt, "恢复项目自动承接", func() bool {
			return b.cur == want.project.ID && tt.HasText(want.channel.Label+" 承接中")
		})
	}
}
