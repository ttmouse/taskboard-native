package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"taskboard-native/internal/model"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// TestSidebarCollapseHidesRail checks that collapsing the project sidebar
// takes the whole rail away — no narrow reopen strip left behind — that the
// board then spans the window, and that the top bar's panel chip brings it
// back.
func TestSidebarCollapseHidesRail(t *testing.T) {
	dir := t.TempDir()
	// Prefs live beside TASKBOARD_DB; point both inside this test's dir so a
	// sidebar toggle cannot write the developer's real ui-prefs.json.
	t.Setenv("TASKBOARD_DB", filepath.Join(dir, "tb.db"))
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })

	fake := seedBoard()
	fake.projects = append(fake.projects, model.Project{ID: "p-2", Name: "工作",
		Columns: []model.Column{{ID: "c2-todo", Name: "待办"}}, CreatedAt: fake.now})
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	if !b.sidebarVisible() {
		t.Fatal("sidebar starts hidden")
	}
	wide := columnLeft(t, tt)
	if wide <= 0 {
		t.Fatalf("first column sits at x=%v with the sidebar shown", wide)
	}

	if err := tt.Click("收起侧栏"); err != nil {
		t.Fatalf("click 收起侧栏: %v", err)
	}
	tt.Frame()
	settle(t, tt)
	if b.sidebarVisible() || !b.prefs.SidebarCollapsed {
		t.Fatal("collapse did not take effect")
	}
	// The rail is gone: no project list, no old 侧栏已收起 strip.
	for _, gone := range []string{"项目列表", "侧栏已收起", "切换到项目 工作"} {
		if tt.HasText(gone) {
			t.Errorf("collapsed frame still shows %q; texts: %v", gone, tt.Texts())
		}
	}
	// Its actions survive in the content bar, next to the reopen control.
	for _, want := range []string{"展开侧栏", "新建任务", "搜索任务", "打开设置"} {
		if !tt.HasText(want) {
			t.Errorf("collapsed frame lacks %q; texts: %v", want, tt.Texts())
		}
	}
	// No 48 DIP gap: the board reclaims the rail's width.
	if narrow := columnLeft(t, tt); narrow >= wide {
		t.Errorf("first column still at x=%v after collapse (was %v)", narrow, wide)
	}

	if err := tt.Click("展开侧栏"); err != nil {
		t.Fatalf("click 展开侧栏: %v", err)
	}
	tt.Frame()
	settle(t, tt)
	if !b.sidebarVisible() || b.prefs.SidebarCollapsed {
		t.Fatal("expand did not restore the sidebar")
	}
	if !tt.HasText("切换到项目 工作") {
		t.Errorf("project list is back but empty; texts: %v", tt.Texts())
	}
}

// TestSidebarToggleShortcut checks ⌘B toggles the same preference.
func TestSidebarToggleShortcut(t *testing.T) {
	dir := t.TempDir()
	// Prefs live beside TASKBOARD_DB; point both inside this test's dir so a
	// sidebar toggle cannot write the developer's real ui-prefs.json.
	t.Setenv("TASKBOARD_DB", filepath.Join(dir, "tb.db"))
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })

	b := newBoard(seedBoard())
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	tt.Key(ui.Cmd, ui.KeyB)
	tt.Frame()
	settle(t, tt)
	if !b.prefs.SidebarCollapsed {
		t.Fatal("⌘B did not collapse the sidebar")
	}
	tt.Key(ui.Cmd, ui.KeyB)
	tt.Frame()
	settle(t, tt)
	if b.prefs.SidebarCollapsed {
		t.Fatal("⌘B did not restore the sidebar")
	}
}

