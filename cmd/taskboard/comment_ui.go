package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"taskboard-native/internal/model"
)

// commentTree splits one task's flat comment list into root threads, each
// parent's replies in store order (oldest first), and an ID lookup for the
// quoted parent. The Playground's static sample uses the same shapes.
func commentTree(comments []model.Comment) (roots []model.Comment, children map[string][]model.Comment, byID map[string]model.Comment) {
	children = make(map[string][]model.Comment)
	byID = make(map[string]model.Comment, len(comments))
	for _, cm := range comments {
		byID[cm.ID] = cm
		if cm.ParentCommentID == "" {
			roots = append(roots, cm)
		} else {
			children[cm.ParentCommentID] = append(children[cm.ParentCommentID], cm)
		}
	}
	return roots, children, byID
}

// commentQuote is the quoted parent a reply answers: an accent rail, the
// parent's author, and up to two lines of its text. It replaces the old
// muted "回复给 X" caption, which read as decoration instead of a quotation
// and left the replied-to comment unidentifiable. A parent that is gone
// keeps the same block so the reply does not shift.
func commentQuote(c *ui.Context, t *ui.Theme, key, author, text string, ok bool) {
	ui.Row(c.Key(key)).FillWidth().Gap(t.Space(2)).
		Padding(t.Space(2), t.Space(2.5)).Radius(t.Radius).
		Background(t.Surface).Children(func() {
		ui.Box(c).Width(2).FillHeight().Radius(1).
			Background(t.Accent.Alpha(0.45))
		ui.Column(c).Grow(1).Gap(t.Space(0.5)).Children(func() {
			if !ok {
				ui.Text(c, "原评论已删除").FontSize(fsm(t)).FontWeight(600).
					TextColor(t.TextMuted)
				return
			}
			ui.Text(c, "@"+author).FontSize(fsm(t)).FontWeight(600).
				TextColor(t.Accent).MaxLines(1).Ellipsis("…").
				Label("回复给 " + author)
			preview := strings.Join(strings.Fields(text), " ")
			if preview == "" {
				preview = "（空评论）"
			}
			ui.Text(c, preview).FontSize(fsm(t)).MaxLines(2).Ellipsis("…").
				Tooltip(text)
		})
	})
}
