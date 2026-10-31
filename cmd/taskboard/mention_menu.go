package main

import (
	"os"
	"path/filepath"
	"strings"

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
	query, hasFileQuery := commentMentionSearch(d)
	channels := executor.Runtimes()
	showChannels := !hasFileQuery || query == ""
	var files []workspaceMentionTarget
	if d.mentionOpen {
		files = b.filteredWorkspaceMentions(d, query)
	}
	channelCount := 0
	if showChannels {
		channelCount = len(channels)
	}
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
				if showChannels {
					ui.Text(c, "跟进渠道").
						FontSize(fxs(t)).TextColor(t.TextMuted).
						Padding(t.Space(1), t.Space(1.5), t.Space(0.5), t.Space(1.5))
				}
				for i, channel := range channels[:channelCount] {
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
				if len(files) > 0 {
					label := "项目文件"
					if query != "" {
						label = "匹配文件"
					}
					ui.Text(c, label).FontSize(fxs(t)).TextColor(t.TextMuted).
						Padding(t.Space(1), t.Space(1.5), t.Space(0.5), t.Space(1.5))
				}
				for i, target := range files {
					index := channelCount + i
					target := target
					item := ui.ButtonBase(c.Key("file-mention-"+target.Path)).
						Label("提及项目文件 "+target.Path).
						FillWidth().Height(t.Space(8)).Padding(t.Space(1), t.Space(1.5)).
						Gap(t.Space(1)).AlignItems(ui.Center).Justify(ui.Start).
						BorderWidth(0).Radius(t.Radius)
					face := ui.Transparent
					if index == d.mentionIndex {
						face = t.Text.Alpha(0.12)
					}
					if item.Hovered() {
						face = t.Text.Alpha(0.08)
					}
					if item.Pressed() {
						face = t.Text.Alpha(0.14)
					}
					item.Background(face).Children(func() {
						icon := "📄"
						if target.IsDir {
							icon = "📁"
						}
						ui.Text(c, icon).FontSize(fsm(t))
						ui.Text(c, target.Path).FontSize(fsm(t)).TextColor(t.Text).
							MaxLines(1).Ellipsis("…").Tooltip(target.Path)
					})
					if item.Clicked() {
						pickWorkspaceMention(&d.comment, &d.commentFiles, target.Path)
						d.mentionOpen = false
						d.commentFocus = 3
					}
				}
				if len(files) == 0 {
					// With no query there is nothing to match yet, so the panel
					// stays short and says how the file search works instead of
					// listing paths the user did not ask for.
					hint := "没有匹配的项目文件"
					if showChannels {
						hint = "直接输入即可检索项目文件"
					}
					ui.Text(c, hint).FontSize(fsm(t)).
						TextColor(t.TextMuted).Padding(t.Space(1.5), t.Space(2))
				}
			})
		})
	})
}

func (b *board) filteredWorkspaceMentions(d *detail, query string) []workspaceMentionTarget {
	if d == nil {
		return nil
	}
	// A bare @ has not started a search yet: the menu is channels only. Listing
	// every workspace path here buried the channels and made the panel tall.
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}
	if d.fileMentionTargets == nil {
		if p, ok := b.projectByID(d.snap.ProjectID); ok && strings.TrimSpace(p.WorkspacePath) != "" {
			d.fileMentionTargets = workspaceMentionTargets(p.WorkspacePath)
		} else {
			d.fileMentionTargets = []workspaceMentionTarget{}
		}
	}
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}
	out := make([]workspaceMentionTarget, 0, 20)
	for _, target := range d.fileMentionTargets {
		if strings.Contains(strings.ToLower(target.Path), query) {
			out = append(out, target)
			if len(out) == 20 {
				break
			}
		}
	}
	return out
}

