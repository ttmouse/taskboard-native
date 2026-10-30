package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSettingsShortcut(t *testing.T) {
	isolatePrefs(t)
	b := newBoard(seedBoard())
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	tt.Key(ui.Cmd, ui.KeyComma)
	tt.Frame()
	waitFor(t, tt, "⌘, 设置快捷键", func() bool { return tt.HasText("跟随系统") })
	if b.settings == nil || !b.settings.open {
		t.Fatal("⌘, did not open settings")
	}
}

// recordShortcut swaps the global-shortcut indirections for recorders and
// returns the recorded register/unregister accelerator calls. A headless
// test cannot call mygo.GlobalShortcut (needsApp panics before App.Run),
// so the switch's Changed path is exercised through these stand-ins.
func recordShortcut(t *testing.T) (*[]string, *[]string) {
	t.Helper()
	var regs, unregs []string
	prevIs := shortcutIsRegistered
	prevReg := shortcutRegister
	prevUn := shortcutUnregister
	shortcutIsRegistered = func(acc string) bool { return containsStr(regs, acc) }
	shortcutRegister = func(acc string, fn func()) error {
		regs = append(regs, acc)
		return nil
	}
	shortcutUnregister = func(acc string) { unregs = append(unregs, acc) }
	t.Cleanup(func() {
		shortcutIsRegistered = prevIs
		shortcutRegister = prevReg
		shortcutUnregister = prevUn
	})
	return &regs, &unregs
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// TestSettingsGlobalHotkeyToggle exercises the exact path the user's
// "快捷键不能" report hinges on: the settings switch's Changed handler must
// persist the preference and register/unregister the global shortcut (via
// the recorder stand-ins). The launch-time bind is simulated with an
// explicit syncGlobalHotkey call, mirroring the WhenReady call in main.go.
func TestSettingsGlobalHotkeyToggle(t *testing.T) {
	isolatePrefs(t)
	regs, unregs := recordShortcut(t)
	b := newBoard(seedBoard())
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	// Launch: a prefs file without the key defaults to ON, and the
	// WhenReady sync registers the shortcut (simulated here by the same
	// call main.go makes).
	if !b.prefs.GlobalNewTaskHot {
		t.Fatal("default GlobalNewTaskHot = false, want true")
	}
	b.syncGlobalHotkey()
	if !containsStr(*regs, globalNewTaskAcc) {
		t.Fatalf("shortcut not registered at launch; regs=%v", *regs)
	}
	if len(*unregs) != 0 {
		t.Fatalf("unexpected unregister at launch; unregs=%v", *unregs)
	}

	// Open settings and click the switch: OFF → unregister + persist.
	tt.Key(ui.Cmd, ui.KeyComma)
	tt.Frame()
	waitFor(t, tt, "设置弹层", func() bool { return tt.HasText("跟随系统") })
	if err := tt.Click("全局热键新建任务"); err != nil {
		t.Fatalf("click 全局热键新建任务: %v", err)
	}
	tt.Frame()
	if b.prefs.GlobalNewTaskHot {
		t.Fatal("switch still on after click, want off")
	}
	if !containsStr(*unregs, globalNewTaskAcc) {
		t.Fatalf("shortcut not unregistered after switching off; unregs=%v", *unregs)
	}
	if got := loadUIPrefs(); got.GlobalNewTaskHot {
		t.Fatal("saved prefs still GlobalNewTaskHot = true, want false")
	}

	// Click again: back ON → register + persist.
	if err := tt.Click("全局热键新建任务"); err != nil {
		t.Fatalf("click 全局热键新建任务 again: %v", err)
	}
	tt.Frame()
	if !b.prefs.GlobalNewTaskHot {
		t.Fatal("switch off after second click, want on")
	}
	if !containsStr(*regs, globalNewTaskAcc) {
		t.Fatalf("shortcut not re-registered after switching on; regs=%v", *regs)
	}
	if got := loadUIPrefs(); !got.GlobalNewTaskHot {
		t.Fatal("saved prefs GlobalNewTaskHot = false, want true")
	}
}

// TestSettingsGlobalHotkeyFailure pins the failed-registration path: when
// mygo refuses the accelerator (another app owns ⌘⇧N), the preference
// reverts to false, the error lands on b.hotkeyErr, and the settings sheet
// renders it — the launch-time failure that otherwise has no frame context
// for a toast.
func TestSettingsGlobalHotkeyFailure(t *testing.T) {
	isolatePrefs(t)
	prevIs := shortcutIsRegistered
	prevReg := shortcutRegister
	shortcutIsRegistered = func(acc string) bool { return false }
	shortcutRegister = func(acc string, fn func()) error { return errHotkeyBusy }
	t.Cleanup(func() {
		shortcutRegister = prevReg
		shortcutIsRegistered = prevIs
	})

	b := newBoard(seedBoard())
	b.syncGlobalHotkey() // launch-time bind, fails
	if b.prefs.GlobalNewTaskHot {
		t.Fatal("pref not reverted after registration failure, want false")
	}
	if b.hotkeyErr == "" {
		t.Fatal("hotkeyErr empty after registration failure")
	}
	if !strings.Contains(b.hotkeyErr, "占用") {
		t.Fatalf("hotkeyErr %q does not explain the conflict", b.hotkeyErr)
	}
	if got := loadUIPrefs(); got.GlobalNewTaskHot {
		t.Fatal("saved prefs GlobalNewTaskHot = true after failure, want false")
	}

	// The settings sheet must surface the error text.
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)
	tt.Key(ui.Cmd, ui.KeyComma)
	tt.Frame()
	waitFor(t, tt, "设置弹层", func() bool { return tt.HasText("跟随系统") })
	if !tt.HasText(b.hotkeyErr) {
		t.Fatalf("settings sheet does not show hotkey error %q; texts=%v", b.hotkeyErr, tt.Texts())
	}
}

// errHotkeyBusy is the failure the failure test injects: the accelerator
// belongs to another application.
var errHotkeyBusy = &hotkeyBusyError{}

type hotkeyBusyError struct{}

func (*hotkeyBusyError) Error() string { return "shortcut is already registered by another app" }
