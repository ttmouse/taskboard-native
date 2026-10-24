// The board's view: top bar, columns of cards, the task detail and the
// small forms. Every color comes from the theme; every measurement from
// the theme's spacing and type scale, as docs/design.md specifies.
package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"taskboard-native/internal/client"
	"taskboard-native/internal/model"

	"github.com/egoist/mygo/plugins/loading"
	"github.com/egoist/mygo/ui"
)

// --- palette ---

// colFace is the column panel's fill: one step off the window in either
// appearance.
func colFace(t *ui.Theme) ui.Color {
	if t.Dark {
		return t.Background
	}
	return t.Surface
}

// cardFace is a resting card's fill, cardHover its fill under the pointer.
func cardFace(t *ui.Theme) ui.Color {
	if t.Dark {
		return t.Surface
	}
	return t.Background
}

// cardHover is a resting card's fill under the pointer. It must never
// equal the column's own face: on light, the card is Background over a
// Surface column, so hover goes one step further to SurfaceHover; on dark,
// the card is Surface over a Background column, so hover goes lighter.
func cardHover(t *ui.Theme) ui.Color {
	if t.Dark {
		return t.SurfaceHover
	}
	return t.SurfaceHover
}

// hoverBorder is the card's outline under the pointer: stronger than the
// resting border so the edge survives on top of a filled face.
func hoverBorder(t *ui.Theme) ui.Color { return t.Border.Mix(t.Text, 0.35) }

// panelBorder outlines a column: one step darker than the flat border so
// the panel reads as a surface, not an accident of tone.
func panelBorder(t *ui.Theme) ui.Color { return t.Border.Mix(t.Text, 0.12) }

// dashedBorder outlines an empty column's placeholder, a step deeper still.
func dashedBorder(t *ui.Theme) ui.Color { return t.Border.Mix(t.Text, 0.3) }

// typeScale is the design's f-* steps relative to the theme's size.
func fxs(t *ui.Theme) float32 { return t.FontSize - 2 }
func fsm(t *ui.Theme) float32 { return t.FontSize - 1 }
func fmd(t *ui.Theme) float32 { return t.FontSize + 2 }
func flg(t *ui.Theme) float32 { return t.FontSize + 7 }

// view renders one frame.
func (b *board) view(c *ui.Context) {
	b.frameCtx = c
	b.pump()
	theme := c.Theme()
	if theme.Dark {
		// The dark theme's #3b82f6 with white text is 3.68:1, under AA for
		// the 13px primary-button label. Hand the frame a copy with the
		// light theme's deeper blue (5.17:1) and mygo's own derived
		// hover/pressed steps, so no new colors enter the palette.
		cp := *theme
		accent := ui.Hex("#2563eb")
		cp.Accent, cp.AccentHover, cp.AccentPressed = accent, accent.Mix(ui.Color{R: 255, G: 255, B: 255, A: 255}, 0.2), accent.Mix(ui.Color{A: 255}, 0.15)
		cp.AccentText = ui.Hex("#ffffff")
		cp.Selection, cp.Focus = accent.Alpha(0.4), accent.Mix(ui.Color{R: 255, G: 255, B: 255, A: 255}, 0.2).Alpha(0.6)
		c.SetTheme(&cp)
		theme = c.Theme()
	}

	t := theme
	b.curTheme = t
	ui.Column(c).Fill().Children(func() {
		b.topBar(c, t)
		ui.Divider(c).BorderColor(t.Border)
		if b.status == client.Offline {
			b.offlineBanner(c, t)
		}
		b.boardArea(c, t)
	})

	if b.detail != nil {
		b.detailView(c, t)
	}
	if b.form != nil {
		b.formView(c, t)
	}
	if b.conflict != nil {
		b.conflictView(c, t)
	}
}

// --- top bar ---

func (b *board) topBar(c *ui.Context, t *ui.Theme) {
	ui.Row(c).Height(52).Padding(0, t.Space(4)).Gap(t.Space(3)).
		AlignItems(ui.Center).BorderWidth(0, 0, 0, 0).Children(func() {
		ui.MenuButton(c, b.projectName(), func(m *ui.Menu) {
			for _, p := range b.projects {
				item := m.Item(p.Name)
				item.Checked(p.ID == b.cur)
				if item.Chosen() {
					b.openProject(p.ID)
				}
			}
			if len(b.projects) > 0 {
				m.Separator()
			}
			if m.Item("新建项目…").Chosen() {
				b.openForm()
			}
		}).Label("项目切换").FontSize(fmd(t)).FontWeight(500)
		if b.switching {
			loading.Ring(c, loading.Options{Size: 16, Color: t.Accent}).Label("正在切换项目")
		}
		ui.Spacer(c)
		if ui.Button(c, "刷新").Label("刷新看板").Clicked() {
			b.load()
		}
		if ui.Button(c, "新建项目").Clicked() {
			b.openForm()
		}
		b.syncDot(c, t)
	})
}