func (b *board) taskFileMentionMenu(c *ui.Context, t *ui.Theme, f *form, box ui.Element) {
	query, open := workspaceMentionQuery(f.name)
	if open {
		f.fileMentionOpen = true
		if f.fileMentionProjectID != f.projectID || f.fileMentionTargets == nil {
			f.fileMentionProjectID = f.projectID
			if p, ok := b.projectByID(f.projectID); ok {
				f.fileMentionTargets = workspaceMentionTargets(p.WorkspacePath)
			} else {
				f.fileMentionTargets = []workspaceMentionTarget{}
			}
		}
	}
	if !open {
		f.fileMentionOpen = false
	}
	if !f.fileMentionOpen {
		return
	}
	files := filterWorkspaceMentionTargets(f.fileMentionTargets, query, 20)
	ui.PopoverBase(c, box, &f.fileMentionOpen, func(panel ui.Element) {
		panel.Width(t.Space(70)).Padding(t.Space(1.5)).Radius(t.Radius+6).
			BorderWidth(1).BorderColor(t.Border).Background(t.Surface).
			Shadow(0, 14, 34, 0, ui.RGBA(0, 0, 0, 0.22)).Margin(t.Space(1)).
			AttachTo(box, ui.AnchorTopLeft, ui.AnchorBottomLeft)
		panel.Children(func() {
			ui.Column(c).FillWidth().Gap(t.Space(0.5)).Children(func() {
				ui.Text(c, "项目文件").FontSize(fxs(t)).TextColor(t.TextMuted).
					Padding(t.Space(1), t.Space(1.5), t.Space(0.5), t.Space(1.5))
				for i, target := range files {
					target := target
					item := ui.ButtonBase(c.Key("task-file-mention-"+target.Path)).
						Label("提及项目文件 "+target.Path).FillWidth().Height(t.Space(8)).
						Padding(t.Space(1), t.Space(1.5)).Gap(t.Space(1)).
						AlignItems(ui.Center).Justify(ui.Start).BorderWidth(0).Radius(t.Radius)
					face := ui.Transparent
					if i == f.fileMentionIndex {
						face = t.Text.Alpha(0.12)
					}
					if item.Hovered() {
						face = t.Text.Alpha(0.08)
					}
					item.Background(face).Children(func() {
						icon := "📄"
						if target.IsDir {
							icon = "📁"
						}
						ui.Text(c, icon).FontSize(fsm(t))
						ui.Text(c, target.Path).FontSize(fsm(t)).TextColor(t.Text).
							MaxLines(1).Ellipsis("…").Tooltip(target.Path)
					})
					if item.Clicked() {
						pickWorkspaceMention(&f.name, &f.fileMentions, target.Path)
						f.fileMentionOpen = false
					}
				}
				if len(files) == 0 {
					hint := "没有匹配的项目文件"
					if strings.TrimSpace(query) == "" {
						hint = "直接输入即可检索项目文件"
					}
					ui.Text(c, hint).FontSize(fsm(t)).TextColor(t.TextMuted).
						Padding(t.Space(1.5), t.Space(2))
				}
			})
		})
	})
}

func (b *board) quickFileMentionMenu(c *ui.Context, t *ui.Theme, box ui.Element) {
	query, open := workspaceMentionQuery(b.quickTitle)
	if open {
		b.quickFileMentionOpen = true
		if b.quickFileMentionProjectID != b.cur || b.quickFileMentionTargets == nil {
			b.quickFileMentionProjectID = b.cur
			if p, ok := b.projectByID(b.cur); ok {
				b.quickFileMentionTargets = workspaceMentionTargets(p.WorkspacePath)
			} else {
				b.quickFileMentionTargets = []workspaceMentionTarget{}
			}
		}
	}
	if !open {
		b.quickFileMentionOpen = false
	}
	if !b.quickFileMentionOpen {
		return
	}
	files := filterWorkspaceMentionTargets(b.quickFileMentionTargets, query, 20)
	ui.PopoverBase(c, box, &b.quickFileMentionOpen, func(panel ui.Element) {
		panel.Width(t.Space(70)).Padding(t.Space(1.5)).Radius(t.Radius+6).
			BorderWidth(1).BorderColor(t.Border).Background(t.Surface).
			Shadow(0, 14, 34, 0, ui.RGBA(0, 0, 0, 0.22)).Margin(t.Space(1)).
			AttachTo(box, ui.AnchorTopLeft, ui.AnchorBottomLeft)
		panel.Children(func() {
			ui.Column(c).FillWidth().Gap(t.Space(0.5)).Children(func() {
				ui.Text(c, "项目文件").FontSize(fxs(t)).TextColor(t.TextMuted).
					Padding(t.Space(1), t.Space(1.5), t.Space(0.5), t.Space(1.5))
				for i, target := range files {
					target := target
					item := ui.ButtonBase(c.Key("quick-file-mention-"+target.Path)).
						Label("提及项目文件 "+target.Path).FillWidth().Height(t.Space(8)).
						Padding(t.Space(1), t.Space(1.5)).Gap(t.Space(1)).
						AlignItems(ui.Center).Justify(ui.Start).BorderWidth(0).Radius(t.Radius)
					face := ui.Transparent
					if i == b.quickFileMentionIndex {
						face = t.Text.Alpha(0.12)
					}
					if item.Hovered() {
						face = t.Text.Alpha(0.08)
					}
					item.Background(face).Children(func() {
						icon := "📄"
						if target.IsDir {
							icon = "📁"
						}
						ui.Text(c, icon).FontSize(fsm(t))
						ui.Text(c, target.Path).FontSize(fsm(t)).TextColor(t.Text).
							MaxLines(1).Ellipsis("…").Tooltip(target.Path)
					})
					if item.Clicked() {
						pickWorkspaceMention(&b.quickTitle, &b.quickFileMentions, target.Path)
						b.quickFileMentionOpen = false
					}
				}
				if len(files) == 0 {
					hint := "没有匹配的项目文件"
					if strings.TrimSpace(query) == "" {
						hint = "直接输入即可检索项目文件"
					}
					ui.Text(c, hint).FontSize(fsm(t)).TextColor(t.TextMuted).
						Padding(t.Space(1.5), t.Space(2))
				}
			})
		})
	})
}

