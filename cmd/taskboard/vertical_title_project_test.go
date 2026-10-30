package main

import (
	"testing"
)

// TestVerticalTitleOnlyPutsProjectAfterTitle: in the stacked board's
// title-only mode, 全部项目 used to put the project name on a second line
// under the title, so every row was at least two lines tall. The name now
// follows the title's own words on the title's line, and a one-line title
// is the same height as the same card inside one project. Full cards still
// put the name under the title.
func TestVerticalTitleOnlyPutsProjectAfterTitle(t *testing.T) {
	isolatePrefs(t)
	if err := saveUIPrefs(uiPrefs{BoardVertical: true, TitleOnlyCards: true}); err != nil {
		t.Fatal(err)
	}
	b := newBoard(seedBoard())
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	// The title and the name are one paragraph now: the name sits after the
	// title's words, not on a line of its own.
	para, ok := tt.Find("把看板接上 SSE  local")
	if !ok {
		t.Fatalf("title and project name are not one paragraph; texts: %v", tt.Texts())
	}
	name, ok := tt.Find("所属项目 local")
	if !ok {
		t.Fatalf("project name missing; texts: %v", tt.Texts())
	}
	card, ok := tt.Find("打开任务：把看板接上 SSE")
	if !ok {
		t.Fatal("card missing")
	}
	if d := (name.Y + name.H/2) - (para.Y + para.H/2); d < -1.5 || d > 1.5 {
		t.Errorf("project midline %.1f vs title midline %.1f (Δ%.1f); want the same row", name.Y+name.H/2, para.Y+para.H/2, d)
	}
	// The name follows the title: it ends far from the card's right edge,
	// so it is not pushed to the end of the row.
	if room := card.X + card.W - (name.X + name.W); room < 100 {
		t.Errorf("project name ends %.1f from the card's right edge; it should follow the title, not the row's end", room)
	}
	if name.X < para.X {
		t.Errorf("project x %.1f starts before the title at %.1f", name.X, para.X)
	}

	globalH := b.bounds["t-1"].H
	if globalH <= 0 {
		t.Fatal("card t-1 has no bounds")
	}
	b.cur = "local"
	b.post(func() {})
	tt.Frame()
	settle(t, tt)
	if tt.HasText("所属项目 local") {
		t.Fatal("single-project view still shows a project name")
	}
	localH := b.bounds["t-1"].H
	if d := globalH - localH; d < -1 || d > 1 {
		t.Errorf("title-only row is %.1f in 全部项目 and %.1f in one project; the name should not add a line", globalH, localH)
	}

	// Full cards keep the name under the title. This is the mode the
	// inline layout must not leak into.
	b.cur = curGlobal
	b.prefs.TitleOnlyCards = false
	b.post(func() {})
	tt.Frame()
	settle(t, tt)
	title, ok := tt.Find("把看板接上 SSE")
	if !ok {
		t.Fatalf("full card title missing")
	}
	name, ok = tt.Find("所属项目 local")
	if !ok {
		t.Fatalf("full card project name missing")
	}
	if name.Y+1 < title.Y+title.H {
		t.Errorf("full card puts the project on the title row (project y %.1f, title ends %.1f); want it underneath", name.Y, title.Y+title.H)
	}
}
