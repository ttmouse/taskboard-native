package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"taskboard-native/internal/model"
)

func TestOkToastAndToastKinds(t *testing.T) {
	isolatePrefs(t)
	fake := seedBoard()
	b := newBoard(fake)
	b.toastLog = []toastEntry{}
	// frameCtx nil → emitToast still records, AddToast skipped.
	b.okToast("已创建", "#1 demo")
	b.infoToast("Git 提交跳过", "项目未设置工作目录")
	b.toast("创建失败", "boom")
	if len(b.toastLog) != 3 {
		t.Fatalf("toastLog len=%d want 3: %+v", len(b.toastLog), b.toastLog)
	}
	want := []string{"success", "info", "error"}
	for i, k := range want {
		if b.toastLog[i].Kind != k {
			t.Fatalf("toast[%d] kind=%q want %q (%+v)", i, b.toastLog[i].Kind, k, b.toastLog[i])
		}
	}
	if b.toastLog[0].Title != "已创建" || b.toastLog[2].Title != "创建失败" {
		t.Fatalf("unexpected titles: %+v", b.toastLog)
	}
}

func TestCreateSuccessUsesOkToast(t *testing.T) {
	isolatePrefs(t)
	fake := seedBoard()
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
	settle(t, tt)
	b.toastLog = []toastEntry{}
	// empty kind → createRelatedTask success path shows 「已创建」.
	b.createRelatedTask("t-1", "", "分流成功提示")
	waitFor(t, tt, "已创建 toast", func() bool {
		for _, e := range b.toastLog {
			if e.Title == "已创建" && e.Kind == "success" {
				return true
			}
		}
		return false
	})
}

func TestGitCommitCommentFailureToastsPartialSuccess(t *testing.T) {
	isolatePrefs(t)
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	// Avoid depending on default branch name.
	run("checkout", "-b", "main")
	tracked := filepath.Join(dir, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "tracked.txt")
	run("commit", "-m", "seed")
	if err := os.WriteFile(tracked, []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fake := seedBoard()
	fake.projects[0].WorkspacePath = dir
	fake.commentFail = errors.New("评论服务不可用")
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
	settle(t, tt)
	b.toastLog = []toastEntry{}
	b.prefs.ProjectGitCommitOnDone = map[string]bool{"local": true}
	task := *b.task("t-1")
	task.ColumnID = "c-done"
	b.maybeGitCommitOnDone(task)

	waitFor(t, tt, "Git 部分成功 toast", func() bool {
		for _, e := range b.toastLog {
			if e.Title == "已提交 Git，评论写入失败" && e.Kind == "error" {
				return true
			}
		}
		return false
	})
	var sawOK, sawFail bool
	for _, e := range b.toastLog {
		if e.Title == "已提交 Git" && e.Kind == "success" {
			sawOK = true
		}
		if e.Title == "已提交 Git，评论写入失败" {
			sawFail = true
			if !strings.Contains(e.Desc, "评论服务不可用") {
				t.Fatalf("fail desc=%q", e.Desc)
			}
		}
	}
	if !sawOK || !sawFail {
		t.Fatalf("want success then partial-fail toasts, got %+v", b.toastLog)
	}
	// UI should also surface the partial-success title.
	waitFor(t, tt, "评论写入失败可见", func() bool { return tt.HasText("已提交 Git，评论写入失败") })
}

func TestGitCommitSkipUsesInfoToast(t *testing.T) {
	isolatePrefs(t)
	fake := seedBoard()
	// no WorkspacePath
	b := newBoard(fake)
	b.toastLog = []toastEntry{}
	b.prefs.ProjectGitCommitOnDone = map[string]bool{"local": true}
	b.maybeGitCommitOnDone(model.Task{ID: "t-1", ProjectID: "local", Number: 1, Title: "x"})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, e := range b.toastLog {
			if e.Title == "Git 提交跳过" && e.Kind == "info" {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("skip toast missing: %+v", b.toastLog)
}
