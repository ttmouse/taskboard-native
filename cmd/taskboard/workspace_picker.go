package main

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
)

// pickWorkspace opens the native directory picker for the project form and
// writes the choice back on the UI thread.
//
// The sheet must not be awaited inside the render callback that clicked the
// button: that frame is what would show the panel, so waiting on it there
// leaves the click looking like nothing happened. It also belongs to the
// board's window (not a detached app-modal panel), so it cannot be lost
// behind the form.
func (b *board) pickWorkspace(f *form) {
	if f.pickingWorkspace {
		return
	}
	f.pickingWorkspace = true
	// Snapshot UI state before leaving the render callback.
	opts := mygo.OpenDialogOptions{
		Parent: b.win, Title: "选择项目工作目录", ButtonLabel: "选择",
		Directory: true, CreateDirectories: true, DefaultPath: f.workspacePath,
	}
	open := b.openDialog
	if open == nil {
		open = mygo.Dialog.Open
	}
	go func() {
		dirs, err := open(opts)
		b.post(func() {
			f.pickingWorkspace = false
			if b.form != f || !f.open {
				return
			}
			if err != nil {
				b.toast("无法选择目录", err.Error())
				return
			}
			if len(dirs) == 0 {
				return
			}
			f.workspacePath = dirs[0]
			if strings.TrimSpace(f.name) == "" {
				f.name = filepath.Base(dirs[0])
			}
		})
	}()
}
