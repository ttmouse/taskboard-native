package main

import (
	"testing"
)

// The create-task pill carries the two modes: Enter creates only, ⌘Enter
// creates and hands the card to the channel the picker names. The frame
// lands in docs/assets/render/ so the channel picker is visible in the
// shipped evidence.
func TestComposeRendersChannelPicker(t *testing.T) {
	isolatePrefs(t)
	fake := seedBoard()
	fake.projects[0].WorkspacePath = t.TempDir()
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	b.openTaskFormIn("local", "c-todo")
	tt.Frame()
	if b.form == nil || !b.form.open {
		t.Fatal("compose did not open")
	}
	// The picker shows the channel ⌘Enter would use.
	if !tt.HasText("DSH") {
		t.Fatalf("channel picker missing; texts: %v", tt.Texts())
	}
	save(t, tt, "compose-channel-picker.png")
}
