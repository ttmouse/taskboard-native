package executor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"taskboard-native/internal/model"
)

// installFakeHerdr adds deterministic Herdr/HC commands to PATH. The caller
// supplies the one workspace the fake Herdr exposes as an existing space.
func installFakeHerdr(t *testing.T, folder, state string) (argsLog, sendsLog string) {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate fake Herdr fixture")
	}
	binDir := filepath.Join(filepath.Dir(source), "..", "..", "testdata", "fake-herdr")
	logDir := t.TempDir()
	argsLog = filepath.Join(logDir, "herdr-args")
	sendsLog = filepath.Join(logDir, "hc-sends")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TASKBOARD_TEST_HERDR_FOLDER", folder)
	t.Setenv("TASKBOARD_TEST_HERDR_STATE", state)
	t.Setenv("TASKBOARD_TEST_HERDR_ARGS", argsLog)
	t.Setenv("TASKBOARD_TEST_HC_SENDS", sendsLog)
	t.Setenv("TASKBOARD_TEST_HERDR_COUNTER", filepath.Join(logDir, "counter"))
	return argsLog, sendsLog
}

func TestHerdrTerminalAddress(t *testing.T) {
	got, err := herdrTerminalAddress([]byte(`{"space":"s1","terminal":"t7"}`))
	if err != nil || got != "t7" {
		t.Fatalf("herdrTerminalAddress() = %q, %v", got, err)
	}
	if _, err := herdrTerminalAddress([]byte(`{"space":"s1"}`)); err == nil {
		t.Fatal("expected missing terminal address to fail")
	}
}

func TestHerdrSessionMarkerAndStatus(t *testing.T) {
	if address, ok := herdrAddress("herdr:t7"); !ok || address != "t7" {
		t.Fatalf("herdrAddress() = %q, %v", address, ok)
	}
	if _, ok := herdrAddress("legacy-cli-session"); ok {
		t.Fatal("legacy CLI session was classified as Herdr")
	}
	var status herdrStatus
	status.Status.State = "blocked"
	if got := herdrState(status); got != "blocked" {
		t.Fatalf("herdrState() = %q", got)
	}
}

func TestHerdrSpaceForFolderRequiresExactExistingSpace(t *testing.T) {
	raw := []byte(`{"spaces":[{"id":"project-space","name":"Project","folder":"/work/project"},{"id":"home-space","name":"Home","folder":"/Users/example"}]}`)
	if got, err := herdrSpaceForFolder(raw, "/work/project", "Project"); err != nil || got != "project-space" {
		t.Fatalf("herdrSpaceForFolder() = %q, %v", got, err)
	}
	if _, err := herdrSpaceForFolder(raw, "/work/other", "Other"); err == nil {
		t.Fatal("expected an unmatched project folder to fail")
	}
}

func TestHerdrSpaceForFolderFallsBackToUniqueActiveProjectName(t *testing.T) {
	raw := []byte(`{"spaces":[{"id":"inactive","name":"Project","folder":"/Users/example","active":false},{"id":"active","name":"Project","folder":"/Users/example","active":true}]}`)
	if got, err := herdrSpaceForFolder(raw, "/Users/example/Projects/Project", "Project"); err != nil || got != "active" {
		t.Fatalf("herdrSpaceForFolder() = %q, %v", got, err)
	}
}

func TestHerdrSpaceForFolderDoesNotReuseSameNameAtSiblingPath(t *testing.T) {
	raw := []byte(`{"spaces":[{"id":"unrelated","name":"Project","folder":"/work/other"}]}`)
	if _, err := herdrSpaceForFolder(raw, "/work/project", "Project"); !errors.Is(err, errHerdrSpaceNotFound) {
		t.Fatalf("herdrSpaceForFolder() error = %v, want missing project space", err)
	}
}

