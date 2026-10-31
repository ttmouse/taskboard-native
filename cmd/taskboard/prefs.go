package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"taskboard-native/internal/service"
)

// defaultAgent is the identity the window writes under when no display
// name is set: the name this app has always used for its own writes.
const defaultAgent = "gui"

// appearance is the chrome's 外观: the light appearance, the dark one, or
// whatever the desktop is in right now. It is a saved preference, not a
// reading of the system: the light appearance has to survive a Mac that is
// in dark mode, which is the whole point of having the setting.
type appearance string

const (
	appearSystem appearance = "system" // follow the desktop
	appearLight  appearance = "light"  // the light appearance, always
	appearDark   appearance = "dark"   // the dark one, always
)

// appearanceChoices lists the choices in the order the settings sheet and
// the context menu show them, with the words the chrome says.
var appearanceChoices = []struct {
	value appearance
	label string
}{
	{appearSystem, "跟随系统"},
	{appearLight, "浅色"},
	{appearDark, "深色"},
}

// valid reports whether a is one of the known appearances; an empty value,
// or one written by a newer build, falls back to following the desktop.
func (a appearance) valid() bool {
	return a == appearSystem || a == appearLight || a == appearDark
}

// label is the appearance's name as the chrome says it.
func (a appearance) label() string {
	for _, c := range appearanceChoices {
		if c.value == a {
			return c.label
		}
	}
	return appearanceChoices[0].label
}

// source maps the appearance onto mygo's appearance override. Forcing light
// or dark asks the whole app for it, so the window chrome, traffic lights,
// menus and standard controls turn with the frame; following the desktop
// hands mygo back to the system.
func (a appearance) source() mygo.ThemeSource {
	switch a {
	case appearLight:
		return mygo.ThemeLight
	case appearDark:
		return mygo.ThemeDark
	default:
		return mygo.ThemeSystem
	}
}

// next is the appearance one step along 跟随系统 → 浅色 → 深色 → 跟随系统,
// for ⌘⇧L and the context menu of the 外观 row.
func (a appearance) next() appearance {
	switch a {
	case appearLight:
		return appearDark
	case appearDark:
		return appearSystem
	default:
		return appearLight
	}
}

// uiPrefs is the small set of window preferences persisted next to
// taskboard.db under Application Support/taskboard/.
type uiPrefs struct {
	TitleOnlyCards    bool            `json:"titleOnlyCards"`
	BoardVertical     bool            `json:"boardVertical,omitempty"`     // true → columns stacked as rows
	CollapsedCols     map[string]bool `json:"collapsedCols,omitempty"`     // folded status groups in the stacked board, by column ID
	SidebarWidth      float32         `json:"sidebarWidth,omitempty"`      // DIP; 0 → theme default
	SidebarCollapsed  bool            `json:"sidebarCollapsed,omitempty"`  // true → narrow reopen rail
	Appearance        appearance      `json:"appearance,omitempty"`        // "" → follow the desktop
	DisplayName       string          `json:"displayName,omitempty"`       // user's display name for agent identity; falls back to "gui"
	LastTaskProjectID string          `json:"lastTaskProjectId,omitempty"` // project preselected by the next global ⌘N
	LastTaskRuntime   string          `json:"lastTaskRuntime,omitempty"`   // execution channel preselected by the next task composer
	ProjectSort       string          `json:"projectSort,omitempty"`       // sidebar project order: default / updated / created
	GlobalNewTaskHot  bool            `json:"globalNewTaskHot"`            // ⌘⇧N works while the app is in the background
}

const uiPrefsFile = "ui-prefs.json"

// windowAgent is the identity the window signs its writes with: the saved
// display name, or defaultAgent when it is unset, blank, or only spaces.
// It is read at launch and again on every edit of the field, so the name
// reaches the service as X-Agent and comes back as a card's last writer.
func (p uiPrefs) windowAgent() string {
	if name := strings.TrimSpace(p.DisplayName); name != "" {
		return name
	}
	return defaultAgent
}

// prefsPath resolves ~/Library/Application Support/taskboard/ui-prefs.json
// (same directory as taskboard.db).
func prefsPath() (string, error) {
	db, err := service.DefaultDBPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(db), uiPrefsFile), nil
}

// loadUIPrefs reads ui-prefs.json. Missing or unreadable files yield
// defaults. The appearance is normalized on every path: the zero value is
// not a choice, and every caller reads b.prefs.Appearance directly. The
// ⌘⇧N global shortcut defaults to ON on a fresh install (no file at all):
// the user who never touched the setting expects it to just work.
func loadUIPrefs() uiPrefs {
	p, ok := readUIPrefs()
	if !ok {
		// Fresh install (no file): ⌘⇧N defaults to ON so the user who
		// never touched the setting gets the shortcut, and the system
		// appearance is the theme default.
		p = uiPrefs{GlobalNewTaskHot: true}
	}
	if !p.Appearance.valid() {
		p.Appearance = appearSystem
	}
	if p.ProjectSort != "" && p.ProjectSort != "updated" && p.ProjectSort != "created" {
		p.ProjectSort = ""
	}
	return p
}

// readUIPrefs reads and decodes ui-prefs.json, reporting whether it did.
func readUIPrefs() (uiPrefs, bool) {
	path, err := prefsPath()
	if err != nil {
		return uiPrefs{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return uiPrefs{}, false
	}
	// Decode the key set first: a prefs file written before the ⌘⇧N switch
	// existed (or a fresh install with no file at all) has no
	// globalNewTaskHot key. Treat that as the feature being ON — the
	// shortcut is the point of this app's global-new-task flow and the
	// user who never touched the setting expects it to just work. Once
	// the user toggles the switch off, saveUIPrefs writes the key (no
	// omitempty) and the next launch honors the explicit off.
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) != nil {
		return uiPrefs{}, false
	}
	var p uiPrefs
	if json.Unmarshal(raw, &p) != nil {
		return uiPrefs{}, false
	}
	if _, ok := keys["globalNewTaskHot"]; !ok {
		p.GlobalNewTaskHot = true
	}
	return p, true
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
