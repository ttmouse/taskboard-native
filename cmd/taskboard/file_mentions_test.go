package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceMentionTargetsAreBoundedAndSkipHiddenAndGeneratedPaths(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"src/main.go", ".secret", "node_modules/pkg/index.js", "vendor/lib.go"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "outside")); err != nil {
		t.Fatal(err)
	}
	targets := workspaceMentionTargets(root)
	for _, target := range targets {
		if strings.HasPrefix(target.Path, ".") || strings.HasPrefix(target.Path, "node_modules/") ||
			strings.HasPrefix(target.Path, "vendor/") || strings.HasPrefix(target.Path, "outside/") {
			t.Errorf("unexpected target %q", target.Path)
		}
	}
	if len(targets) == 0 || len(targets) > maxWorkspaceMentionTargets {
		t.Fatalf("target count = %d", len(targets))
	}
}

func TestWorkspaceMentionQueryAndInsertion(t *testing.T) {
	for _, tc := range []struct {
		text string
		want string
		ok   bool
	}{
		{"look at @src/ma", "src/ma", true},
		{"email me@home", "", false},
		{"@", "", true},
		{"say @a b", "", false},
	} {
		got, ok := workspaceMentionQuery(tc.text)
		if got != tc.want || ok != tc.ok {
			t.Errorf("workspaceMentionQuery(%q) = %q, %v; want %q, %v", tc.text, got, ok, tc.want, tc.ok)
		}
	}
	if got := insertWorkspaceMention("open @src/ma", "src/main.go"); got != "open @{src/main.go} " {
		t.Fatalf("insertWorkspaceMention = %q", got)
	}
}

func TestExpandWorkspaceMentionsOnlyLinksExistingInWorkspace(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a file.txt")
	if err := os.WriteFile(path, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := expandWorkspaceMentions("see @{a file.txt} and @{../../outside}", root)
	if !strings.Contains(got, "[📄 a file.txt](file://") {
		t.Fatalf("valid mention did not become a file link: %q", got)
	}
	if !strings.Contains(got, "@{../../outside}") {
		t.Fatalf("outside path should remain plain text: %q", got)
	}
}

func TestCommentFileMentionSuggestsAndInsertsProjectPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := newBoard(seedBoard())
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return len(b.tasks) > 0 && len(b.projects) > 0 })
	projectID := b.task("t-1").ProjectID
	for i := range b.projects {
		if b.projects[i].ID == projectID {
			b.projects[i].WorkspacePath = root
		}
	}
	b.openDetail(*b.task("t-1"))
	tt.Frame()
	waitFor(t, tt, "评论载入", func() bool { return !b.detail.loading })
	if err := tt.Click("写评论"); err != nil {
		t.Fatal(err)
	}
	tt.Type("检查 @main")
	waitFor(t, tt, "文件建议出现", func() bool {
		_, ok := tt.Find("提及项目文件 main.go")
		return ok
	})
	if err := tt.Click("提及项目文件 main.go"); err != nil {
		t.Fatal(err)
	}
	if got := b.detail.comment; got != "检查 @{main.go} " {
		t.Fatalf("comment draft = %q, want selected project token", got)
	}
	if got := workspaceMentionPaths(b.detail.comment); len(got) != 1 || got[0] != "main.go" {
		t.Fatalf("selected file chips = %v", got)
	}
}

func TestTaskComposeFileMentionSuggestsAndInsertsProjectPath(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	fake := seedBoard()
	fake.projects[0].WorkspacePath = root
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return len(b.tasks) > 0 })
	settle(t, tt)
	b.openTaskFormIn("local", "c-todo")
	tt.Frame()
	tt.Type("update @docs")
	waitFor(t, tt, "创建任务文件建议出现", func() bool {
		_, ok := tt.Find("提及项目文件 docs")
		return ok
	})
	if err := tt.Click("提及项目文件 docs"); err != nil {
		t.Fatal(err)
	}
	if got := b.form.name; got != "update @{docs} " {
		t.Fatalf("task title draft = %q, want selected project directory token", got)
	}
}
