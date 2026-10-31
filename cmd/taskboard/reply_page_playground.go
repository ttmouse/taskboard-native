//go:build motionplayground

package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

// replyDemoComment is one hand-written comment in the static review page.
// It exists only in the Playground build and never reaches the service.
type replyDemoComment struct {
	author  string
	at      string
	text    string
	parent  int  // index in replyDemoThread; -1 for a thread root
	gone    bool // quote a parent that has been deleted
	mention string
	replies []int
}

var replyDemoThread = func() []replyDemoComment {
	list := []replyDemoComment{
		{
			author: "豆爸", at: "10-31 14:02",
			text:   "任务详情里面的回复，现在的结构看不出来回复的是哪一个内容，所以需要更真实地还原一下。",
			parent: -1,
		},
		{
			author: "Claude Code", at: "10-31 14:18",
			text:   "改成引用了：回复块把被回复的那条评论原文引出来，作者用强调色，最多两行、超出用省略号。回复按时间正序排在父评论下面，左侧一条竖线表示归属。",
			parent: 0,
		},
		{
			author: "豆爸", at: "10-31 14:24",
			text:   "引用区里如果被引用的评论本身很长呢？",
			parent: 1,
		},
		{
			author: "Claude Code", at: "10-31 14:26",
			text:   "引用最多两行，超出省略，鼠标悬停看全文；被引用的评论被删掉时显示「原评论已删除」，回复块的位置不会跳。",
			parent: 2,
		},
		{
			author: "豆爸", at: "10-31 14:31",
			text:   "好，这样能看出回复的是哪一条了。",
			parent: 1,
		},
		{
			author: "Claude Code", at: "10-31 14:40",
			text:   "这条引用的是已经不存在的评论，用来检查兜底样式。",
			parent: 3, gone: true,
		},
		{
			author: "豆爸", at: "10-31 15:07",
			text:    "第二条根评论：这里放一段比较长的正文，用来看正文换行、以及它下面回复的引用块会不会把整张卡片撑歪。正文段落之间应该保持和详情页一致的间距，不需要额外的分割线。",
			parent:  -1,
			mention: "claude",
		},
		{
			author: "Codex", at: "10-31 15:12",
			text:   "收到，我按新的呈现方式回写结果。",
			parent: 6,
		},
	}
	for i, cm := range list {
		if cm.parent >= 0 {
			list[cm.parent].replies = append(list[cm.parent].replies, i)
		}
	}
	return list
}()

// replyDemoPage draws the static reply sample requested on the card: a
// detail-shaped page that shows how replies read once they quote what they
// answer. It returns true when it took over the pane, so the board never
// renders underneath it.
func (b *board) replyDemoPage(c *ui.Context, t *ui.Theme) bool {
	if !b.replyDemo {
		return false
	}
	ui.Column(c).Key("reply-demo").Fill().Background(t.Background).Children(func() {
		ui.Row(c).FillWidth().Gap(t.Space(3)).
			Padding(t.Space(3.5), t.Space(6)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "任务详情 · 评论回复").FontSize(fmd(t)).FontWeight(600)
			ui.Text(c, "静态样张 · 假数据，不写入看板").FontSize(fxs(t)).
				TextColor(t.TextMuted)
			ui.Spacer(c)
			if ui.Button(c.Key("reply-demo-back"), "返回看板").
				Label("关闭评论回复样张").Clicked() {
				b.replyDemo = false
			}
		})
		ui.Divider(c)
		ui.Scroll(c).FillWidth().Grow(1).Children(func() {
			ui.Row(c).FillWidth().Justify(ui.Center).
				Padding(t.Space(6), t.Space(6)).Children(func() {
				ui.Column(c).FillWidth().MaxWidth(t.Space(150)).Gap(t.Space(4)).Children(func() {
					ui.Text(c, "任务详情里面的回复看不出回复的是哪一条").
						FontSize(fmd(t)).FontWeight(600)
					ui.Text(c, "这个页面只用来确认回复的呈现：引用块、竖线归属、回复顺序、长文截断、被删评论兜底。按钮没有绑定动作。").
						FontSize(fsm(t)).TextColor(t.TextMuted)
					ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "评论").FontSize(fxs(t)).FontWeight(600).
							TextColor(t.TextMuted)
						ui.Text(c, fmt.Sprint(len(replyDemoThread))).
							FontSize(fxs(t)).TextColor(t.TextMuted)
					})
					for i := len(replyDemoThread) - 1; i >= 0; i-- {
						if replyDemoThread[i].parent < 0 {
							replyDemoCard(c, t, i, 0)
						}
					}
				})
			})
		})
	})
	return true
}

// replyDemoCard mirrors commentThreads' card shell, quote block, rail and
// nesting cap so the sample and the real detail pane cannot drift apart.
func replyDemoCard(c *ui.Context, t *ui.Theme, i, depth int) {
	cm := replyDemoThread[i]
	ui.Box(c.Key(fmt.Sprintf("demo-c-%d", i))).FillWidth().
		Padding(t.Space(2.5), t.Space(3)).Gap(t.Space(1.5)).
		Radius(t.Radius + 2).BorderWidth(0).
		Background(t.SurfaceHover.Alpha(0.55)).Children(func() {
		ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Avatar(c, cm.author, nil).Size(20, 20)
			ui.Text(c, cm.author).FontSize(fsm(t)).FontWeight(600).
				MaxLines(1).Ellipsis("…").Tooltip(cm.author)
			if cm.mention != "" {
				ui.Text(c, "@"+runtimeLabel(cm.mention)).FontSize(fxs(t)).
					TextColor(t.Accent).MaxLines(1).Ellipsis("…").
					Tooltip("这条评论指定由 " + runtimeLabel(cm.mention) + " 处理")
			}
			ui.Spacer(c)
			ui.Text(c, cm.at).FontSize(fxs(t)).TextColor(t.TextMuted)
			ui.Button(c.Key(fmt.Sprintf("demo-reply-%d", i)), "回复")
			ui.Icon(c, commentMore).Size(14, 14).TextColor(t.TextMuted)
		})
		if cm.parent >= 0 {
			parent := replyDemoThread[cm.parent]
			commentQuote(c, t, fmt.Sprintf("demo-quote-%d", i),
				parent.author, parent.text, !cm.gone)
		}
		ui.Text(c, cm.text).FontSize(t.FontSize)
	})
	if len(cm.replies) == 0 {
		return
	}
	// Past two levels the rail narrows the card to unreadable width, so deeper
	// replies keep their quote but drop the extra nesting — the same rule
	// commentThreads applies, so the sample never shows more depth than the
	// real pane would.
	if depth >= 2 {
		for _, reply := range cm.replies {
			replyDemoCard(c, t, reply, depth+1)
		}
		return
	}
	ui.Row(c.Key(fmt.Sprintf("demo-thread-%d", i))).FillWidth().
		Gap(t.Space(2)).Children(func() {
		ui.Column(c).Width(2).FillHeight().Radius(1).MarginStart(t.Space(2.5)).
			Background(t.Border)
		ui.Column(c).Grow(1).Gap(t.Space(2)).Children(func() {
			for _, reply := range cm.replies {
				replyDemoCard(c, t, reply, depth+1)
			}
		})
	})
}