func TestSidebarToggleKeyDownLatency(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TASKBOARD_DB", filepath.Join(dir, "tb.db"))
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })

	b := newBoard(seedBoard())
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	// MyGo OnShortcut runs on KeyPressed. Tester.Key sends press then release;
	// settle after press already flips the pref before release is delivered.
	tt.Key(ui.Cmd, ui.KeyB)
	if !b.prefs.SidebarCollapsed {
		t.Fatal("⌘B key-down did not collapse the sidebar")
	}
	for i := 0; i < 4 && b.sidebarPerfFirstChange == 0; i++ {
		tt.Advance(time.Millisecond)
		tt.Frame()
	}
	if b.sidebarPerfFirstChange <= 0 || b.sidebarPerfFirstChange >= 50*time.Millisecond {
		t.Fatalf("key_down first rail change = %v, want (0, 50ms)", b.sidebarPerfFirstChange)
	}
	t.Logf("key_down first rail change: %.3fms", float64(b.sidebarPerfFirstChange)/float64(time.Millisecond))
	settle(t, tt)
}

// TestSidebarToggleAnimatesBeforeKeyRelease is the regression for 0.1.94:
// the preference flipped on key-down, but the drawer only moved after B
// released because railWidth never called AnimationFrame. Prove the rail
// slides with no KeyUp / KeyReleased at all.
func TestSidebarToggleAnimatesBeforeKeyRelease(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TASKBOARD_DB", filepath.Join(dir, "tb.db"))
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })

	b := newBoard(seedBoard())
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	startW := b.railNow
	if startW <= 0 {
		t.Fatalf("rail resting width = %v, want open sidebar", startW)
	}
	// Same path Cmd+B OnShortcut takes on KeyPressed (before any KeyReleased).
	b.toggleSidebarForCase("key_down")
	if !b.prefs.SidebarCollapsed {
		t.Fatal("key-down toggle did not collapse the sidebar")
	}
	tt.Frame()
	moved := false
	for i := 0; i < 12; i++ {
		tt.Advance(8 * time.Millisecond)
		tt.Frame()
		if b.railNow < startW-0.5 {
			moved = true
			break
		}
	}
	if !moved {
		t.Fatalf("rail did not slide without key-release: start=%.2f now=%.2f", startW, b.railNow)
	}
	if b.sidebarPerfFirstChange <= 0 || b.sidebarPerfFirstChange >= 50*time.Millisecond {
		t.Fatalf("first rail change on key-down path = %v, want (0, 50ms)", b.sidebarPerfFirstChange)
	}
	t.Logf("key_down_before_release first rail change: %.3fms rail=%.2f→%.2f",
		float64(b.sidebarPerfFirstChange)/float64(time.Millisecond), startW, b.railNow)
	fmt.Printf("sidebar_perf case=key_down_before_release toggle_to_first_rail_change_ms=%.3f\n",
		float64(b.sidebarPerfFirstChange)/float64(time.Millisecond))
}

func TestSidebarToggleKeyReleaseNoRetrigger(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TASKBOARD_DB", filepath.Join(dir, "tb.db"))
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })

	b := newBoard(seedBoard())
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	tt.Key(ui.Cmd, ui.KeyB)
	tt.Frame()
	if !b.prefs.SidebarCollapsed {
		t.Fatal("⌘B key-down did not collapse the sidebar")
	}
	settle(t, tt)
	before := b.prefs.SidebarCollapsed
	// Exercise a held key and its release as distinct input events. The Cmd+B
	// shortcut was delivered above; this unmodified B press must not toggle it.
	tt.KeyDown(ui.KeyB)
	tt.Frame()
	tt.KeyUp(ui.KeyB)
	tt.Frame()
	if b.prefs.SidebarCollapsed != before {
		t.Fatal("KeyUp retriggered the sidebar toggle")
	}
	fmt.Printf("sidebar_perf case=key_release no_change=true toggle_to_first_rail_change_ms=0.000\n")
}


