//go:build motionplayground

package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// testReplyPage renders the static reply sample in a headless frame, so the
// page the Playground shows can be checked without a display: the same view
// function draws both.
func testReplyPage(t *testing.T, name string, width, height int) (*board, *ui.Tester, string) {
	t.Helper()
	isolatePrefs(t)
	b := newBoard(seedBoard())
	b.replyDemo = true
	tt := ui.NewTester(b.view, width, height)
	tt.Frame()
	return b, tt, saveFrame(t, tt, name, width, height)
}

// TestReplySamplePageShowsQuotedParents covers the card's request: a reply
// must show which comment it answers, so each reply renders its parent's
// author and text above its own body, indented from the parent.
func TestReplySamplePageShowsQuotedParents(t *testing.T) {
	_, tt, path := testReplyPage(t, "reply-page.png", 1280, 1500)
	t.Logf("sample: %s", path)

	if !tt.HasText("任务详情 · 评论回复") {
		t.Fatalf("page header missing; texts=%v", tt.Texts())
	}
	// Every reply quotes its parent; the parent that no longer exists keeps the
	// block so the reply does not shift.
	for _, want := range []string{"回复给 豆爸", "回复给 Claude Code", "原评论已删除"} {
		if !tt.HasText(want) {
			t.Fatalf("quote %q missing", want)
		}
	}

	root, ok := tt.Find("任务详情里面的回复，现在的结构看不出来回复的是哪一个内容，所以需要更真实地还原一下。")
	if !ok {
		t.Fatal("root comment body missing")
	}
	reply, ok := tt.Find("改成引用了：回复块把被回复的那条评论原文引出来，作者用强调色，最多两行、超出用省略号。回复按时间正序排在父评论下面，左侧一条竖线表示归属。")
	if !ok {
		t.Fatal("reply body missing")
	}
	if reply.X <= root.X || reply.Y <= root.Y {
		t.Fatalf("reply does not sit indented below its parent: root=%+v reply=%+v", root, reply)
	}
	quote, ok := tt.Find("回复给 豆爸")
	if !ok {
		t.Fatal("quote block missing")
	}

	// The rail is the only thing left of a reply card, so a long vertical run
	// there proves the thread connector did not collapse to zero height.
	if run := longestRunLeftOf(tt, quote); run < 120 {
		t.Fatalf("thread rail is not drawn: longest run %dpx left of the reply", run)
	}
}

// TestReplySamplePageStopsNestingAtProductionDepth keeps the sample honest:
// the real detail pane stops adding rails past two levels, so the page must
// show deep replies at their ancestor's indent rather than inventing extra
// nesting the board would never draw.
func TestReplySamplePageStopsNestingAtProductionDepth(t *testing.T) {
	_, tt, _ := testReplyPage(t, "reply-page.png", 1280, 1500)
	at := func(text string) ui.Rect {
		t.Helper()
		r, ok := tt.Find(text)
		if !ok {
			t.Fatalf("%q missing", text)
		}
		return r
	}
	level2 := at("引用区里如果被引用的评论本身很长呢？")
	level3 := at("引用最多两行，超出省略，鼠标悬停看全文；被引用的评论被删掉时显示「原评论已删除」，回复块的位置不会跳。")
	level4 := at("这条引用的是已经不存在的评论，用来检查兜底样式。")
	sibling := at("好，这样能看出回复的是哪一条了。")
	for _, r := range []ui.Rect{level3, level4, sibling} {
		if r.X != level2.X {
			t.Fatalf("nesting did not flatten at depth 2: level2.X=%.2f deep.X=%.2f", level2.X, r.X)
		}
	}
	if level2.X <= at("改成引用了：回复块把被回复的那条评论原文引出来，作者用强调色，最多两行、超出用省略号。回复按时间正序排在父评论下面，左侧一条竖线表示归属。").X {
		t.Fatal("level 2 is not indented from level 1")
	}
}

// TestPlaygroundShowsItsOwnVersion keeps the on-screen build number aligned
// with the bundle Finder reports: the Playground ships separately from the
// board, so it cannot inherit the production version.
func TestPlaygroundShowsItsOwnVersion(t *testing.T) {
	isolatePrefs(t)
	b := newBoard(seedBoard())
	tt := ui.NewTester(b.view, renderW, renderH)
	tt.Frame()
	if !tt.HasText(playgroundVersion) {
		t.Fatalf("top bar does not show %q", playgroundVersion)
	}
}

// TestReplySamplePageFitsSmallestWindow keeps the sample readable at the
// Playground's minimum width instead of relying on a wide screenshot.
func TestReplySamplePageFitsSmallestWindow(t *testing.T) {
	_, tt, _ := testReplyPage(t, "reply-page-narrow.png", 1060, 700)
	for _, want := range []string{"任务详情 · 评论回复", "返回看板", "评论"} {
		r, ok := tt.Find(want)
		if !ok {
			t.Fatalf("%q missing at the minimum window", want)
		}
		if r.X < 0 || r.X+r.W > 1060 || r.Y < 0 || r.Y+r.H > 700 {
			t.Fatalf("%q is outside the minimum window: %+v", want, r)
		}
	}
}

// longestRunLeftOf measures the tallest uninterrupted non-background column in
// the strip left of r, which is where the thread rail sits.
func longestRunLeftOf(tt *ui.Tester, r ui.Rect) int {
	img := tt.Image()
	b := img.Bounds()
	bg := img.RGBAAt(b.Min.X+2, b.Min.Y+2)
	best := 0
	for x := b.Min.X; x < int(r.X)-1; x++ {
		run := 0
		for y := b.Min.Y; y < b.Max.Y; y++ {
			c := img.RGBAAt(x, y)
			if c.R != bg.R || c.G != bg.G || c.B != bg.B {
				run++
				if run > best {
					best = run
				}
				continue
			}
			run = 0
		}
	}
	return best
}
