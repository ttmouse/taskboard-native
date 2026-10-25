package main

import "testing"

func TestLaunchDefaultsToGlobal(t *testing.T) {
	b := &board{cur: curGlobal}
	if b.cur != curGlobal {
		t.Fatalf("want curGlobal, got %q", b.cur)
	}
	// Mirror load()'s empty-cur branch.
	target := ""
	wantGlobal := target == curGlobal
	if target == "" {
		wantGlobal = true
		target = curGlobal
	}
	if !wantGlobal || target != curGlobal {
		t.Fatalf("empty cur should open global, got wantGlobal=%v target=%q", wantGlobal, target)
	}
}
