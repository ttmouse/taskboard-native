package main

import (
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
	_ = os.Setenv("TASKBOARD_DB", "")
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

	if err := saveUIPrefs(uiPrefs{TitleOnlyCards: true}); err != nil {
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

	if err := saveUIPrefs(uiPrefs{TitleOnlyCards: false}); err != nil {
		t.Fatalf("save off: %v", err)
	}
	got = loadUIPrefs()
	if got.TitleOnlyCards {
		t.Fatalf("loaded TitleOnlyCards = true after save off")
	}
}
