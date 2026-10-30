package model

import "testing"

// A released record is the convergence result for a session confirmed
// unreachable: it must stop holding the write lock, otherwise the card can be
// neither dispatched nor followed up again. A disconnected one still holds it
// so a retry cannot start a second writer.
func TestExecutionActiveStatuses(t *testing.T) {
	for _, tc := range []struct {
		status string
		active bool
	}{
		{"submitting", true},
		{"running", true},
		{"waiting_approval", true},
		{"unknown", true},
		{"disconnected", true},
		{"succeeded", false},
		{"returned", false},
		{"failed", false},
		{"released", false},
		// detached is the desktop "tracking stopped, session still alive" state:
		// it stays resumable, so it must keep the lock.
		{"detached", true},
	} {
		if got := (Execution{Status: tc.status}).Active(); got != tc.active {
			t.Fatalf("Execution{%s}.Active() = %v, want %v", tc.status, got, tc.active)
		}
	}
}

// Releasing is only safe once nothing may still be writing: a live session has
// to be stopped in its own channel first.
func TestExecutionReleasableStatuses(t *testing.T) {
	for _, tc := range []struct {
		status     string
		releasable bool
	}{
		{"submitting", false},
		{"running", false},
		{"waiting_approval", false},
		{"unknown", true},
		{"disconnected", true},
		{"returned", true},
		{"failed", true},
		{"released", true},
		{"detached", true},
	} {
		if got := (Execution{Status: tc.status}).Releasable(); got != tc.releasable {
			t.Fatalf("Execution{%s}.Releasable() = %v, want %v", tc.status, got, tc.releasable)
		}
	}
}

// A detached record only means "resumable" for the desktop channel, which never
// uses Herdr sessions. A Herdr record left detached by an older build has to
// count as released, otherwise it would lock the card for good.
func TestDetachedHerdrSessionCountsAsReleased(t *testing.T) {
	run := Execution{Status: "detached", SessionID: "herdr:t5m"}
	if run.Active() {
		t.Fatal("detached Herdr session still holds the write lock")
	}
	desktop := Execution{Status: "detached", SessionID: "aa_desktop_session", Runtime: "dsh"}
	if !desktop.Active() {
		t.Fatal("detached desktop session lost its resumable lock")
	}
}