func filterWorkspaceMentionTargets(targets []workspaceMentionTarget, query string, limit int) []workspaceMentionTarget {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}
	out := make([]workspaceMentionTarget, 0, limit)
	for _, target := range targets {
		if strings.Contains(strings.ToLower(target.Path), query) {
			out = append(out, target)
			if len(out) == limit {
				break
			}
		}
	}
	return out
}

func (b *board) workspaceMentionChips(c *ui.Context, t *ui.Theme, files *[]string, root, key string) {
	if files == nil || len(*files) == 0 {
		return
	}
	// Content-sized row so chips sit after the draft in the composer Row
	// instead of claiming a full-width strip above/beside the field.
	ui.Row(c.Key(key)).Gap(t.Space(1)).Children(func() {
		for i, rel := range *files {
			i, rel := i, rel
			icon := "📄"
			if root != "" {
				if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil && info.IsDir() {
					icon = "📁"
				}
			}
			label := icon + " " + filepath.Base(filepath.FromSlash(rel))
			if ui.Button(c.Key(key+"-"+rel), label+" ×").
				Label("移除文件提及 "+rel).Tooltip("项目路径："+rel).
				FontSize(fxs(t)).Padding(t.Space(0.5), t.Space(1)).BorderWidth(0).
				Radius(t.Space(2)).Background(t.Accent.Alpha(0.16)).TextColor(t.Accent).Clicked() {
				*files = append((*files)[:i], (*files)[i+1:]...)
			}
		}
	})
}

func pickWorkspaceMention(text *string, files *[]string, rel string) {
	*text = clearWorkspaceMentionQuery(*text)
	*files = appendWorkspaceMention(*files, rel)
}

func (b *board) selectMentionOption(d *detail, index int) {
	query, hasFileQuery := commentMentionSearch(d)
	showChannels := !hasFileQuery || query == ""
	channels := executor.Runtimes()
	if showChannels && index < len(channels) {
		b.selectMentionChannel(d, channels[index].ID)
		return
	}
	files := b.filteredWorkspaceMentions(d, query)
	fileIndex := index
	if showChannels {
		fileIndex -= len(channels)
	}
	if fileIndex >= 0 && fileIndex < len(files) {
		if strings.HasPrefix(d.comment, d.mentionSearchBase) {
			d.comment = d.mentionSearchBase
		}
		pickWorkspaceMention(&d.comment, &d.commentFiles, files[fileIndex].Path)
		d.mentionOpen = false
		d.commentFocus = 3
	}
}

func commentMentionSearch(d *detail) (string, bool) {
	if query, ok := workspaceMentionQuery(d.comment); ok {
		return query, true
	}
	if d != nil && d.mentionOpen && strings.HasPrefix(d.comment, d.mentionSearchBase) {
		query := strings.TrimSpace(strings.TrimPrefix(d.comment, d.mentionSearchBase))
		if query != "" && !strings.ContainsAny(query, " \t\n") {
			return query, true
		}
	}
	return "", false
}

func mentionOptionCount(channels int, files int, showChannels bool) int {
	if showChannels {
		return channels + files
	}
	return files
}

func (b *board) selectMentionChannel(d *detail, channelID string) {
	if strings.HasPrefix(d.comment, d.mentionSearchBase) {
		d.comment = d.mentionSearchBase
	}
	d.commentMention = channelID
	d.mentionOpen = false
	// Picking a channel must not steal focus from the draft.
	d.commentFocus = 3
}