// TestSidebarToggleKeyRepeatDoesNotRetrigger: MyGo routes isARepeat
// KeyPressed into OnShortcut. Repeats inside the cooldown must not flip-flop.
func TestSidebarToggleKeyRepeatDoesNotRetrigger(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TASKBOARD_DB", filepath.Join(dir, "tb.db"))
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })

	b := newBoard(seedBoard())
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	tt.Key(ui.Cmd, ui.KeyB)
	tt.Frame()
	if !b.prefs.SidebarCollapsed {
		t.Fatal("first ⌘B did not collapse")
	}
	if !b.sidebarKeyGate {
		t.Fatal("key gate should arm after the first ⌘B")
	}

	// Simulate auto-repeat arriving before the cooldown elapses.
	// Use a real Key so allow runs on the live frame clock.
	tt.Advance(50 * time.Millisecond)
	tt.Key(ui.Cmd, ui.KeyB)
	tt.Frame()
	if !b.prefs.SidebarCollapsed {
		t.Fatal("auto-repeat flipped the sidebar open")
	}

	// After cooldown, a real second press works (gate may still be armed).
	tt.Advance(sidebarKeyCooldown + 20*time.Millisecond)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyB)
	tt.Frame()
	if b.prefs.SidebarCollapsed {
		t.Fatal("second ⌘B after cooldown did not restore the sidebar")
	}
	fmt.Printf("sidebar_perf case=key_repeat_guard suppressed=true restored_after_cooldown=true\n")
}

// TestSidebarToggleHoldPastCooldownRetriggers: a held chord whose repeats
// arrive farther apart than the cooldown must produce multiple toggles.
// Drive real Key events so ArmedAt is stamped from the live frame clock
// (frameCtx.Now() is zero outside a frame).
func TestSidebarToggleHoldPastCooldownRetriggers(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TASKBOARD_DB", filepath.Join(dir, "tb.db"))
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })

	b := newBoard(seedBoard())
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	wantCollapsed := []bool{true, false, true}
	for i, want := range wantCollapsed {
		if i > 0 {
			// Gap longer than cooldown, as if the OS delivered a slow repeat.
			tt.Advance(sidebarKeyCooldown + 50*time.Millisecond)
			tt.Frame()
		}
		tt.Key(ui.Cmd, ui.KeyB)
		tt.Frame()
		if b.prefs.SidebarCollapsed != want {
			t.Fatalf("step %d: SidebarCollapsed=%v, want %v", i+1, b.prefs.SidebarCollapsed, want)
		}
	}
	// A tight repeat still inside the window must stay suppressed.
	before := b.prefs.SidebarCollapsed
	tt.Advance(50 * time.Millisecond)
	tt.Key(ui.Cmd, ui.KeyB)
	tt.Frame()
	if b.prefs.SidebarCollapsed != before {
		t.Fatal("tight repeat flipped the sidebar")
	}
	fmt.Printf("sidebar_perf case=key_hold_past_cooldown toggles=3 suppressed_tight=true\n")
}

func TestSidebarToggleIconClickLatency(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TASKBOARD_DB", filepath.Join(dir, "tb.db"))
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })

	b := newBoard(seedBoard())
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	if err := tt.Click("收起侧栏"); err != nil {
		t.Fatalf("click collapse chip: %v", err)
	}
	tt.Frame()
	if !b.prefs.SidebarCollapsed {
		t.Fatal("collapse chip did not collapse the sidebar")
	}
	for i := 0; i < 4 && b.sidebarPerfFirstChange == 0; i++ {
		tt.Advance(time.Millisecond)
		tt.Frame()
	}
	if b.sidebarPerfFirstChange <= 0 || b.sidebarPerfFirstChange >= 50*time.Millisecond {
		t.Fatalf("icon_click first rail change = %v, want (0, 50ms)", b.sidebarPerfFirstChange)
	}
	t.Logf("icon_click first rail change: %.3fms", float64(b.sidebarPerfFirstChange)/float64(time.Millisecond))
}

