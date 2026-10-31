package executor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func installFakeRuntime(t *testing.T, runtime, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, runtime), []byte("#!/bin/sh\n"+script+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestRuntimeCommands(t *testing.T) {
	dir := t.TempDir()
	prompt := "中文任务; $(literal)\n第二行"
	for _, tc := range []struct {
		id   string
		args []string
	}{

		{RuntimeCodex, []string{"exec", "--cd", dir, "--sandbox", "workspace-write", "--skip-git-repo-check", "--color", "never", "--", prompt}},
		{RuntimePi, []string{"--print", "--", prompt}},
		{RuntimeClaude, []string{"--print", "--permission-mode", "acceptEdits", "--allowedTools", "Bash", "--", prompt}},
	} {
		t.Run(tc.id, func(t *testing.T) {
			installFakeRuntime(t, tc.id, `printf '%s\n' "$PWD" "$@"`)
			cmd, err := execCommand(context.Background(), dir, prompt, tc.id)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(cmd.Args[1:], tc.args) {
				t.Fatalf("args: %q", cmd.Args)
			}
			out, err := cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(out), dir+"\n") || !strings.Contains(string(out), prompt) {
				t.Fatalf("cwd or literal prompt changed: %q", out)
			}
		})
	}
}

func TestAllRuntimeOutcomes(t *testing.T) {
	for _, runtime := range Runtimes() {
		if runtime.ID == RuntimeDSH {
			continue
		} // Desktop lifecycle has socket-based tests.
		t.Run(runtime.ID, func(t *testing.T) {
			st := newTestStore(t)
			p, task := projectWithWorkspace(t, st)
			argsLog, _ := installFakeHerdr(t, p.WorkspacePath, "done")
			d := NewDispatcher(st)
			defer d.Close()
			if err := d.Dispatch(task.ID, runtime.ID, "runtime-test"); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "Herdr session returned", func() (bool, string) {
				run, err := st.Execution(task.ID)
				return err == nil && run != nil && run.Status == "returned", fmt.Sprint(run, err)
			})
			run, err := st.Execution(task.ID)
			if err != nil || run.Channel() != runtime.ID || run.SessionID != "herdr:test-terminal-1" {
				t.Fatalf("execution = %+v, err=%v", run, err)
			}
			cur, _ := st.Task(task.ID)
			doing, _ := p.ColumnByRole("doing")
			if cur.ColumnID != doing.ID || cur.Version != task.Version+1 {
				t.Fatalf("accepted dispatch did not move task into doing: %+v", cur)
			}
			comments, _ := st.Comments(task.ID)
			if len(comments) != 2 || !strings.Contains(comments[0].Text, "派发 "+runtime.ID+" 执行") || !strings.Contains(comments[1].Text, "已交给 Herdr") {
				t.Fatalf("dispatch comments = %+v", comments)
			}
			args, err := os.ReadFile(argsLog)
			if err != nil || !strings.Contains(string(args), "--folder\n"+p.WorkspacePath) || !strings.Contains(string(args), runtime.ID) {
				t.Fatalf("Herdr launch args = %q, err=%v", args, err)
			}
		})
	}
}

func TestAllRuntimeCancellation(t *testing.T) {
	for _, runtime := range Runtimes() {
		if runtime.ID == RuntimeDSH {
			continue
		} // Desktop lifecycle has socket-based tests.
		t.Run(runtime.ID, func(t *testing.T) {
			st := newTestStore(t)
			p, task := projectWithWorkspace(t, st)
			_, sendsLog := installFakeHerdr(t, p.WorkspacePath, "working")
			d := NewDispatcher(st)
			defer d.Close()
			if err := d.Dispatch(task.ID, runtime.ID, "runtime-test"); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "Herdr session tracking", func() (bool, string) {
				run, err := st.Execution(task.ID)
				return err == nil && run != nil && run.Status == "running", fmt.Sprint(run, err)
			})
			d.Close()
			run, err := st.Execution(task.ID)
			if err != nil || run == nil || (run.Status != "running" && run.Status != "disconnected") || !strings.HasPrefix(run.SessionID, "herdr:") {
				t.Fatalf("closing tracking changed the Herdr session: %+v %v", run, err)
			}
			cur, _ := st.Task(task.ID)
			doing, _ := p.ColumnByRole("doing")
			if cur.ColumnID != doing.ID || cur.Version != task.Version+1 {
				t.Fatalf("accepted dispatch did not retain task in doing after closing tracking: %+v", cur)
			}
			if _, err := os.Stat(sendsLog); !os.IsNotExist(err) {
				t.Fatalf("dispatcher unexpectedly sent to or killed the Herdr terminal: %v", err)
			}
		})
	}
}
