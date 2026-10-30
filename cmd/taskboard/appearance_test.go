package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// recordAppearance swaps mygo's appearance for a recorder, so a test can
// see what the board asked the app for without a running app (mygo's
// Theme needs one, and blocks without it). It reports the last source.
func recordAppearance(t *testing.T) *[]mygo.ThemeSource {
	t.Helper()
	asked := &[]mygo.ThemeSource{}
	prev := askAppearance
	askAppearance = func(source mygo.ThemeSource) { *asked = append(*asked, source) }
	t.Cleanup(func() { askAppearance = prev })
	return asked
}

// TestAppearancePrefsRoundTrip pins the saved 外观: the light appearance
// written by the settings sheet has to come back as light, not as "follow
// the desktop", which is what a missing or unknown value means.
func TestAppearancePrefsRoundTrip(t *testing.T) {
	for _, want := range []appearance{appearSystem, appearLight, appearDark} {
		dir := t.TempDir()
		mygo.App.SetPath(mygo.PathUserData, dir)
		t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })
		t.Setenv("TASKBOARD_DB", "")

		if err := saveUIPrefs(uiPrefs{Appearance: want}); err != nil {
			t.Fatalf("save %s: %v", want, err)
		}
		path := filepath.Join(dir, uiPrefsFile)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("prefs file missing at %s: %v", path, err)
		}
		if got := loadUIPrefs().Appearance; got != want {
			t.Fatalf("loaded appearance = %q, want %q", got, want)
		}
	}
}