func TestHerdrCreateAddsTabToProjectSpace(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	command := filepath.Join(dir, "herdr-app")
	script := "#!/bin/sh\nif [ \"$1\" = ls ]; then printf '%s' '{\"spaces\":[{\"id\":\"project-space\",\"folder\":\"/work/project\"}]}'; exit 0; fi\nprintf '%s\\n' \"$@\" > \"$ARGS_PATH\"\nprintf '%s' '{\"terminal\":\"t9\"}'\n"
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ARGS_PATH", argsPath)
	got, err := herdrCreate(context.Background(), model.Task{ID: "task-1", Title: "Implement feature"}, "Project", "/work/project", RuntimeCodex, "http://127.0.0.1:47826", "Codex", "task prompt")
	if err != nil || got != "herdr:t9" {
		t.Fatalf("herdrCreate() = %q, %v", got, err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"do\ntab.new", "--space\nproject-space", "--folder\n/work/project", "TASKBOARD_ADDR=127.0.0.1:47826", "TB_AGENT=Codex", "sandbox_workspace_write.network_access=true", "task prompt"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("Herdr args missing %q: %s", want, args)
		}
	}
	if strings.Contains(string(args), "space.new") {
		t.Fatalf("created a new space instead of reusing the project space: %s", args)
	}
}

func TestHerdrCreateMakesProjectSpaceWhenMissing(t *testing.T) {
	dir := t.TempDir()
	spacesPath := filepath.Join(dir, "spaces.json")
	spaceLog := filepath.Join(dir, "space-args")
	tabLog := filepath.Join(dir, "tab-args")
	command := filepath.Join(dir, "herdr-app")
	script := `#!/bin/sh
if [ "$1" = ls ]; then
	if [ -f "$SPACES_PATH" ]; then cat "$SPACES_PATH"; else printf '%s' '{"spaces":[{"id":"sibling-space","name":"Project","folder":"/work/other"}]}'; fi
	exit 0
fi
if [ "$1" = do ] && [ "$2" = space.new ]; then
	printf '%s\n' "$@" > "$SPACE_LOG"
	printf '%s' '{"spaces":[{"id":"sibling-space","name":"Project","folder":"/work/other"},{"id":"created-space","name":"Project","folder":"/work/project"}]}' > "$SPACES_PATH"
	exit 0
fi
if [ "$1" = do ] && [ "$2" = tab.new ]; then
	printf '%s\n' "$@" > "$TAB_LOG"
	printf '%s' '{"terminal":"t-created"}'
	exit 0
fi
exit 2
`
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SPACES_PATH", spacesPath)
	t.Setenv("SPACE_LOG", spaceLog)
	t.Setenv("TAB_LOG", tabLog)
	got, err := herdrCreate(context.Background(), model.Task{ID: "task-2", Title: "Implement feature"}, "Project", "/work/project", RuntimeCodex, "http://127.0.0.1:47826", "Codex", "task prompt")
	if err != nil || got != "herdr:t-created" {
		t.Fatalf("herdrCreate() = %q, %v", got, err)
	}
	createdArgs, err := os.ReadFile(spaceLog)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"do\nspace.new", "--name\nProject", "--folder\n/work/project"} {
		if !strings.Contains(string(createdArgs), want) {
			t.Errorf("space.new args missing %q: %s", want, createdArgs)
		}
	}
	tabArgs, err := os.ReadFile(tabLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tabArgs), "do\ntab.new") || !strings.Contains(string(tabArgs), "--space\ncreated-space") {
		t.Fatalf("task tab was not created in the new project space: %s", tabArgs)
	}
}

