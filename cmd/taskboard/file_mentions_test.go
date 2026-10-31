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

func TestWorkspaceMentionFilterNeedsAQuery(t *testing.T) {
	targets := []workspaceMentionTarget{
		{Path: "main.go"},
		{Path: "docs/guide.md"},
	}
	if got := filterWorkspaceMentionTargets(targets, "", 20); len(got) != 0 {
		t.Fatalf("empty query listed %d files, want none", len(got))
	}
	if got := filterWorkspaceMentionTargets(targets, "  ", 20); len(got) != 0 {
		t.Fatalf("blank query listed %d files, want none", len(got))
	}
	got := filterWorkspaceMentionTargets(targets, "MAIN", 20)
	if len(got) != 1 || got[0].Path != "main.go" {
		t.Fatalf("query match = %+v", got)
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
	if got := b.detail.comment; got != "检查" {
		t.Fatalf("comment draft = %q, want plain text without duplicated path token", got)
	}
	if got := b.detail.commentFiles; len(got) != 1 || got[0] != "main.go" {
		t.Fatalf("selected file chips = %v", got)
	}
	if tt.HasText("@{main.go}") {
		t.Fatal("composer still shows raw @{path} alongside the badge")
	}
	if _, ok := tt.Find("移除文件提及 main.go"); !ok {
		t.Fatalf("comment file badge not visible inline; texts: %v", tt.Texts())
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
	if got := b.form.name; got != "update" {
		t.Fatalf("task title draft = %q, want plain text without duplicated path token", got)
	}
	if got := b.form.fileMentions; len(got) != 1 || got[0] != "docs" {
		t.Fatalf("compose file chips = %v", got)
	}
	if tt.HasText("@{docs}") {
		t.Fatal("compose field still shows raw @{path} alongside the badge")
	}
	if _, ok := tt.Find("移除文件提及 docs"); !ok {
		t.Fatalf("compose file badge not visible inline; texts: %v", tt.Texts())
	}
}

func TestQuickAddFileMentionShowsChipWithoutRawToken(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main"), 0o600); err != nil {
		t.Fatal(err)
	}
	isolatePrefs(t)
	if err := saveUIPrefs(uiPrefs{BoardVertical: true}); err != nil {
		t.Fatal(err)
	}
	fake := seedBoard()
	fake.projects[0].WorkspacePath = root
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)
	if err := tt.Click("切换到项目 local"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, "切换到项目", func() bool { return b.cur == "local" })
	focusQuickAdd(t, tt)
	tt.Type("@main")
	waitFor(t, tt, "快速添加文件建议出现", func() bool {
		_, ok := tt.Find("提及项目文件 main.go")
		return ok
	})
	if err := tt.Click("提及项目文件 main.go"); err != nil {
		t.Fatal(err)
	}
	if b.quickTitle != "" || len(b.quickFileMentions) != 1 || b.quickFileMentions[0] != "main.go" {
		t.Fatalf("quick add draft = %q, file chips = %v", b.quickTitle, b.quickFileMentions)
	}
	if tt.HasText("@{main.go}") {
		t.Fatal("quick add still shows raw @{path} alongside the badge")
	}
	if _, ok := tt.Find("移除文件提及 main.go"); !ok {
		t.Fatalf("quick add file badge is not visible; texts: %v", tt.Texts())
	}
}
