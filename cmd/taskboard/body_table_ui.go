package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown/parser"
	"taskboard-native/internal/model"
)

// detailBodyView renders the task description as Markdown blocks. When the
// body contains GFM tables, each top-level span (table or plain text between
// tables) is clickable on its own: click opens only that span's source;
// blur/Esc splices it back. Bodies without tables keep the previous single
// overlay. No rendered table preview sits under an open source editor.
func (b *board) detailBodyView(c *ui.Context, d *detail) {
	t := c.Theme()
	body := ui.Box(c.Key("detail-body")).FillWidth().MinHeight(t.Space(8))
	body.Children(func() {
		if strings.TrimSpace(d.body) == "" {
			ui.Text(c, "添加任务描述，可粘贴图片…").FontSize(fsm(t)).TextColor(t.TextMuted)
			b.detailBodyFullEditOverlay(c, d)
			return
		}
		spans := bodyTableSpans(d.body)
		if len(spans) == 0 {
			b.detailMarkdown(c, d.body)
			b.detailBodyFullEditOverlay(c, d)
			return
		}
		ui.Column(c.Key("detail-body-blocks")).FillWidth().Gap(t.Space(2)).Children(func() {
			pos := 0
			for _, sp := range spans {
				if sp.Start > pos {
					b.detailBodyPlainSegment(c, d, d.body[pos:sp.Start], pos, sp.Start)
				}
				if d.editTable && d.tableStart == sp.Start && d.tableEnd == sp.End {
					b.detailBlockEditor(c, d, "表格编辑器", "| 列 | … |")
				} else {
					b.detailBodyTableSegment(c, d, sp)
				}
				pos = sp.End
			}
			if pos < len(d.body) {
				b.detailBodyPlainSegment(c, d, d.body[pos:], pos, len(d.body))
			}
		})
	})
}

func (b *board) detailBodyFullEditOverlay(c *ui.Context, d *detail) {
	edit := ui.ButtonBase(c.Key("detail-body-edit")).Absolute().Fill().
		Padding(0).BorderWidth(0).Background(ui.Transparent).
		Focusable().Cursor(ui.CursorPointer).Label("编辑任务正文")
	if edit.Clicked() {
		d.commitTableEdit()
		d.editBody = true
		d.bodyTop = true
		d.bodyEditSeenFocus = false
	}
}

func (b *board) detailBodyPlainSegment(c *ui.Context, d *detail, seg string, start, end int) {
	if strings.TrimSpace(seg) == "" {
		return
	}
	if d.editTable && d.tableStart == start && d.tableEnd == end {
		b.detailBlockEditor(c, d, "正文块编辑器", "编辑此段…")
		return
	}
	box := ui.Box(c.Key("detail-body-plain-" + strconv.Itoa(start))).FillWidth()
	box.Children(func() {
		b.detailMarkdown(c, seg)
		edit := ui.ButtonBase(c.Key("detail-body-edit-" + strconv.Itoa(start))).Absolute().Fill().
			Padding(0).BorderWidth(0).Background(ui.Transparent).
			Focusable().Cursor(ui.CursorPointer).Label("编辑正文块")
		if edit.Clicked() {
			// Typora-style: only this inter-table span becomes source.
			d.beginTableEdit(start, end)
		}
	})
}

func (b *board) detailBodyTableSegment(c *ui.Context, d *detail, sp bodyTableSpan) {
	src := d.body[sp.Start:sp.End]
	box := ui.Box(c.Key(fmt.Sprintf("detail-table-%d", sp.Start))).FillWidth()
	box.Children(func() {
		doc := parser.Parse(src)
		idx := -1
		for i := 1; i < doc.Block(0).Next; i = doc.Block(i).Next {
			if doc.Block(i).Kind == parser.Table {
				idx = i
				break
			}
		}
		if idx >= 0 {
			b.fitMarkdownTable(c, doc, idx)
		} else {
			b.detailMarkdown(c, src)
		}
		edit := ui.ButtonBase(c.Key(fmt.Sprintf("detail-table-edit-%d", sp.Start))).Absolute().Fill().
			Padding(0).BorderWidth(0).Background(ui.Transparent).
			Focusable().Cursor(ui.CursorPointer).Label("编辑表格")
		if edit.Clicked() {
			d.beginTableEdit(sp.Start, sp.End)
		}
	})
}

func (b *board) detailBlockEditor(c *ui.Context, d *detail, label, placeholder string) {
	t := c.Theme()
	input := ui.TextArea(c, &d.tableEdit).Label(label).Placeholder(placeholder).
		FillWidth().MinHeight(detailBodyMinHeight(d.tableEdit, t)).
		BorderWidth(1).BorderColor(t.Border).Radius(t.Radius).
		Background(ui.Transparent).
		Padding(t.Space(2), t.Space(2)).
		AutoFocus()
	if localImageMarkdown.MatchString(d.tableEdit) {
		input.Lines(1, 1000)
	} else {
		input.Lines(0, 0)
	}
	if d.tableTop {
		input.SetTextSelection(0, 0)
		d.tableTop = false
	}
	input.OnTextKey(func(k ui.TextKey) bool {
		if k.Key == ui.KeyEscape && k.Mods == 0 {
			d.commitTableEdit()
			return true
		}
		return false
	})
	if input.Focused() {
		d.tableEditSeenFocus = true
	}
	if d.tableEditSeenFocus && !input.Focused() && !input.Hovered() {
		d.commitTableEdit()
	}
	b.imageInput(input, d, &d.tableEdit, &d.bodyImage, mygo.Clipboard.ReadImage)
	b.imageRemoveControls(c, &d.tableEdit, &d.bodyImage, "正文块")
}

