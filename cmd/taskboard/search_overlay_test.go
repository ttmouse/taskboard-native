package main

import (
	"strings"
	"testing"

	"taskboard-native/internal/model"

	"github.com/egoist/mygo/ui"
)

// Render the search surface in both appearances and at the minimum window
// size. Its bounds must match the visible surface, without an outer frame.
func TestSearchOverlaySurface(t *testing.T) {
	for _, tc := range []struct {
		name string
		dark bool
		w, h int
	}{
		{"light", false, renderW, renderH},
		{"dark", true, renderW, renderH},
		{"minimum", false, 960, 560},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolatePrefs(t)
			b := newBoard(seedBoard())
			tt := ui.NewTester(b.view, tc.w, tc.h)
			tt.SetDark(tc.dark)
			waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
			settle(t, tt)
			b.searchOpen, b.searchQuery = true, "看板"
			tt.Frame()
			panel, ok := tt.Find("任务搜索面板")
			if !ok {
				t.Fatal("search surface missing")
			}
			if panel.W > 560 || panel.X < 0 || panel.Y < 0 || panel.X+panel.W > float32(tc.w) || panel.Y+panel.H > float32(tc.h) {
				t.Fatalf("search surface outside intended bounds: %+v", panel)
			}
			// Sample beside the input, where Modal's default padding used
			// to introduce a second, white surface around the inner panel.
			pixel := tt.Image().RGBAAt(int(panel.X+3), int(panel.Y+panel.H/2))
			face := b.curTheme.Surface
			if pixel.R != face.R || pixel.G != face.G || pixel.B != face.B {
				t.Errorf("search edge is %v, want surface %v", pixel, face)
			}
			if !tt.HasText("打开任务 把看板接上 SSE") {
				t.Fatal("matching task missing")
			}
			saveFrame(t, tt, "search-"+tc.name+".png", tc.w, tc.h)
			for _, state := range []struct{ name, query, message string }{
				{"empty", "", "输入标题或描述中的关键词"},
				{"no-results", "不存在的关键词", "没有匹配的任务"},
			} {
				b.searchQuery = state.query
				tt.Frame()
				if !tt.HasText(state.message) {
					t.Fatalf("missing search message %q", state.message)
				}
				saveFrame(t, tt, "search-"+state.name+"-"+tc.name+".png", tc.w, tc.h)
			}
			b.task("t-1").Title = strings.Repeat("看板搜索结果中的长标题应在面板内省略显示", 5)
			b.searchQuery = "看板"
			tt.Frame()
			saveFrame(t, tt, "search-long-title-"+tc.name+".png", tc.w, tc.h)
			if err := tt.Click("关闭搜索"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if b.searchOpen || b.searchQuery != "" {
				t.Fatal("close button did not dismiss and clear search")
			}
			b.searchOpen, b.searchQuery = true, "看板"
			tt.Frame()
			tt.Key(0, ui.KeyEscape)
			tt.Frame()
			if b.searchOpen || b.searchQuery != "" {
				t.Fatal("Escape did not dismiss and clear search")
			}
		})
	}
}

func TestSearchKeyboardSelectionAndOpen(t *testing.T) {
	isolatePrefs(t)
	fake := seedBoard()
	fake.add(model.Task{
		ID: "t-6", ProjectID: "local", ColumnID: "c-review",
		Title: "看板搜索另一条结果", Position: 3000,
	})
	b := newBoard(fake)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
	settle(t, tt)
	b.searchOpen, b.searchQuery = true, "看板"
	tt.Frame()

	if b.searchSelection != -1 {
		t.Fatalf("initial search selection = %d, want none", b.searchSelection)
	}
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if b.searchSelection != 0 {
		t.Fatalf("first Down selected %d, want 0", b.searchSelection)
	}
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if b.searchSelection != 1 {
		t.Fatalf("second Down selected %d, want 1", b.searchSelection)
	}
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if b.searchSelection != 0 {
		t.Fatalf("Down at end selected %d, want wrapped index 0", b.searchSelection)
	}
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if b.searchSelection != 1 {
		t.Fatalf("Up at start selected %d, want wrapped index 1", b.searchSelection)
	}
	tt.Key(0, ui.KeyEnter)
	waitFor(t, tt, "打开选中的搜索结果", func() bool {
		return b.detail != nil && b.detail.id == "t-6" && !b.searchOpen
	})
	if b.searchQuery != "" {
		t.Fatalf("search query after opening a result = %q, want cleared", b.searchQuery)
	}
}
