package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
)

// TestMain isolates ui-prefs.json from the developer's real Application
// Support folder so render tests see the default (full) card layout.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "taskboard-ui-prefs-*")
	if err != nil {
		panic(err)
	}
	mygo.App.SetPath(mygo.PathUserData, dir)
	_ = os.Setenv("TASKBOARD_DB", filepath.Join(dir, "tb.db"))
	code := m.Run()
	mygo.App.SetPath(mygo.PathUserData, "")
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestUIPrefsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })
	t.Setenv("TASKBOARD_DB", "")

	got := loadUIPrefs()
	if got.TitleOnlyCards {
		t.Fatalf("default TitleOnlyCards = true, want false")
	}

	if err := saveUIPrefs(uiPrefs{TitleOnlyCards: true, LastTaskRuntime: "codex"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	path := filepath.Join(dir, uiPrefsFile)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("prefs file missing at %s: %v", path, err)
	}
	got = loadUIPrefs()
	if !got.TitleOnlyCards {
		t.Fatalf("loaded TitleOnlyCards = false, want true")
	}
	if got.LastTaskRuntime != "codex" {
		t.Fatalf("loaded LastTaskRuntime = %q, want codex", got.LastTaskRuntime)
	}

	if err := saveUIPrefs(uiPrefs{TitleOnlyCards: false}); err != nil {
		t.Fatalf("save off: %v", err)
	}
	got = loadUIPrefs()
	if got.TitleOnlyCards {
		t.Fatalf("loaded TitleOnlyCards = true after save off")
	}
}

func TestUIPrefsProjectSortRoundTrip(t *testing.T) {
	dir := t.TempDir()
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })
	t.Setenv("TASKBOARD_DB", "")

	if err := saveUIPrefs(uiPrefs{ProjectSort: "updated"}); err != nil {
		t.Fatalf("save project sort: %v", err)
	}
	if got := loadUIPrefs().ProjectSort; got != "updated" {
		t.Fatalf("loaded ProjectSort = %q, want updated", got)
	}

	if err := saveUIPrefs(uiPrefs{ProjectSort: "invalid"}); err != nil {
		t.Fatalf("save invalid project sort: %v", err)
	}
	if got := loadUIPrefs().ProjectSort; got != "" {
		t.Fatalf("invalid ProjectSort = %q after load, want default", got)
	}
}

// TestUIPrefsGlobalHotkeyRoundTrip covers the ⌘⇧N global-hotkey switch: a
// prefs file without the key (fresh install, or one written before the
// switch existed) defaults to ON — the user who never touched the setting
// expects ⌘⇧N to just work. Once saved explicitly, the flag survives a
// save/load cycle like every other pref, and OFF persists (no omitempty on
// this field, so "explicitly off" cannot be mistaken for "never set").
func TestUIPrefsGlobalHotkeyRoundTrip(t *testing.T) {
	dir := t.TempDir()
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })
	t.Setenv("TASKBOARD_DB", "")

	// No file → the key is absent → ON by default.
	if got := loadUIPrefs(); !got.GlobalNewTaskHot {
		t.Fatalf("default GlobalNewTaskHot = false, want true (feature on by default)")
	}
	if err := saveUIPrefs(uiPrefs{GlobalNewTaskHot: true}); err != nil {
		t.Fatalf("save on: %v", err)
	}
	if got := loadUIPrefs(); !got.GlobalNewTaskHot {
		t.Fatalf("loaded GlobalNewTaskHot = false, want true")
	}
	if err := saveUIPrefs(uiPrefs{GlobalNewTaskHot: false}); err != nil {
		t.Fatalf("save off: %v", err)
	}
	if got := loadUIPrefs(); got.GlobalNewTaskHot {
		t.Fatalf("loaded GlobalNewTaskHot = true after save off")
	}
	// The saved file must carry the key even for false, so a later launch
	// distinguishes "user turned it off" from "never set".
	path := filepath.Join(dir, uiPrefsFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read prefs after save off: %v", err)
	}
	if !bytes.Contains(raw, []byte("globalNewTaskHot")) {
		t.Fatalf("saved prefs omit globalNewTaskHot even when false; raw: %s", raw)
	}
}
