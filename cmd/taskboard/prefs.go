package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"taskboard-native/internal/service"
)

// uiPrefs is the small set of window preferences persisted next to
// taskboard.db under Application Support/taskboard/.
type uiPrefs struct {
	TitleOnlyCards bool    `json:"titleOnlyCards"`
	BoardVertical  bool    `json:"boardVertical,omitempty"` // true → columns stacked as rows
	SidebarWidth     float32 `json:"sidebarWidth,omitempty"` // DIP; 0 → theme default
	SidebarCollapsed bool    `json:"sidebarCollapsed,omitempty"` // true → narrow reopen rail
}

const uiPrefsFile = "ui-prefs.json"

// prefsPath resolves ~/Library/Application Support/taskboard/ui-prefs.json
// (same directory as taskboard.db).
func prefsPath() (string, error) {
	db, err := service.DefaultDBPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(db), uiPrefsFile), nil
}

// loadUIPrefs reads ui-prefs.json. Missing or unreadable files yield defaults.
func loadUIPrefs() uiPrefs {
	path, err := prefsPath()
	if err != nil {
		return uiPrefs{}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return uiPrefs{}
	}
	var p uiPrefs
	if json.Unmarshal(raw, &p) != nil {
		return uiPrefs{}
	}
	return p
}

// saveUIPrefs writes ui-prefs.json atomically when possible.
func saveUIPrefs(p uiPrefs) error {
	path, err := prefsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
