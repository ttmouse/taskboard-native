package main

import "testing"

func TestBoardDisplayControlsStayAtTopRight(t *testing.T) {
	for _, mode := range []string{"sidebar", "collapsed", "empty"} {
		t.Run(mode, func(t *testing.T) {
			isolatePrefs(t)
			fake := seedBoard()
			if mode == "empty" {
				fake.projects = nil
				fake.tasks = nil
			}
			b := newBoard(fake)
			b.prefs.SidebarCollapsed = mode == "collapsed"
			tt := newBoardTester(b)
			waitFor(t, tt, "看板载入", func() bool { return !b.loading })
			settle(t, tt)
			cardLabel := "卡片仅显示标题，卡片：完整内容 · 点击仅显示标题"
			layoutLabel := "纵向排列状态列，布局：横向 · 点击纵向排列状态列"
			card, ok := tt.Find(cardLabel)
			if !ok {
				t.Fatal("missing card display control")
			}
			layout, ok := tt.Find(layoutLabel)
			if !ok || card.X < renderW-100 || layout.X <= card.X || layout.Y > 40 {
				t.Fatalf("controls are not at the top right: card=%+v layout=%+v", card, layout)
			}
			if err := tt.Click(layoutLabel); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			settle(t, tt)
			if !b.prefs.BoardVertical || !loadUIPrefs().BoardVertical {
				t.Fatal("vertical layout was not applied and saved")
			}
			if err := tt.Click("纵向排列状态列，布局：纵向 · 点击横向排列状态列"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if b.prefs.BoardVertical || loadUIPrefs().BoardVertical {
				t.Fatal("horizontal layout was not restored and saved")
			}
		})
	}
}