func TestSidebarTogglePersistsLatestPreferenceAsynchronously(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TASKBOARD_DB", filepath.Join(dir, "tb.db"))
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })

	b := newBoard(seedBoard())
	b.toggleSidebar()
	b.toggleSidebar()
	want, err := prefsPath()
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		raw, readErr := os.ReadFile(want)
		if readErr == nil {
			var got uiPrefs
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if !got.SidebarCollapsed {
				return // two immediate toggles settle back to expanded
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("latest sidebar preference was not persisted")
}

func TestSidebarAnimationReusesColumnTaskIndex(t *testing.T) {
	fake := seedBoard()
	b := newBoard(fake)
	b.tasks = append([]model.Task(nil), fake.tasks...)
	first := b.tasksIn("c-todo")
	if len(first) == 0 {
		t.Fatal("seed board has no todo task")
	}
	for i := 0; i < 12; i++ { // equivalent repeated lookups across rail frames
		again := b.tasksIn("c-todo")
		if len(again) == 0 || &again[0] != &first[0] {
			t.Fatalf("frame %d rebuilt the per-column task slice", i)
		}
	}

	updated := first[0]
	updated.Position += 100
	if applied, _ := b.upsert(updated); !applied {
		t.Fatal("task update was not applied")
	}
	after := b.tasksIn("c-todo")
	found := false
	for _, task := range after {
		if task.ID == updated.ID {
			found = true
			if task.Position != updated.Position {
				t.Fatalf("cached position = %v, want %v", task.Position, updated.Position)
			}
		}
	}
	if !found {
		t.Fatalf("updated task %q disappeared from its column", updated.ID)
	}
}

func TestSidebarResizeKeepsRailAndBoardInSync(t *testing.T) {
	isolatePrefs(t)
	b := newBoard(seedBoard())
	defer b.stop()
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	before := b.sidebarW
	handle, ok := tt.Find("调整侧栏宽度")
	if !ok {
		t.Fatal("sidebar resize handle is missing")
	}
	x, y := handle.X+handle.W/2, handle.Y+handle.H/2
	tt.Press(x, y)
	tt.Move(x+24, y)
	tt.Frame()
	if b.sidebarW <= before {
		t.Fatalf("drag did not widen the sidebar: before %.1f, after %.1f", before, b.sidebarW)
	}
	for i := 0; i < 4; i++ {
		tt.Move(x+24+float32((i+1)*12), y)
		tt.Frame()
		if absDiff(b.railNow, b.sidebarW) > 0.5 {
			t.Errorf("drag frame %d: rail %.1f lagged width %.1f", i, b.railNow, b.sidebarW)
		}
		if absDiff(b.paneW+b.railNow, renderW) > 0.5 {
			t.Errorf("drag frame %d: pane %.1f plus rail %.1f does not fill %d", i, b.paneW, b.railNow, renderW)
		}
	}
	width := b.sidebarW
	tt.Release(x+72, y)
	tt.Frame()
	if b.prefs.SidebarWidth != width {
		t.Errorf("released width was not persisted: prefs %.1f, live %.1f", b.prefs.SidebarWidth, width)
	}
}

// columnLeft is the x of the first column panel, found through its cards'
// accessible names; 0 when none is on screen.
func columnLeft(t *testing.T, tt *ui.Tester) float32 {
	t.Helper()
	r, ok := tt.Find("打开任务：把看板接上 SSE")
	if !ok {
		t.Fatalf("no card to measure; texts: %v", tt.Texts())
	}
	return r.X
}

// TestSidebarToggleKeepsCardsWhole is the regression for the clipped cards:
// opening or collapsing the sidebar re-flows the board, and while the width
// jumped, the columns' and cards' own Position+Size transitions carried them
// from their old place — outside the pane, so their left side showed cut
// off. Every frame of the toggle must keep a card's text inside the card and
// the card inside its column.
func TestSidebarToggleKeepsCardsWhole(t *testing.T) {
	for _, dark := range []bool{false, true} {
		dir := t.TempDir()
		t.Setenv("TASKBOARD_DB", filepath.Join(dir, "tb.db"))
		mygo.App.SetPath(mygo.PathUserData, dir)
		t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })

		b := newBoard(seedBoard())
		tt := newBoardTester(b)
		tt.SetDark(dark)
		waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
		settle(t, tt)

		for _, toggle := range []string{"收起侧栏", "展开侧栏"} {
			if err := tt.Click(toggle); err != nil {
				t.Fatalf("%s: %v", toggle, err)
			}
			tt.Frame()
			// Walk the whole transition, including its first frames, where
			// a tween is most visible.
			for i, d := range []time.Duration{0, 6 * time.Millisecond, 6 * time.Millisecond,
				12 * time.Millisecond, 12 * time.Millisecond, 24 * time.Millisecond, 120 * time.Millisecond} {
				tt.Advance(d)
				tt.Frame()

				col, okCol := tt.Find("待办列")
				card, okCard := tt.Find("打开任务：把看板接上 SSE")
				text, okText := tt.Find("把看板接上 SSE")
				if !okCol || !okCard || !okText {
					t.Fatalf("%s[%d] dark=%v: board missing after the toggle; texts: %v",
						toggle, i, dark, tt.Texts())
				}
				// The card's own padding is Space(3.5) = 14 DIP; text starting
				// left of it is text drawn outside the card's box, i.e. cropped.
				if inset := text.X - card.X; inset < 13.5 {
					t.Errorf("%s[%d] dark=%v: text starts %.1f into the card, want ~14 — clipped at the left",
						toggle, i, dark, inset)
				}
				if card.X < col.X-0.5 {
					t.Errorf("%s[%d] dark=%v: card x=%.1f sits left of its column x=%.1f",
						toggle, i, dark, card.X, col.X)
				}
			}
		}
		b.stop()
	}
}