// commentBodyView renders one comment like the task body: Markdown by default;
// when it contains tables, clicking a table (own comments only) edits just
// that table's pipe source. Spans use raw comment text (not commentMarkdown),
// because hard-break spaces would shift offsets.
func (b *board) commentBodyView(c *ui.Context, d *detail, cm model.Comment) {
	base := cm.Text
	if d.commentBlockID == cm.ID {
		base = d.commentBlockBase
	}
	spans := bodyTableSpans(base)
	own := cm.Author == b.agent
	if len(spans) == 0 {
		b.detailMarkdown(c, commentMarkdown(base))
		return
	}
	ui.Column(c.Key("comment-blocks-"+cm.ID)).FillWidth().Gap(c.Theme().Space(2)).Children(func() {
		pos := 0
		for _, sp := range spans {
			if sp.Start > pos {
				seg := base[pos:sp.Start]
				if strings.TrimSpace(seg) != "" {
					b.detailMarkdown(c, commentMarkdown(seg))
				}
			}
			editing := d.commentBlockID == cm.ID && d.commentBlockStart == sp.Start && d.commentBlockEnd == sp.End
			if editing {
				b.commentBlockEditor(c, d)
			} else {
				src := base[sp.Start:sp.End]
				box := ui.Box(c.Key(fmt.Sprintf("comment-table-%s-%d", cm.ID, sp.Start))).FillWidth()
				box.Children(func() {
					doc := parser.Parse(src)
					idx := -1
					for i := 1; i < doc.Block(0).Next; i = doc.Block(i).Next {
						if doc.Block(i).Kind == parser.Table {
							idx = i
							break
						}
					}
					if idx >= 0 {
						b.fitMarkdownTable(c, doc, idx)
					} else {
						b.detailMarkdown(c, commentMarkdown(src))
					}
					if own && !b.commenting {
						edit := ui.ButtonBase(c.Key(fmt.Sprintf("comment-table-edit-%s-%d", cm.ID, sp.Start))).Absolute().Fill().
							Padding(0).BorderWidth(0).Background(ui.Transparent).
							Focusable().Cursor(ui.CursorPointer).Label("编辑评论表格")
						if edit.Clicked() {
							srcBase := cm.Text
							if d.commentBlockID == cm.ID {
								srcBase = d.commentBlockBase
							} else if d.commentBlockID != "" {
								b.commitCommentBlockEdit()
								srcBase = cm.Text
								for _, x := range d.comments {
									if x.ID == cm.ID {
										srcBase = x.Text
										break
									}
								}
							}
							// Recompute span against the base we will edit.
							for _, rsp := range bodyTableSpans(srcBase) {
								if rsp.Start == sp.Start && rsp.End == sp.End {
									d.beginCommentBlockEdit(cm.ID, srcBase, rsp.Start, rsp.End)
									return
								}
							}
							if rs := bodyTableSpans(srcBase); len(rs) > 0 {
								// Fallback: same ordinal table.
								ord := 0
								for i, s := range spans {
									if s.Start == sp.Start {
										ord = i
										break
									}
								}
								if ord < len(rs) {
									d.beginCommentBlockEdit(cm.ID, srcBase, rs[ord].Start, rs[ord].End)
								}
							}
						}
					}
				})
			}
			pos = sp.End
		}
		if pos < len(base) {
			seg := base[pos:]
			if strings.TrimSpace(seg) != "" {
				b.detailMarkdown(c, commentMarkdown(seg))
			}
		}
	})
}

func (b *board) commentBlockEditor(c *ui.Context, d *detail) {
	t := c.Theme()
	input := ui.TextArea(c, &d.commentBlockEdit).Label("评论表格编辑器").Placeholder("| 列 | … |").
		FillWidth().MinHeight(t.Space(12)).
		BorderWidth(1).BorderColor(t.Border).Radius(t.Radius).
		Background(ui.Transparent).
		Padding(t.Space(2), t.Space(2)).
		AutoFocus()
	input.Lines(0, 0)
	if d.commentBlockTop {
		input.SetTextSelection(0, 0)
		d.commentBlockTop = false
	}
	input.OnTextKey(func(k ui.TextKey) bool {
		if k.Key == ui.KeyEscape && k.Mods == 0 {
			b.commitCommentBlockEdit()
			return true
		}
		return false
	})
	if input.Focused() {
		d.commentBlockSeenFocus = true
	}
	if d.commentBlockSeenFocus && !input.Focused() && !input.Hovered() {
		b.commitCommentBlockEdit()
	}
}