// syncDot shows the stream's health: a green dot at rest, a pulse just
// after an event, and a spinner while (re)connecting. While the board's
// own full-page indicator shows, this one stays a plain dot, so the
// screen never carries two board-level spinners at once.
func (b *board) syncDot(c *ui.Context, t *ui.Theme) {
	switch b.status {
	case client.Online:
		if !b.loading && time.Now().Before(b.pulseTill) {
			loading.Pulse(c, loading.Options{Size: 10, Color: t.Accent}).Label("正在同步更新")
			c.After(time.Until(b.pulseTill))
			return
		}
		ui.Box(c).Size(8, 8).Radius(999).Background(t.Success).Label("实时同步已连接")
	case client.Offline:
		loading.Comet(c, loading.Options{Size: 12, Color: t.Warning}).Label("正在重新连接本地服务")
	default:
		if b.loading {
			ui.Box(c).Size(8, 8).Radius(999).Background(t.Border).Label("正在连接本地服务")
			return
		}
		loading.Arc(c, loading.Options{Size: 12, Color: t.TextMuted}).Label("正在连接本地服务")
	}
}

// --- offline banner ---

func (b *board) offlineBanner(c *ui.Context, t *ui.Theme) {
	ui.Row(c).Height(36).Padding(0, t.Space(4)).Gap(t.Space(2)).
		Background(t.Warning.Alpha(0.14)).AlignItems(ui.Center).Children(func() {
		loading.Comet(c, loading.Options{Size: 12, Color: t.Warning}).Label("正在重新连接本地服务")
		ui.Text(c, "本地服务未连接，正在重试…").FontSize(fsm(t)).TextColor(t.Text)
		ui.Spacer(c)
		if ui.Button(c, "重试").Clicked() {
			b.startStream()
			b.load()
		}
	})
	ui.Divider(c).BorderColor(t.Border)
}

// --- board ---

func (b *board) boardArea(c *ui.Context, t *ui.Theme) {
	if b.loading {
		b.loadingState(c, t)
		return
	}
	if len(b.projects) == 0 {
		b.noProjectState(c, t)
		return
	}
	if b.loadErr != "" {
		ui.Column(c).Grow(1).Center().Gap(t.Space(2)).Children(func() {
			ui.Text(c, "看板没能加载").FontSize(fmd(t)).Bold()
			ui.Text(c, b.loadErr).FontSize(fsm(t)).TextColor(t.TextMuted)
			if ui.Button(c, "重试").Clicked() {
				b.load()
			}
		})
		return
	}

	// Columns split the window evenly, within the design's 240..360
	// range; a narrower window scrolls instead of squeezing them.
	width := b.columnWidth(c, t)
	board := ui.ScrollHorizontal(c).Grow(1).Padding(t.Space(4)).Gap(t.Space(3)).AlignItems(ui.Stretch)
	if b.switching {
		board.Opacity(0.55)
	}
	board.Children(func() {
		for i := range b.columns {
			b.columnView(c, t, &b.columns[i], width)
		}
	})
}

func (b *board) loadingState(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Grow(1).Center().Gap(t.Space(3)).Children(func() {
		loading.Arc(c, loading.Options{Size: 20, Color: t.TextMuted}).Label("正在载入看板")
		ui.Text(c, "正在载入看板…").FontSize(fsm(t)).TextColor(t.TextMuted)
	})
}

func (b *board) noProjectState(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Grow(1).Center().Gap(t.Space(2)).PaddingY(t.Space(8)).Children(func() {
		ui.Text(c, "还没有项目").FontSize(flg(t)).FontWeight(600)
		ui.Text(c, "创建第一个项目，开始整理任务。").FontSize(fsm(t)).TextColor(t.TextMuted)
		if ui.PrimaryButton(c, "新建项目").Clicked() {
			b.openForm()
		}
	})
}

// columnWidth is the width of one column: the window's room split among
// the columns, clamped to the design's 240..360.
func (b *board) columnWidth(c *ui.Context, t *ui.Theme) float32 {
	w, _ := c.Size()
	room := w - 2*t.Space(4) - float32(max(len(b.columns)-1, 0))*t.Space(3)
	if len(b.columns) == 0 {
		return 320
	}
	width := room / float32(len(b.columns))
	return min(max(width, 240), 360)
}