// TestAppearanceDefaultsToSystem checks a preferences file without an
// appearance means "follow the desktop", including one written by a newer
// build with a value this build does not know.
func TestAppearanceDefaultsToSystem(t *testing.T) {
	dir := t.TempDir()
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })
	t.Setenv("TASKBOARD_DB", "")

	if got := loadUIPrefs().Appearance; got != appearSystem {
		t.Fatalf("default appearance = %q, want %q", got, appearSystem)
	}
	if err := os.WriteFile(filepath.Join(dir, uiPrefsFile), []byte(`{"appearance":"sepia"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadUIPrefs().Appearance; got != appearSystem {
		t.Fatalf("unknown appearance loaded as %q, want %q", got, appearSystem)
	}
}

// TestAppearanceCycle pins the order ⌘⇧L walks, the labels the row uses,
// and that each choice maps onto the matching mygo source.
func TestAppearanceCycle(t *testing.T) {
	steps := []appearance{appearSystem, appearLight, appearDark, appearSystem}
	got := appearSystem
	for i := 1; i < len(steps); i++ {
		got = got.next()
		if got != steps[i] {
			t.Fatalf("step %d = %q, want %q", i, got, steps[i])
		}
	}
	sources := map[appearance]mygo.ThemeSource{
		appearSystem: mygo.ThemeSystem,
		appearLight:  mygo.ThemeLight,
		appearDark:   mygo.ThemeDark,
	}
	for _, choice := range appearanceChoices {
		if !choice.value.valid() {
			t.Errorf("%q is not valid", choice.value)
		}
		if choice.value.label() != choice.label {
			t.Errorf("%q labels as %q, want %q", choice.value, choice.value.label(), choice.label)
		}
		if choice.value.source() != sources[choice.value] {
			t.Errorf("%q asks for %q, want %q", choice.value, choice.value.source(), sources[choice.value])
		}
	}
	if !appearance("").valid() == false {
		t.Fatal("the zero appearance must not pass as valid")
	}
}

// TestSettingsRowAppearance checks the 外观 row in the settings sheet: all
// three choices are there, choosing 浅色 while the desktop is dark asks the
// app for the light appearance, and the choice is saved. Asking mygo is
// what turns the frame, the window chrome and the controls together, so
// this is the bug the setting exists for.
func TestSettingsRowAppearance(t *testing.T) {
	isolatePrefs(t)
	asked := recordAppearance(t)
	b := newBoard(seedBoard())
	tt := newBoardTester(b)
	tt.SetDark(true)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	b.openSettings()
	// Wait on the sheet's own control: 外观 also appears in a card's summary,
	// so matching that word alone would return before the sheet is drawn.
	waitFor(t, tt, "设置弹层", func() bool { return tt.HasText("跟随系统") })
	for _, want := range []string{"外观", "跟随系统", "浅色", "深色"} {
		if !tt.HasText(want) {
			t.Fatalf("settings sheet lacks %q; texts: %v", want, tt.Texts())
		}
	}
	if err := tt.Click("浅色"); err != nil {
		t.Fatalf("click 浅色: %v", err)
	}
	tt.Frame()
	if b.prefs.Appearance != appearLight {
		t.Fatalf("appearance = %q, want light", b.prefs.Appearance)
	}
	if len(*asked) == 0 || (*asked)[len(*asked)-1] != mygo.ThemeLight {
		t.Fatalf("asked the app for %v, want the last call to be %q", *asked, mygo.ThemeLight)
	}
	if got := loadUIPrefs().Appearance; got != appearLight {
		t.Fatalf("saved appearance = %q, want light", got)
	}
}

// TestCycleAppearanceShortcut pins ⌘⇧L against a dark desktop: the first
// press is 浅色, which asks for the light appearance and is saved; the
// second is 深色 under a light desktop, which asks for dark.
func TestCycleAppearanceShortcut(t *testing.T) {
	isolatePrefs(t)
	asked := recordAppearance(t)
	b := newBoard(seedBoard())
	tt := newBoardTester(b)
	tt.SetDark(true)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	tt.Key(ui.Cmd|ui.Shift, ui.KeyL)
	tt.Frame()
	if b.prefs.Appearance != appearLight {
		t.Fatalf("after ⌘⇧L: appearance %q, want light", b.prefs.Appearance)
	}
	if len(*asked) != 1 || (*asked)[0] != mygo.ThemeLight {
		t.Fatalf("asked the app for %v, want one call with %q", *asked, mygo.ThemeLight)
	}
	if got := loadUIPrefs().Appearance; got != appearLight {
		t.Fatalf("saved appearance = %q, want light", got)
	}

	tt.SetDark(false)
	tt.Frame()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyL)
	tt.Frame()
	if b.prefs.Appearance != appearDark {
		t.Fatalf("second ⌘⇧L: appearance %q, want dark", b.prefs.Appearance)
	}
	if last := (*asked)[len(*asked)-1]; last != mygo.ThemeDark {
		t.Fatalf("asked the app for %q, want %q", last, mygo.ThemeDark)
	}

	// A third press goes back to following the desktop, which must hand the
	// system back rather than pin one of the two.
	tt.Key(ui.Cmd|ui.Shift, ui.KeyL)
	tt.Frame()
	if b.prefs.Appearance != appearSystem {
		t.Fatalf("third ⌘⇧L: appearance %q, want system", b.prefs.Appearance)
	}
	if last := (*asked)[len(*asked)-1]; last != mygo.ThemeSystem {
		t.Fatalf("asked the app for %q, want %q", last, mygo.ThemeSystem)
	}
}

// TestBoardLetsWindowShowThrough checks the change the light appearance
// asked for: a column paints no block of its own, so the window face runs
// behind the board. Cards use their fill and hover state without outlines.
func TestBoardLetsWindowShowThrough(t *testing.T) {
	for _, t2 := range []*ui.Theme{ui.LightTheme(), ui.DarkTheme()} {
		if got := colFace(t2); got.A != 0 {
			t.Errorf("dark=%v column face alpha %d, want 0 (no block)", t2.Dark, got.A)
		}
		if cardHover(t2) == colFace(t2) {
			t.Errorf("dark=%v hover face equals the column face", t2.Dark)
		}
		if cardFace(t2) == colFace(t2) {
			t.Errorf("dark=%v resting card face equals the column face", t2.Dark)
		}
	}
}
