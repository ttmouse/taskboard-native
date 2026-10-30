package main

import (
	"sync"
	"testing"
)

// TestDisplayNameFallsBackToGUI pins the resolution rule: the window writes
// as the saved display name, and as gui when none is set. A field the user
// cleared holds spaces or nothing, which must not become the identity.
func TestDisplayNameFallsBackToGUI(t *testing.T) {
	cases := []struct{ stored, want string }{
		{"", defaultAgent},
		{"   ", defaultAgent},
		{"\t\n ", defaultAgent},
		{"豆爸", "豆爸"},
		{" 豆爸 ", "豆爸"},
	}
	for _, tc := range cases {
		if got := (uiPrefs{DisplayName: tc.stored}).windowAgent(); got != tc.want {
			t.Errorf("DisplayName %q → agent %q, want %q", tc.stored, got, tc.want)
		}
	}
}

// TestDisplayNamePrefsRoundTrip pins that 显示名 is a saved preference
// beside appearance, so the identity survives a restart, and that a
// preferences file written before the field existed still resolves to gui.
func TestDisplayNamePrefsRoundTrip(t *testing.T) {
	isolatePrefs(t)

	if err := saveUIPrefs(uiPrefs{DisplayName: "豆爸"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := loadUIPrefs().windowAgent(); got != "豆爸" {
		t.Fatalf("reloaded agent = %q, want 豆爸", got)
	}

	if err := saveUIPrefs(uiPrefs{}); err != nil {
		t.Fatalf("save empty: %v", err)
	}
	if got := loadUIPrefs().windowAgent(); got != defaultAgent {
		t.Fatalf("agent from prefs without a display name = %q, want %q", got, defaultAgent)
	}
}

// agentRecordingBoard is the fake board plus the one method a live client
// adds: SetAgent, which the 显示名 field calls on every edit.
type agentRecordingBoard struct {
	*fakeBoard
	mu  sync.Mutex
	got string
}

func (a *agentRecordingBoard) SetAgent(agent string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.got = agent
}

func (a *agentRecordingBoard) agent() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.got
}

// TestSettingsDisplayNameIsTheWriteIdentity drives the row the way a user
// does: open 设置, type a name, and the board and its client both stop
// writing as gui. The client is what puts the name on the wire.
func TestSettingsDisplayNameIsTheWriteIdentity(t *testing.T) {
	isolatePrefs(t)
	api := &agentRecordingBoard{fakeBoard: seedBoard()}
	b := newBoard(api)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	if b.agent != defaultAgent {
		t.Fatalf("fresh board writes as %q, want %q", b.agent, defaultAgent)
	}

	b.openSettings()
	waitFor(t, tt, "设置弹层", func() bool { return tt.HasText("显示名") })
	if err := tt.Click("显示身份名"); err != nil {
		t.Fatalf("click 显示名 字段: %v", err)
	}
	tt.Type("豆爸")
	tt.Frame()

	if b.agent != "豆爸" {
		t.Fatalf("board writes as %q, want 豆爸", b.agent)
	}
	if got := api.agent(); got != "豆爸" {
		t.Fatalf("client was handed %q, want 豆爸", got)
	}
	if got := loadUIPrefs().DisplayName; got != "豆爸" {
		t.Fatalf("saved display name = %q, want 豆爸", got)
	}
}

// TestRenderSettingsDisplayName renders the settings sheet carrying 显示名,
// so the new row has a frame to check by eye and the saved name is what the
// field shows. The frame lands in docs/assets/render/ with the others.
func TestRenderSettingsDisplayName(t *testing.T) {
	isolatePrefs(t)
	if err := saveUIPrefs(uiPrefs{DisplayName: "豆爸"}); err != nil {
		t.Fatal(err)
	}
	b := newBoard(seedBoard())
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	if b.prefs.DisplayName != "豆爸" {
		t.Fatalf("loaded display name = %q, want 豆爸", b.prefs.DisplayName)
	}

	b.openSettings()
	waitFor(t, tt, "设置弹层", func() bool { return tt.HasText("显示名") })
	tt.Frame()
	t.Logf("saved %s", save(t, tt, "settings-display-name.png"))
}
