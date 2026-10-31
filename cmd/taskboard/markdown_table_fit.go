package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown/parser"
)

// fitMarkdownTable draws a GFM table that fills the detail pane width:
// columns share the row equally (MinWidth 0 + Grow), cell text wraps.
// Prefer this over nested horizontal scroll inside the detail's vertical
// scroll — trackpad gestures rarely reach the inner axis, so wide tables
// looked clipped with no way to reach the right edge.
//
// Returns true when index is a Table block and it was drawn.
func (b *board) fitMarkdownTable(c *ui.Context, doc *parser.Document, index int) bool {
	block := doc.Block(index)
	if block.Kind != parser.Table {
		return false
	}
	t := c.Theme()
	cols := len(block.Align)
	if cols == 0 {
		return false
	}
	padX, padY := t.Space(1.5), t.Space(1)
	ui.Column(c).FillWidth().MinWidth(0).
		Border(1, t.Border).Radius(t.Radius).Clip().
		Role(ui.RoleTable).
		Children(func() {
			rowIndex := 0
			for r := index + 1; r < block.Next; r = doc.Block(r).Next {
				row := doc.Block(r)
				if row.Kind != parser.TableRow {
					continue
				}
				rowEl := ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Stretch).Role(ui.RoleRow)
				if row.Header {
					rowEl.Background(t.Text.Alpha(0.04))
				}
				if rowIndex > 0 {
					rowEl.BorderWidth(1, 0, 0, 0).BorderColor(t.Border)
				}
				rowEl.Children(func() {
					k := 0
					for cell := r + 1; cell < row.Next; cell = doc.Block(cell).Next {
						if k >= cols {
							break
						}
						text := strings.TrimSpace(parser.PlainText(doc.Inlines(cell)))
						role := ui.RoleCell
						weight := 400
						if row.Header {
							role = ui.RoleColumnHeader
							weight = 600
						}
						box := ui.Column(c).Grow(1).MinWidth(0).
							Padding(padY, padX).Role(role)
						if k > 0 {
							box.BorderWidth(0, 0, 0, 1).BorderColor(t.Border)
						}
						align := ui.Start
						switch block.Align[k] {
						case parser.AlignCenter:
							align = ui.Center
						case parser.AlignRight:
							align = ui.End
						}
						label := text
						if label == "" {
							label = " "
						}
						box.Children(func() {
							ui.Text(c, label).
								FontSize(fsm(t)).FontWeight(weight).
								TextColor(t.Text).TextAlign(align)
						})
						k++
					}
					// Pad missing trailing cells so column count stays even.
					for ; k < cols; k++ {
						ui.Column(c).Grow(1).MinWidth(0).Padding(padY, padX)
					}
				})
				rowIndex++
			}
		})
	return true
}