func TestAutomaticIntakeUsesHerdrForCodex(t *testing.T) {
	binDir := t.TempDir()
	st := newTestStore(t)
	project, task := projectWithWorkspace(t, st)
	spaceJSON, err := json.Marshal(map[string]any{"spaces": []map[string]string{{"id": "project-space", "folder": project.WorkspacePath}}})
	if err != nil {
		t.Fatal(err)
	}
	herdrScript := "#!/bin/sh\nif [ \"$1\" = ls ]; then printf '%s' '" + string(spaceJSON) + "'; exit 0; fi\nprintf '%s' '{\"terminal\":\"t-auto\"}'\n"
	if err := os.WriteFile(filepath.Join(binDir, "herdr-app"), []byte(herdrScript), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "hc"), []byte("#!/bin/sh\nprintf '%s' '{\"status\":{\"state\":\"done\"}}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	d := NewDispatcher(st)
	t.Cleanup(d.Close)
	if _, err := d.StartProjectRun(project.ID, RuntimeCodex, "auto-test"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "automatic Herdr session", func() (bool, string) {
		run, err := d.store.Execution(task.ID)
		if err != nil || run == nil {
			return false, "execution not saved"
		}
		return run.SessionID == "herdr:t-auto", run.SessionID + " " + run.Status
	})
	if _, ok := herdrAddress("herdr:t-auto"); !ok {
		t.Fatal("automatic intake did not persist a Herdr terminal address")
	}
}

func TestTrackHerdrDoesNotTreatInitialIdleAsReturned(t *testing.T) {
	binDir := t.TempDir()
	counter := filepath.Join(t.TempDir(), "hc-count")
	script := "#!/bin/sh\ncount=0\n[ -f \"$COUNTER\" ] && count=$(cat \"$COUNTER\")\ncount=$((count+1))\nprintf '%s' \"$count\" > \"$COUNTER\"\nif [ \"$count\" -eq 1 ]; then printf '%s' '{\"status\":{\"state\":\"idle\"},\"turn\":0}'; else printf '%s' '{\"status\":{\"state\":\"working\"},\"turn\":1}'; fi\n"
	if err := os.WriteFile(filepath.Join(binDir, "hc"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("COUNTER", counter)
	st := newTestStore(t)
	_, task := projectWithWorkspace(t, st)
	run := &model.Execution{TaskID: task.ID, Runtime: RuntimeCodex, SessionID: "herdr:t-startup", Status: "submitting"}
	if err := st.SaveExecution(*run); err != nil {
		t.Fatal(err)
	}
	d := &Dispatcher{store: st}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.trackHerdr(ctx, run)
	}()
	t.Cleanup(func() { cancel(); <-done })

	waitFor(t, "first Herdr status", func() (bool, string) {
		count, _ := os.ReadFile(counter)
		persisted, _ := st.Execution(task.ID)
		if string(count) != "1" {
			return false, string(count)
		}
		return persisted != nil && persisted.Status == "running" && strings.Contains(persisted.Detail, "等待初始 turn"), string(count) + " " + persisted.Status
	})

	waitFor(t, "Herdr first active turn", func() (bool, string) {
		count, _ := os.ReadFile(counter)
		persisted, _ := st.Execution(task.ID)
		return string(count) == "2" && persisted != nil && persisted.Status == "running" && persisted.Detail == "", string(count)
	})
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Herdr tracking did not stop after cancellation")
	}
	persisted, err := st.Execution(task.ID)
	if err != nil || persisted == nil || persisted.Status == "returned" {
		t.Fatalf("initial idle incorrectly ended the run: %#v, %v", persisted, err)
	}
}

func TestNewDispatcherRecoversReturnedHerdrSessionStillWorking(t *testing.T) {
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "hc"), []byte("#!/bin/sh\nprintf '%s' '{\"status\":{\"state\":\"working\"},\"turn\":1}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	st := newTestStore(t)
	_, task := projectWithWorkspace(t, st)
	if err := st.SaveExecution(model.Execution{TaskID: task.ID, Runtime: RuntimeCodex, SessionID: "herdr:t-live", Status: "returned", Detail: "Herdr agent turn 已结束"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProjectRun(model.ProjectRun{ProjectID: task.ProjectID, Runtime: RuntimeCodex, Status: "failed", TaskIDs: []string{task.ID}, Cursor: 1, CurrentTaskID: task.ID, Detail: "卡片 " + task.ID + " 执行结束但仍在进行中，承接停止"}); err != nil {
		t.Fatal(err)
	}
	d := NewDispatcher(st)
	t.Cleanup(d.Close)
	waitFor(t, "returned Herdr session recovery", func() (bool, string) {
		run, err := st.Execution(task.ID)
		if err != nil || run == nil {
			return false, "execution missing"
		}
		return run.Status == "running", run.Status + " " + run.Detail
	})
	if err := d.Dispatch(task.ID, RuntimeCodex, "retry"); err != ErrInProgress {
		t.Fatalf("Dispatch() = %v, want ErrInProgress for the existing Herdr session", err)
	}
	projectRun, err := st.ProjectRun(task.ProjectID)
	if err != nil || projectRun.Status != "running" {
		t.Fatalf("ProjectRun() = %#v, %v; want the prior auto-accept run restored", projectRun, err)
	}
}

func TestNewDispatcherContinuesAutoRunAfterReturnedHerdrCard(t *testing.T) {
	binDir := t.TempDir()
	script := "#!/bin/sh\nif [ \"$2\" = t-completed ]; then printf '%s' '{\"status\":{\"state\":\"done\"},\"turn\":4}'; else printf '%s' '{\"status\":{\"state\":\"working\"},\"turn\":1}'; fi\n"
	if err := os.WriteFile(filepath.Join(binDir, "hc"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	st := newTestStore(t)
	project, completed := projectWithWorkspace(t, st)
	intake, ok := project.ColumnByRole(model.ColumnRoleIntake)
	if !ok {
		t.Fatal("project has no intake column")
	}
	next, err := st.CreateTask(project.ID, intake.ID, "后续卡片", "", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	doing, _ := project.ColumnByRole(model.ColumnRoleDoing)
	review, _ := project.ColumnByRole(model.ColumnRoleReview)
	completed, err = st.MoveTask(completed.ID, completed.Version, review.ID, "", "Codex")
	if err != nil {
		t.Fatal(err)
	}
	next, err = st.MoveTask(next.ID, next.Version, doing.ID, "", "Codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveExecution(model.Execution{TaskID: completed.ID, Runtime: RuntimeCodex, SessionID: "herdr:t-completed", Status: "returned"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveExecution(model.Execution{TaskID: next.ID, Runtime: RuntimeCodex, SessionID: "herdr:t-next", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProjectRun(model.ProjectRun{ProjectID: project.ID, Runtime: RuntimeCodex, Status: "failed", TaskIDs: []string{completed.ID, next.ID}, Cursor: 1, CurrentTaskID: completed.ID, Detail: "卡片 " + completed.ID + " 执行结束但仍在进行中，承接停止"}); err != nil {
		t.Fatal(err)
	}
	d := NewDispatcher(st)
	t.Cleanup(d.Close)
	run, err := st.ProjectRun(project.ID)
	if err != nil || run.Status != "running" || run.CurrentTaskID != next.ID || run.Cursor != 2 {
		t.Fatalf("ProjectRun() = %#v, %v; want continuation on existing next session", run, err)
	}
}

func TestNewDispatcherRestoresAutoRunWhenReturnedCardStillDoing(t *testing.T) {
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "hc"), []byte("#!/bin/sh\nprintf '%s' '{\"status\":{\"state\":\"done\"},\"turn\":2}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	st := newTestStore(t)
	project, task := projectWithWorkspace(t, st)
	doing, _ := project.ColumnByRole(model.ColumnRoleDoing)
	if _, err := st.MoveTask(task.ID, task.Version, doing.ID, "", "Codex"); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveExecution(model.Execution{TaskID: task.ID, Runtime: RuntimeCodex, SessionID: "herdr:t-returned", Status: "returned"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProjectRun(model.ProjectRun{
		ProjectID: project.ID, Runtime: RuntimeCodex, Status: "failed",
		TaskIDs: []string{task.ID}, Cursor: 1, CurrentTaskID: task.ID,
		Detail: "卡片 " + task.ID + " 执行结束但仍在进行中，承接停止",
	}); err != nil {
		t.Fatal(err)
	}

	d := NewDispatcher(st)
	t.Cleanup(d.Close)
	run, err := st.ProjectRun(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !run.Running() || run.CurrentTaskID != task.ID || !strings.Contains(run.Detail, "等待卡片") {
		t.Fatalf("ProjectRun() = %#v; expected restart to restore enabled state and wait on the existing card", run)
	}
}
