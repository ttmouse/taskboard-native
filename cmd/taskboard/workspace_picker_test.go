package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo"
)

func TestWorkspacePickerDoesNotBlockAndSavesSelection(t *testing.T) {
	fake := seedBoard()
	f := &form{open: true, name: "local", editingProjectID: "local", workspacePath: "/old"}
	b := &board{api: fake, form: f, win: &mygo.Window{}, msgs: make(chan func(), 64)}
	started := make(chan mygo.OpenDialogOptions, 2)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	b.openDialog = func(opts mygo.OpenDialogOptions) ([]string, error) {
		started <- opts
		<-release
		return []string{"/new/workspace"}, nil
	}
	// This must return while the native picker is still waiting for input.
	b.pickWorkspace(f)
	select {
	case opts := <-started:
		if opts.Parent != b.win || !opts.Directory || opts.DefaultPath != "/old" {
			t.Fatalf("picker options = %+v", opts)
		}
	case <-time.After(time.Second):
		t.Fatal("picker did not start")
	}
	b.pickWorkspace(f)
	// Only the UI thread may change the form after a native result.
	release <- struct{}{}
	select {
	case apply := <-b.msgs:
		if f.workspacePath != "/old" {
			t.Fatal("background picker mutated UI state")
		}
		apply()
	case <-time.After(time.Second):
		t.Fatal("picker did not deliver its result")
	}
	if f.workspacePath != "/new/workspace" || f.name != "local" || f.pickingWorkspace {
		t.Fatalf("selection not applied: %+v", f)
	}
	if len(started) != 0 {
		t.Fatal("repeated click opened another picker")
	}
	b.saveProjectEdit()
	select {
	case apply := <-b.msgs:
		apply()
	case <-time.After(time.Second):
		t.Fatal("save did not complete")
	}
	if fake.projects[0].WorkspacePath != "/new/workspace" {
		t.Fatalf("saved path = %q", fake.projects[0].WorkspacePath)
	}
}

func TestWorkspacePickerCancelAndStaleForm(t *testing.T) {
	for _, mode := range []string{"cancel", "replaced", "closed", "unnamed"} {
		t.Run(mode, func(t *testing.T) {
			f := &form{open: true, workspacePath: "/old"}
			b := &board{form: f, msgs: make(chan func(), 64)}
			b.openDialog = func(mygo.OpenDialogOptions) ([]string, error) {
				if mode == "cancel" {
					return nil, nil
				}
				return []string{"/new/workspace"}, nil
			}
			b.pickWorkspace(f)
			if mode == "replaced" {
				b.form = &form{open: true}
			} else if mode == "closed" {
				f.open = false
			}
			select {
			case apply := <-b.msgs:
				apply()
			case <-time.After(time.Second):
				t.Fatal("picker did not complete")
			}
			if mode == "unnamed" {
				if f.name != "workspace" || f.workspacePath != "/new/workspace" {
					t.Fatalf("unnamed form = %+v", f)
				}
			} else if f.workspacePath != "/old" || b.form != f && b.form.workspacePath != "" {
				t.Fatal("cancelled or stale result changed the form")
			}
		})
	}
}

// The 浏览… button in the project form must hand the picker to the board's
// dialog hook and leave the frame free: clicking it has to reach the panel,
// not stall the render callback that drew the button.
func TestProjectFormBrowseClickReachesPicker(t *testing.T) {
	isolatePrefs(t)
	fake := seedBoard()
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	started := make(chan mygo.OpenDialogOptions, 1)
	release := make(chan struct{})
	defer close(release)
	b.openDialog = func(opts mygo.OpenDialogOptions) ([]string, error) {
		started <- opts
		<-release
		return []string{"/tmp/新项目"}, nil
	}
	b.form = &form{open: true}
	tt.Frame()
	if err := tt.Click("浏览…"); err != nil {
		t.Fatal(err)
	}
	// The frame that drew the button returned already; only the picker waits.
	tt.Frame()
	select {
	case opts := <-started:
		if opts.Parent != b.win && b.win != nil {
			t.Fatal("picker opened without the board window")
		}
		if !opts.Directory {
			t.Fatal("picker is not a directory picker")
		}
	case <-time.After(time.Second):
		t.Fatal("浏览… did not open the directory picker")
	}
	release <- struct{}{}
	waitFor(t, tt, "工作目录回填", func() bool { return tt.HasText("/tmp/新项目") })
	if b.form == nil || b.form.workspacePath != "/tmp/新项目" || b.form.name != "新项目" {
		t.Fatalf("picked directory not applied: %+v", b.form)
	}
}