// columnView is one column: header, quick add, and its cards.
func (b *board) columnView(c *ui.Context, t *ui.Theme, col *model.Column, width float32) {
	tasks := b.tasksIn(col.ID)
	panel := ui.Column(c.Key(col.ID)).Width(width).FillHeight().
		Padding(t.Space(2.5)).Gap(t.Space(2)).Radius(t.Radius+4).
		Background(colFace(t)).Border(1, panelBorder(t)).
		Role(ui.RoleGroup).
		Label(fmt.Sprintf("%s列，%d 个任务", col.Name, len(tasks)))

	if _, over := ui.DragOver[cardDrag](panel); over {
		panel.Border(2, t.Accent)
	}
	if id, ok := ui.Drop[cardDrag](panel); ok {
		b.dropOn(string(id), col, panel)
	}

	panel.Children(func() {
		ui.Row(c).Height(32).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, col.Name).FontSize(t.FontSize).FontWeight(600).
				Role(ui.RoleHeading).Level(2)
			ui.Spacer(c)
			ui.Badge(c, strconv.Itoa(len(tasks))).Label(fmt.Sprintf("%s列，%d 个任务", col.Name, len(tasks)))
		})

		b.quickAdd(c, t, col)

		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).FillWidth().Gap(t.Space(2)).Padding(0, 0, t.Space(6), 0).
				FocusGroup(ui.Vertical).Label(col.Name + "列任务").Children(func() {
				if len(tasks) == 0 {
					ui.Box(c).FillWidth().MinHeight(64).Padding(t.Space(3)).Radius(t.Radius).
						BorderWidth(1).BorderColor(dashedBorder(t)).BorderStyle(ui.BorderDashed).Children(func() {
						ui.Text(c, "还没有任务").FontSize(fsm(t)).TextColor(t.TextMuted)
					})
					return
				}
				for i := range tasks {
					b.cardView(c, t, &tasks[i])
				}
			})
		})
	})
}

// quickAdd is the per-column input: Enter creates, and stays for the next.
func (b *board) quickAdd(c *ui.Context, t *ui.Theme, col *model.Column) {
	if b.drafts == nil {
		b.drafts = map[string]string{}
	}
	draft := b.drafts[col.ID]
	in := ui.TextInput(c, &draft).Placeholder("添加任务，回车创建").
		Label("在「" + col.Name + "」中添加任务")
	// The input edits its own copy; keep ours in step for the next frame.
	if draft != b.drafts[col.ID] {
		b.drafts[col.ID] = draft
	}
	if in.Submitted() {
		title := strings.TrimSpace(draft)
		if title != "" {
			b.drafts[col.ID] = ""
			b.createTop(col.ID, title)
		}
	}
}

// createTop creates a card and lifts it to the head of its column, which
// is where a quick add puts it.
func (b *board) createTop(columnID, title string) {
	proj := b.cur
	go func() {
		t, err := b.api.CreateTask(proj, columnID, title, "", "")
		if err != nil {
			b.toast("创建失败", err.Error())
			return
		}
		if _, err := b.api.Move(t.ID, t.Version, columnID, "", true); err != nil {
			b.toast("新任务未能置顶", err.Error())
		}
	}()
}

