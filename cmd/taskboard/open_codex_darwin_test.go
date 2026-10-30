//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"taskboard-native/internal/executor"
	"taskboard-native/internal/model"
)

func installFakeHerdrOpen(t *testing.T, dir string) string {
	t.Helper()
	argsPath := filepath.Join(dir, "open-args")
	command := filepath.Join(dir, "open")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$OPEN_ARGS_PATH\"\n"
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPEN_ARGS_PATH", argsPath)
	return argsPath
}

func assertHerdrWindowForeground(t *testing.T, argsPath string) {
	t.Helper()
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(args)), "-b\ndev.herdr.app"; got != want {
		t.Fatalf("Herdr activation args = %q, want %q", got, want)
	}
}

func TestOpenCodexSessionFocusesHerdrTerminal(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	openArgsPath := installFakeHerdrOpen(t, dir)
	command := filepath.Join(dir, "herdr-app")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ARGS_PATH\"\n"
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ARGS_PATH", argsPath)
	if err := openCodexSession("", "herdr:t123"); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(args)), "do\nagent.focus\n--terminal\nt123"; got != want {
		t.Fatalf("Herdr focus args = %q, want %q", got, want)
	}
	assertHerdrWindowForeground(t, openArgsPath)
}

func TestOpenCodexSessionFindsHerdrOutsideFinderPath(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	argsPath := filepath.Join(home, "args")
	openDir := t.TempDir()
	openArgsPath := installFakeHerdrOpen(t, openDir)
	command := filepath.Join(dir, "herdr-app")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ARGS_PATH\"\n"
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", openDir+string(os.PathListSeparator)+"/usr/bin:/bin")
	t.Setenv("ARGS_PATH", argsPath)
	if err := openCodexSession("", "herdr:t123"); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(args)), "do\nagent.focus\n--terminal\nt123"; got != want {
		t.Fatalf("Herdr focus args = %q, want %q", got, want)
	}
	assertHerdrWindowForeground(t, openArgsPath)
}

func TestClickingReturnedExecutionStatusDoesNotFocusHerdrTerminal(t *testing.T) {
	isolatePrefs(t)
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	openArgsPath := installFakeHerdrOpen(t, dir)
	command := filepath.Join(dir, "herdr-app")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ARGS_PATH\"\n"
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ARGS_PATH", argsPath)

	fake := seedBoard()
	task := fake.tasks[0]
	task.ColumnID = "c-review"
	task.Execution = &model.Execution{
		TaskID: task.ID, SessionID: "herdr:t123", Runtime: executor.RuntimeCodex, Status: "returned",
	}
	fake.tasks[0] = task
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "执行状态图标", func() bool { return tt.HasText("执行已返回") })
	if err := tt.Click("Herdr 执行状态：执行已返回"); err != nil {
		t.Fatalf("click execution status: %v", err)
	}
	if _, err := os.Stat(argsPath); !os.IsNotExist(err) {
		t.Fatalf("status icon unexpectedly focused Herdr; stat err=%v", err)
	}
	if _, err := os.Stat(openArgsPath); !os.IsNotExist(err) {
		t.Fatalf("status icon unexpectedly activated Herdr; stat err=%v", err)
	}
	if b.detail == nil {
		t.Fatal("status icon click did not follow the card's normal detail action")
	}
}

func TestActiveHerdrExecutionCanBeFocusedFromTaskMenu(t *testing.T) {
	isolatePrefs(t)
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	openArgsPath := installFakeHerdrOpen(t, dir)
	command := filepath.Join(dir, "herdr-app")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ARGS_PATH\"\n"
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ARGS_PATH", argsPath)

	fake := seedBoard()
	task := fake.tasks[0]
	task.Execution = &model.Execution{
		TaskID: task.ID, SessionID: "herdr:t123", Runtime: executor.RuntimeCodex, Status: "running",
	}
	fake.tasks[0] = task
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return len(b.tasks) > 0 })
	if err := tt.RightClick("打开任务：把看板接上 SSE"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("在 Herdr 中定位 Codex 对话"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, "定位活动 Herdr terminal", func() bool {
		_, err := os.Stat(argsPath)
		return err == nil
	})
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(args)), "do\nagent.focus\n--terminal\nt123"; got != want {
		t.Fatalf("Herdr focus args = %q, want %q", got, want)
	}
	waitFor(t, tt, "Herdr window activation", func() bool {
		_, err := os.Stat(openArgsPath)
		return err == nil
	})
	assertHerdrWindowForeground(t, openArgsPath)
}

func TestActiveHerdrConversationIconOpensExistingTerminal(t *testing.T) {
	isolatePrefs(t)
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	openArgsPath := installFakeHerdrOpen(t, dir)
	command := filepath.Join(dir, "herdr-app")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ARGS_PATH\"\n"
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ARGS_PATH", argsPath)

	fake := seedBoard()
	task := fake.tasks[0]
	task.Execution = &model.Execution{
		TaskID: task.ID, SessionID: "herdr:t123", Runtime: executor.RuntimeCodex, Status: "running",
	}
	fake.tasks[0] = task
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "活动 Herdr Codex 对话图标", func() bool {
		return tt.HasText("打开 Herdr 中的 Codex 对话")
	})
	if err := tt.Click("打开 Herdr 中的 Codex 对话"); err != nil {
		t.Fatalf("click conversation icon: %v", err)
	}
	waitFor(t, tt, "定位原 Herdr terminal", func() bool {
		_, err := os.Stat(argsPath)
		return err == nil
	})
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(args)), "do\nagent.focus\n--terminal\nt123"; got != want {
		t.Fatalf("Herdr focus args = %q, want %q", got, want)
	}
	waitFor(t, tt, "Herdr window activation", func() bool {
		_, err := os.Stat(openArgsPath)
		return err == nil
	})
	assertHerdrWindowForeground(t, openArgsPath)
	if b.detail != nil {
		t.Fatalf("clicking the conversation icon opened task detail: %+v", b.detail)
	}
}