// TestSidebarToggleAnimatesCrisply checks the rail slides rather than
// snapping: it finishes in a crisp 80-260 ms, only ever narrows while
// collapsing, and the board pane plus the rail always add up to the window.
func TestSidebarToggleAnimatesCrisply(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TASKBOARD_DB", filepath.Join(dir, "tb.db"))
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })

	b := newBoard(seedBoard())
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	full := b.sidebarWidth(ui.LightTheme())
	if full <= 0 {
		t.Fatalf("resting rail width is %v", full)
	}

	tt.Key(ui.Cmd, ui.KeyB)
	tt.Frame()
	var widths []float32
	for i := 0; i < 40; i++ {
		widths = append(widths, b.railNow)
		if absDiff(b.paneW+b.railNow, renderW) > 0.5 {
			t.Fatalf("frame %d: pane %.1f + rail %.1f != %d", i, b.paneW, b.railNow, renderW)
		}
		if b.railNow < 0.5 {
			break
		}
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
	}
	if b.sidebarPerfFirstChange <= 0 || b.sidebarPerfFirstChange >= 50*time.Millisecond {
		t.Errorf("toggle to first rail width change = %v, want (0, 50ms); animation is %v",
			b.sidebarPerfFirstChange, railCollapseFor)
	}
	t.Logf("toggle to first rail width change: %v; collapse animation: %v",
		b.sidebarPerfFirstChange, railCollapseFor)

	if last := widths[len(widths)-1]; last >= 0.5 {
		t.Fatalf("rail never finished collapsing; widths %v", widths)
	}
	// A crisp slide is a handful of frames, not a slow crawl.
	if n := len(widths); n < 3 || n > 20 {
		t.Errorf("collapse took %d frames of 16ms, want 3-20 (a quick snap, not one frame)", n)
	}
	for i := 1; i < len(widths); i++ {
		if widths[i] > widths[i-1]+0.001 {
			t.Fatalf("rail widened mid-collapse: %v", widths)
		}
	}
	if widths[0] < full-0.5 {
		t.Errorf("first frame of the collapse already dropped the rail to %.1f of %.1f — it should start where it was", widths[0], full)
	}
}
