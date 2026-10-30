package main

import (
	"log"

	"github.com/egoist/mygo"
)

// globalNewTaskAcc is the system-wide shortcut that opens the create-task
// sheet while the app is in the background. It deliberately includes Shift
// so it never collides with the in-window ⌘N.
const globalNewTaskAcc = "CmdOrCtrl+Shift+N"

// The mygo shortcut calls go through these indirections so render tests
// can swap in a recorder: mygo.GlobalShortcut.* needs a running app
// (needsApp panics before App.Run), and a headless test has none. The
// setters mirror the module's surface exactly.
var (
	shortcutIsRegistered = func(acc string) bool { return mygo.GlobalShortcut.IsRegistered(acc) }
	shortcutRegister     = func(acc string, fn func()) error { return mygo.GlobalShortcut.Register(acc, fn) }
	shortcutUnregister   = func(acc string) { mygo.GlobalShortcut.Unregister(acc) }
)

// syncGlobalHotkey registers or unregisters the global ⌘⇧N shortcut to match
// the saved preference. It is called after the app is ready (launch) and on
// every toggle in the settings sheet, so the state follows the user's switch
// without a relaunch.
func (b *board) syncGlobalHotkey() {
	on := b.prefs.GlobalNewTaskHot
	if shortcutIsRegistered(globalNewTaskAcc) {
		if on {
			return
		}
		shortcutUnregister(globalNewTaskAcc)
		return
	}
	if !on {
		return
	}
	// The callback must run on the main thread — mygo guarantees that — and
	// must show the window before opening the sheet, so a background ⌘⇧N
	// brings the app forward instead of acting invisibly.
	if err := shortcutRegister(globalNewTaskAcc, func() {
		if b.win != nil {
			b.win.Show()
			b.win.Focus()
		}
		b.openTaskForm(b.defaultTaskColumnID())
	}); err != nil {
		// The accelerator is likely owned by another app (or a previous
		// window of ours that never unregistered). Revert the preference so
		// the switch tells the truth; the settings sheet then shows why.
		b.prefs.GlobalNewTaskHot = false
		b.hotkeyErr = "⌘⇧N 已被占用或系统不允许：" + err.Error()
		_ = saveUIPrefs(b.prefs)
		log.Printf("global shortcut %s unavailable: %v", globalNewTaskAcc, err)
		// A toast is how a mid-session toggle reports failure, but at
		// launch the first frame may not exist yet (frameCtx is nil), so
		// the error is also persisted on b.hotkeyErr for the settings
		// sheet to render until the user closes it.
		b.toast("全局热键不可用", b.hotkeyErr)
		return
	}
	// Registration succeeded (or nothing was needed): clear any stale
	// failure note from a previous attempt.
	b.hotkeyErr = ""
}