// cardView draws one card: title, summary, and its footer.
func (b *board) cardView(c *ui.Context, t *ui.Theme, task *model.Task) {
	tr := ui.ElementTransition{
		Duration: 200 * time.Millisecond,
		Ease:     ui.EaseOut,
		Position: true,
		Size:     true,
		Colors:   true,
		Enter:    &ui.Motion{Collapse: true, Scale: 0.98},
		Exit:     &ui.Motion{Collapse: true, Opacity: 0},
		Match:    cardMatch{ID: task.ID},
	}
	card := ui.Box(c.Key(task.ID)).FillWidth().MinHeight(64).
		Padding(t.Space(3)).Gap(t.Space(1.5)).Radius(t.Radius).Border(1, t.Border).
		Focusable().Cursor(ui.CursorPointer).Role(ui.RoleButton).
		Label("打开任务：" + task.Title).
		Drag(cardDrag(task.ID)).Transition(tr)
	// The fill animates between rest, hover, press and the accent wash a
	// card shows for a moment after another agent changed it.
	face := cardFace(t)
	if card.Hovered() {
		face = cardHover(t)
		card.Border(1, hoverBorder(t))
	}
	if card.Pressed() {
		face = t.SurfacePressed
	}
	if until, ok := b.recent[task.ID]; ok {
		if time.Now().Before(until) {
			face = t.Accent.Alpha(0.12)
			c.After(time.Until(until))
		} else {
			delete(b.recent, task.ID)
		}
	}
	card.Background(card.AnimateColor("face", face, ui.Animation{Duration: 200 * time.Millisecond, Ease: ui.EaseOut}))
	if card.Dragging() {
		card.Opacity(0.4)
	}
	card.ContextMenu(func(m *ui.Menu) {
		m.Submenu("移动到", func(sub *ui.Menu) {
			for _, col := range b.columns {
				it := sub.Item(col.Name)
				it.Checked(col.ID == task.ColumnID)
				if it.Chosen() {
					b.moveTask(task.ID, col.ID, "", false)
				}
			}
		})
		if m.Item("打开详情").Chosen() {
			b.openDetail(*task)
		}
		m.Separator()
		if m.Item("归档").Chosen() {
			b.archiveTask(task.ID)
		}
	})

	card.Children(func() {
		ui.Row(c).FillWidth().Gap(t.Space(1)).Children(func() {
			ui.Text(c, task.Title).FontSize(t.FontSize).FontWeight(500).
				MaxLines(2).Ellipsis("…").Grow(1)
			if task.Version > 1 {
				ui.Badge(c, "v"+strconv.Itoa(task.Version)).Label("版本 " + strconv.Itoa(task.Version))
			}
		})
		if summary := firstLines(task.Description, 2); summary != "" {
			ui.Text(c, summary).FontSize(fsm(t)).TextColor(t.TextMuted).
				MaxLines(2).Ellipsis("…")
		}
		ui.Row(c).Height(20).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
			if task.Assignee != "" {
				ui.Avatar(c, task.Assignee, nil).Size(18, 18)
			}
			ui.Spacer(c)
			if n := len(task.Comments); n > 0 {
				ui.Badge(c, strconv.Itoa(n)).Label(strconv.Itoa(n) + " 条评论")
			}
			if task.UpdatedBy != "" && task.UpdatedBy != "local" {
				ui.Text(c, task.UpdatedBy).FontSize(fxs(t)).TextColor(t.TextMuted)
			}
			if b.savingID == task.ID && (b.detail == nil || b.detail.id != task.ID) {
				loading.LinearDots(c, loading.Options{Size: 14, Color: t.TextMuted}).Label("正在保存任务")
			}
			ui.Text(c, stamp(task.UpdatedAt)).FontSize(fxs(t)).TextColor(t.TextMuted)
		})
	})
	b.bounds[task.ID] = card.Bounds()

	if card.Clicked() {
		b.openDetail(*task)
	}
}

// dropOn turns a drop on a column into a move, using the pointer's Y to
// choose the slot.
func (b *board) dropOn(dragID string, col *model.Column, panel ui.Element) {
	_, y, ok := panel.PointerPosition()
	if !ok {
		return
	}
	tasks := b.tasksIn(col.ID)
	after, atTop := "", len(tasks) > 0
	for _, task := range tasks {
		box, has := b.bounds[task.ID]
		if !has {
			continue
		}
		if y >= box.Y+box.H/2 {
			after, atTop = task.ID, false
		} else {
			break
		}
	}
	if after == dragID {
		return
	}
	b.moveTask(dragID, col.ID, after, atTop)
}

// --- detail ---

