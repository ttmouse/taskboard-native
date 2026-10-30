package main

import (
	"github.com/egoist/mygo/ui"

	"taskboard-native/internal/executor"
)

// mentionMenu offers the execution channels in a floating list above the
// comment box, the way the assistant composers do: the choice lives over the
// sheet instead of pushing the draft down, and a press anywhere outside or
// Escape closes it without touching the text.
//
// It must be built after the box it anchors to, in the same frame; the panel
// flips above the box when the sheet leaves no room below.
func (b *board) mentionMenu(c *ui.Context, t *ui.Theme, d *detail, box ui.Element) {
	ui.PopoverBase(c, box, &d.mentionOpen, func(panel ui.Element) {
		panel.Width(t.Space(66)).
			Padding(t.Space(1.5)).
			Radius(t.Radius+6).
			BorderWidth(1).BorderColor(t.Border).
			Background(t.Surface).
			Shadow(0, 14, 34, 0, ui.RGBA(0, 0, 0, 0.22)).
			Margin(t.Space(1)).
			// Above the box: a menu that hangs past the sheet's bottom edge
			// reads as detached from the draft it belongs to.
			AttachTo(box, ui.AnchorTopLeft, ui.AnchorBottomLeft)
		panel.Children(func() {
			ui.Column(c).FillWidth().Gap(t.Space(0.5)).Children(func() {
				ui.Text(c, "跟进渠道").
					FontSize(fxs(t)).TextColor(t.TextMuted).
					Padding(t.Space(1), t.Space(1.5), t.Space(0.5), t.Space(1.5))
				channels := executor.Runtimes()
				for i, channel := range channels {
					ch := channel
					item := ui.ButtonBase(c.Key("mention-"+ch.ID)).
						Label("评论提醒 "+ch.Label).
						FillWidth().
						Height(t.Space(8)).
						Padding(t.Space(1), t.Space(1.5)).
						Gap(t.Space(1)).
						AlignItems(ui.Center).
						Justify(ui.Start).
						BorderWidth(0).
						Radius(t.Radius)
					// The menu face answers the pointer, as the item list it is.
					face := ui.Transparent
					if i == d.mentionIndex {
						face = t.Text.Alpha(0.12)
					}
					if item.Hovered() {
						face = t.Text.Alpha(0.08)
					}
					if item.Pressed() {
						face = t.Text.Alpha(0.14)
					}
					item.Background(face).Children(func() {
						ui.Text(c, "@").FontSize(fsm(t)).TextColor(t.TextMuted)
						ui.Text(c, ch.Label).FontSize(fsm(t)).TextColor(t.Text)
					})
					if item.Clicked() {
						b.selectMentionChannel(d, ch.ID)
					}
				}
			})
		})
	})
}

func (b *board) selectMentionChannel(d *detail, channelID string) {
	d.commentMention = channelID
	d.mentionOpen = false
	// Picking a channel must not steal focus from the draft.
	d.commentFocus = 3
}