func (b *board) detailView(c *ui.Context, t *ui.Theme) {
	d := b.detail
	open := d.open
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(t.Space(95)).Gap(t.Space(3)).Children(func() {
			ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "任务详情").FontSize(fmd(t)).FontWeight(600)
				ui.Spacer(c)
				if ui.Button(c, "关闭").Label("关闭详情").Clicked() {
					open = false
				}
			})
			if d.pushed != nil && d.dirty() {
				ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
					ui.Text(c, fmt.Sprintf("该任务已被 %s 更新到 v%d，你的编辑尚未保存。",
						agentName(d.pushBy), d.pushed.Version)).
						FontSize(fsm(t)).TextColor(t.Warning)
				})
			}

			ui.Field(c, "标题", func() {
				ui.TextInput(c, &d.title).Label("任务标题").AutoFocus()
			})
			ui.Field(c, "正文", func() {
				ui.TextArea(c, &d.body).Label("任务正文").MinHeight(96)
			})
			ui.Field(c, "移动到", func() {
				names := b.columnNames()
				if d.column == "" {
					if t := b.task(d.id); t != nil {
						d.column = b.columnName(t.ColumnID)
					} else {
						d.column = names[0]
					}
				}
				if ui.Select(c, &d.column, names).Label("移动到列").Changed() {
					if colID := b.columnID(d.column); colID != "" {
						b.moveTask(d.id, colID, "", false)
					}
				}
			})

			ui.Divider(c).BorderColor(t.Border)
			ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "评论").FontSize(fsm(t)).FontWeight(600).TextColor(t.TextMuted)
				if d.loading {
					loading.LinearDots(c, loading.Options{Size: 12, Color: t.TextMuted}).Label("正在载入评论")
				}
			})
			if len(d.comments) == 0 && !d.loading {
				ui.Text(c, "还没有评论").FontSize(fsm(t)).TextColor(t.TextMuted)
			}
			if len(d.comments) > 0 {
				ui.Scroll(c).MaxHeight(180).Children(func() {
					ui.Column(c).FillWidth().Gap(t.Space(2)).Children(func() {
						for i := range d.comments {
							cm := &d.comments[i]
							if i > 0 {
								ui.Divider(c).BorderColor(t.Border)
							}
							ui.Column(c).Key("c-" + cm.ID).FillWidth().Gap(2).Children(func() {
								ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
									ui.Text(c, cm.Author).FontSize(fsm(t)).FontWeight(600)
									ui.Spacer(c)
									ui.Text(c, cm.CreatedAt.Format("01-02 15:04")).FontSize(fxs(t)).TextColor(t.TextMuted)
								})
								ui.Text(c, cm.Text).FontSize(t.FontSize)
							})
						}
					})
				})
			}
			ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				ui.TextInput(c, &d.comment).Placeholder("写评论，回车发送").Label("写评论").
					Grow(1).Disabled(b.commenting)
				if b.commenting {
					loading.BouncingDots(c, loading.Options{Size: 12, Color: t.Accent}).Label("正在发送评论")
				}
				if ui.PrimaryButton(c, "发送").Label("发送评论").
					Disabled(strings.TrimSpace(d.comment) == "").Clicked() {
					b.sendComment()
				}
			})

			ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				if ui.Button(c, "归档").Label("归档任务").Cursor(ui.CursorPointer).
					TextColor(t.Danger).Clicked() {
					b.archiveTask(d.id)
				}
				if b.archiving {
					loading.Arc(c, loading.Options{Size: 14, Color: t.TextMuted}).Label("正在归档任务")
				}
				ui.Spacer(c)
				if b.saving {
					loading.LinearDots(c, loading.Options{Size: 14, Color: t.TextMuted}).Label("正在保存任务")
				}
				if ui.PrimaryButton(c, "保存").Label("保存任务").Clicked() {
					b.saveDetail()
				}
			})
		})
	})
	if !open {
		b.detail = nil
		return
	}
	d.open = open
}

// --- forms ---

func (b *board) openForm() { b.form = &form{open: true} }

func (b *board) formView(c *ui.Context, t *ui.Theme) {
	f := b.form
	open := f.open
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(t.Space(80)).Gap(t.Space(3)).Children(func() {
			ui.Text(c, "新建项目").FontSize(fmd(t)).FontWeight(600)
			ui.Field(c, "项目名", func() {
				ui.TextInput(c, &f.name).Label("项目名").AutoFocus()
			})
			ui.Row(c).FillWidth().Gap(t.Space(2)).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Clicked() {
					open = false
				}
				if ui.PrimaryButton(c, "创建").Clicked() {
					b.createProject()
				}
			})
		})
	})
	if !open {
		b.form = nil
		return
	}
	f.open = open
}

// --- conflict ---

func (b *board) conflictView(c *ui.Context, t *ui.Theme) {
	cf := b.conflict
	open := cf.open
	choice := ui.AlertDialog(c, &open, cf.title, cf.message, "使用最新版本", "保留我的修改")
	switch choice {
	case 0:
		cf.open = false
		b.conflict = nil
	case 1:
		cf.open = false
		b.conflict = nil
		if fresh := b.task(cf.id); fresh != nil && cf.retry != nil {
			cf.retry(fresh.Version)
		}
	}
	if !open {
		b.conflict = nil
	}
}

// --- format helpers ---

// stamp is the card's time: "刚刚" within a minute, else the clock time.
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	if d := time.Since(t); d >= 0 && d < time.Minute {
		return "刚刚"
	}
	if time.Since(t) < 24*time.Hour && t.Day() == time.Now().Day() {
		return t.Format("15:04")
	}
	return t.Format("01-02")
}

// firstLines returns the first n non-empty lines of s.
func firstLines(s string, n int) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
		if len(out) == n {
			break
		}
	}
	return strings.Join(out, " ")
}
