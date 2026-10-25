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

	"github.com/egoist/mygo"
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


// The column panel itself carries no outline: in either appearance it is
// one clean step of tone against the window, and the cards carry the
// only borders on the board. The accent appears when a drag hovers.

// typeScale is the design's f-* steps relative to the theme's size.
func fxs(t *ui.Theme) float32 { return t.FontSize - 2 }
func fsm(t *ui.Theme) float32 { return t.FontSize - 1 }
func fmd(t *ui.Theme) float32 { return t.FontSize + 2 }
func flg(t *ui.Theme) float32 { return t.FontSize + 7 }

func syncText(b *board, t *ui.Theme) string {
	switch b.status {
	case client.Online:
		if time.Now().Before(b.pulseTill) {
			return "同步中"
		}
		return "已同步"
	case client.Offline:
		return "重连中"
	default:
		return "连接中"
	}
}

// view renders one frame.
func (b *board) view(c *ui.Context) {
	b.frameCtx = c
	b.pump()
	theme := c.Theme()
	if theme.Dark && !c.Preferences().HighContrast {
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
		if b.status == client.Offline {
			b.offlineBanner(c, t)
		}
		ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
			if len(b.projects) > 0 {
				b.projectSidebar(c, t)
			}
			b.boardArea(c, t)
		})
	})
	// After TrackScroll updates offsets, set thumb visibility for this paint.
	// MyGo would otherwise keep thumbs visible for the whole hover; we only
	// show them briefly while scrolling (gutter still reserved).
	b.applyScrollbarTheme(c, theme)

	if b.detail != nil {
		b.detailView(c, t)
	}
	if b.form != nil {
		b.formView(c, t)
	}
	if b.settings != nil {
		b.settingsView(c, t)
	}
	if b.conflict != nil {
		b.conflictView(c, t)
	}
}

// --- top bar ---

func (b *board) applyScrollbarTheme(c *ui.Context, base *ui.Theme) {
	cp := *base
	if b.scrollbarActive() {
		cp.Scrollbar = base.Scrollbar.Alpha(0.45)
	} else {
		cp.Scrollbar = ui.Transparent
	}
	c.SetTheme(&cp)
	b.curTheme = c.Theme()
}

func (b *board) productionTopBar(c *ui.Context, t *ui.Theme) {
	// Compact ~40 DIP toolbar. Do NOT use max(..., bar.Height):
	// HiddenInset reports ~46–52 and that was the too-tall regression.
	// Traffic lights via TrafficLightPosition.
	bar := c.TitleBar()
	h := t.Space(10) // 40 — room for overlaid traffic lights
	padL := bar.Left + t.Space(2)
	if padL < t.Space(14) {
		padL = t.Space(14)
	}
	padR := max(bar.Right, t.Space(2))
	ui.Row(c).Height(h).Padding(0, padR, 0, padL).Gap(t.Space(2)).
		AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).DragWindow().Children(func() {
		if b.switching {
			loading.Ring(c, loading.Options{Size: 12, Color: t.Accent}).Label("正在切换项目")
		}
		// Quiet field: no outline — border made the slim bar look noisy.
		ui.TextInput(c, &b.searchQuery).
			Placeholder("搜索任务…").Label("搜索任务").
			Width(t.Space(48)).Padding(t.Space(1.25), t.Space(2.5)).
			Radius(t.Radius).BorderWidth(0).
			Background(t.Surface).FontSize(fsm(t)).TextColor(t.Text)
		if strings.TrimSpace(b.searchQuery) != "" {
			if ui.Button(c, "清除").Label("清除搜索").
				Padding(t.Space(1), t.Space(1.5)).BorderWidth(0).
				TextColor(t.TextMuted).Clicked() {
				b.searchQuery = ""
			}
		}
		ui.Spacer(c)
		// 新建任务 lives here — never on column headers (sidebar "+" is 新建项目 only).
		if ui.Button(c, "").Label("新建任务").Tooltip("新建任务").
			Padding(t.Space(1), t.Space(1.5)).BorderWidth(0).
			TextColor(t.TextMuted).
			Children(func() { ui.Icon(c, taskPlus).Size(16, 16) }).
			Clicked() {
			b.openTaskForm(b.defaultTaskColumnID())
		}
		// Icon-only settings. No refresh, no sync dot — SSE stays silent in the chrome;
		// offline still uses the full-width banner below.
		if ui.Button(c, "").Label("打开设置").Tooltip("设置").
			Padding(t.Space(1), t.Space(1.5)).BorderWidth(0).
			TextColor(t.TextMuted).
			Children(func() { ui.Icon(c, settingsSliders).Size(16, 16) }).
			Clicked() {
			b.openSettings()
		}
	})
}

// projectSidebar is a quiet project switcher on L0 (window face), not a
// second column panel — columns stay the only L1 surfaces.
func (b *board) projectSidebar(c *ui.Context, t *ui.Theme) {
	// Surface (not Background): one step off the board pane so the strip
	// reads as a distinct rail. No vertical Divider — tone does the split.
	ui.Column(c).Width(b.sidebarWidth(t)).FillHeight().
		Padding(t.Space(1.5), t.Space(2), t.Space(1.5), t.Space(2)).Gap(t.Space(1.5)).
		Background(t.Surface).Label("项目列表").Children(func() {
		ui.Row(c).FillWidth().Height(t.Space(7)).AlignItems(ui.Center).Gap(t.Space(1)).Children(func() {
			ui.Text(c, "项目").FontSize(fsm(t)).FontWeight(600).TextColor(t.TextMuted).
				TextAlign(ui.Start).Grow(1)
			if ui.Button(c, "+").Label("新建项目").Tooltip("新建项目").
				Padding(t.Space(1), t.Space(1.5)).Radius(t.Radius).BorderWidth(0).
				Clicked() {
				b.openForm()
			}
		})
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).FillWidth().Gap(t.Space(1)).AlignItems(ui.Stretch).Children(func() {
				// Global entry: all projects' cards in one board (by column name).
				globalOn := b.cur == curGlobal
				gRow := ui.Button(c.Key("project-global"), "全部项目").
					FillWidth().Radius(t.Radius).
					Padding(t.Space(1.5), t.Space(2)).
					BorderWidth(0).
					FontSize(t.FontSize).FontWeight(500).TextColor(t.Text).
					TextAlign(ui.Start).Justify(ui.Start).
					Label("切换到全部项目视图")
				gFace := ui.Transparent
				gWeight := 500
				switch {
				case globalOn && gRow.Pressed():
					gFace = t.Accent.Alpha(0.18)
					gWeight = 600
				case globalOn:
					gFace = t.Accent.Alpha(0.14)
					gWeight = 600
				case gRow.Pressed():
					gFace = t.SurfacePressed
				case gRow.Hovered():
					gFace = t.SurfaceHover
				}
				gRow.Background(gFace).FontWeight(gWeight)
				if gRow.Clicked() && !globalOn {
					b.openGlobal()
				}
				for _, p := range b.projects {
					p := p
					active := b.cur != curGlobal && p.ID == b.cur
					row := ui.Button(c.Key("project-"+p.ID), p.Name).
						FillWidth().Radius(t.Radius).
						Padding(t.Space(1.5), t.Space(2)).
						BorderWidth(0).
						FontSize(t.FontSize).FontWeight(500).TextColor(t.Text).
						TextAlign(ui.Start).Justify(ui.Start).
						Label("切换到项目 " + p.Name)
					face := ui.Transparent
					weight := 500
					switch {
					case active && row.Pressed():
						face = t.Accent.Alpha(0.18)
						weight = 600
					case active:
						face = t.Accent.Alpha(0.14)
						weight = 600
					case row.Pressed():
						face = t.SurfacePressed
					case row.Hovered():
						face = t.SurfaceHover
					}
					row.Background(face).FontWeight(weight)
					if row.Clicked() && !active {
						b.openProject(p.ID)
					}
				}
			})
		})
	})
}

func (b *board) sidebarWidth(t *ui.Theme) float32 {
	return t.Space(44) // 176 when Spacing=4 — fits names without crowding the board
}



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

	// Columns split the board pane evenly, within the design's 240..400
	// range; a narrower pane scrolls instead of squeezing them, and a
	// wider one centers the board rather than leaving it hanging left.
	w, _ := c.Size()
	avail := w
	if len(b.projects) > 0 {
		// c.Size may be the window; sidebar already occupies its strip.
		avail -= b.sidebarWidth(t)
	}
	width := b.columnWidth(c, t)
	// With a sidebar, keep only a small left gutter — the sidebar is the left edge.
	padL, padR := t.Space(5), t.Space(5)
	if len(b.projects) > 0 {
		padL = t.Space(2)
	}
	boardScroll := b.columnScroll("_board")
	// Column gap is s1 only: borders are gone, so the old s2/s3 gutters
	// read as empty border leftovers between panels.
	colGap := t.Space(1)
	board := ui.ScrollHorizontal(c).Grow(1).Padding(t.Space(1), padR, t.Space(2), padL).Gap(colGap).AlignItems(ui.Stretch).
		TrackScroll(&boardScroll.state)
	b.noteScroll(boardScroll)
	if b.switching {
		board.Opacity(0.55)
	}
	used := float32(len(b.columns))*width + float32(max(len(b.columns)-1, 0))*colGap
	lead := max((avail-padL-padR-used)/2, 0)
	board.Children(func() {
		if lead > 0 {
			ui.Spacer(c).Width(lead)
		}
		for i := range b.columns {
			b.columnView(c, t, &b.columns[i], width, i)
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
// the columns, clamped to the design's 240..400.
func (b *board) columnWidth(c *ui.Context, t *ui.Theme) float32 {
	w, _ := c.Size()
	padL, padR := t.Space(5), t.Space(5)
	if len(b.projects) > 0 {
		w -= b.sidebarWidth(t)
		padL = t.Space(2)
	}
	room := w - padL - padR - float32(max(len(b.columns)-1, 0))*t.Space(1)
	if len(b.columns) == 0 {
		return 320
	}
	width := room / float32(len(b.columns))
	return min(max(width, 240), 400)
}

// columnView is one column: header and its cards. A column
// enters by rising a step, its neighbors delayed in turn, so a board
// arriving reads as one motion and a project switch replays it.
func (b *board) columnView(c *ui.Context, t *ui.Theme, col *model.Column, width float32, index int) {
	tasks := b.tasksIn(col.ID)
	panel := ui.Column(c.Key(col.ID)).Width(width).FillHeight().
		// Inner pad s2 (was s3): without column borders the old pad looked like
		// extra gutter between columns.
		Padding(t.Space(2)).Gap(t.Space(2)).Radius(t.Radius + 4).
		Background(colFace(t)).
		Role(ui.RoleGroup).
		Label(fmt.Sprintf("%s列", col.Name))
	panel.Transition(ui.ElementTransition{
		Duration: 280 * time.Millisecond,
		Ease:     ui.EaseOut,
		Enter:    &ui.Motion{Y: t.Space(3), Opacity: 0},
		Animation: &ui.Animation{Duration: 280 * time.Millisecond, Ease: ui.EaseOut,
			Delay: time.Duration(index) * 40 * time.Millisecond},
	})

	// The transient full-height preview owns placement feedback. Tracks
	// keep their geometry and color while cards make room.
	dragID, over := ui.DragOver[cardDrag](panel)
	if id, ok := ui.Drop[cardDrag](panel); ok {
		b.dropOn(string(id), col, panel)
	}

	panel.Children(func() {
		ui.Row(c).Height(t.Space(7)).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, col.Name).FontSize(fmd(t)).FontWeight(600).
				Role(ui.RoleHeading).Level(2)
			ui.Text(c, strconv.Itoa(len(tasks))).FontSize(fsm(t)).TextColor(t.TextMuted).
				Label(fmt.Sprintf("%d 个任务", len(tasks)))
			ui.Spacer(c)
		})

		// Scrollbar overlays content by default; reserve a right gutter so
		// thumbs never cover cards. Same columnView → 待办/进行中/评审/完成.
		bar := t.ScrollbarWidth
		if bar <= 0 {
			bar = 6
		}
		gutter := bar + t.Space(2)
		track := b.columnScroll(col.ID)
		ui.Scroll(c).Grow(1).TrackScroll(&track.state).Children(func() {
			list := ui.Column(c).FillWidth().Gap(t.Space(2)).Padding(0, gutter, t.Space(6), 0).
				FocusGroup(ui.Vertical).Label(col.Name + "列任务")
			list.Children(func() {
				b.columnCards(c, t, col, tasks, panel, list, string(dragID), over)
			})
		})
		b.noteScroll(track)
	})
}

// createTop creates a card and lifts it to the head of its column, which
// is where a quick add puts it.
func (b *board) createTop(columnID, title string) {
	proj := b.cur
	if proj == "" || proj == curGlobal {
		b.toast("无法创建", "请先在侧栏选一个项目，再添加任务。")
		return
	}
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

// cardView draws one card: title, summary, and its footer. The footer
// carries the card's provenance in one quiet line — version, the agent
// that wrote it last, and when — because who moved a task is this
// board's story.
func (b *board) taskCard(c *ui.Context, t *ui.Theme, task *model.Task) ui.Element {
	// No resting border: cardFace vs colFace carries the edge (theme tokens).
	card := ui.Box(c.Key(task.ID)).FillWidth().
		Padding(t.Space(3.5)).Gap(t.Space(2)).Radius(t.Radius+2).BorderWidth(0).
		Focusable().Cursor(ui.CursorPointer).Role(ui.RoleButton).
		Label("打开任务：" + task.Title).
		Drag(cardDrag(task.ID))
	// The fill animates between rest, hover, press and the accent wash a
	// card shows for a moment after another agent changed it.
	face := cardFace(t)
	if card.Hovered() {
		face = cardHover(t)
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
		card.Opacity(b.dragSourceOpacity())
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
		if m.Item("复制 ID").Chosen() {
			mygo.Clipboard.WriteText(task.ID)
			b.toast("已复制 ID", task.ID)
		}
		m.Separator()
		if m.Item("归档").Chosen() {
			b.archiveTask(task.ID)
		}
	})

	approve := false
	inReview := b.columnName(task.ColumnID) == "评审"
	card.Children(func() {
		ui.Row(c).FillWidth().Gap(t.Space(1)).AlignItems(ui.Start).Children(func() {
			ui.Column(c).Grow(1).Gap(t.Space(2)).Children(func() {
				ui.Text(c, task.Title).FontSize(t.FontSize).FontWeight(500).
					MaxLines(2).Ellipsis("…").FillWidth().Tooltip(task.Title)
				if b.cur == curGlobal {
					if name := b.projectNameByID(task.ProjectID); name != "" {
						ui.Text(c, name).FontSize(fxs(t)).TextColor(t.TextMuted).
							MaxLines(1).Ellipsis("…").Label("所属项目 " + name)
					}
				}
				// Title-only mode keeps the title alone so denser columns fit more cards.
				if !b.prefs.TitleOnlyCards {
					if summary := firstLines(task.Description, 2); summary != "" {
						ui.Text(c, summary).FontSize(fsm(t)).TextColor(t.TextMuted).
							MaxLines(2).Ellipsis("…")
					}
					ui.Row(c).Height(20).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
						if task.Assignee != "" {
							ui.Avatar(c, task.Assignee, nil).Size(18, 18)
						}
						if meta := provenance(task); meta != "" {
							ui.Text(c, meta).FontSize(fxs(t)).TextColor(t.TextMuted).
								Grow(1).MaxLines(1).Ellipsis("…").Tooltip(meta)
						}
						if n := len(task.Comments); n > 0 {
							ui.Badge(c, strconv.Itoa(n)).Label(strconv.Itoa(n) + " 条评论")
						}
						if b.savingID == task.ID && (b.detail == nil || b.detail.id != task.ID) {
							loading.LinearDots(c, loading.Options{Size: 14, Color: t.TextMuted}).Label("正在保存任务")
						}
					})
				}
			})
			// 评审 only: hover reveals a lucide check (same stroke/size as
			// the top-bar settings gear — muted, no green pill).
			if inReview && card.Hovered() {
				if ui.Button(c, "").Label("验收通过，移到完成").Tooltip("移到完成").
					Padding(t.Space(1), t.Space(1.5)).BorderWidth(0).
					TextColor(t.TextMuted).
					Children(func() { ui.Icon(c, approveCheck).Size(16, 16) }).
					Clicked() {
					approve = true
					if done := b.columnID("完成"); done != "" {
						// Pin newly completed cards to the top of「完成」.
						b.moveTask(task.ID, done, "", true)
					}
				}
			}
		})
	})
	b.bounds[task.ID] = card.Bounds()

	if card.Clicked() && !approve {
		b.openDetail(*task)
	}
	return card
}

// --- detail ---

func (b *board) detailView(c *ui.Context, t *ui.Theme) {
	d := b.detail
	open := d.open
	w, h := c.Size()
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(t.Space(130)).MaxWidth(w - t.Space(20)).Gap(t.Space(4)).Children(func() {
			ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "任务详情").FontSize(fmd(t)).FontWeight(600)
				ui.Spacer(c)
				if ui.Button(c, "关闭").Label("关闭详情").Clicked() {
					open = false
				}
			})
			ui.Scroll(c).FillWidth().MaxHeight(max(h-t.Space(56), t.Space(30))).Children(func() {
				ui.Column(c).FillWidth().Gap(t.Space(4)).Children(func() {
					if d.pushed != nil && d.dirty() {
						ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
							ui.Text(c, fmt.Sprintf("该任务已被 %s 更新到 v%d，你的编辑尚未保存。",
								agentName(d.pushBy), d.pushed.Version)).
								FontSize(fsm(t)).TextColor(t.Warning)
						})
					}

					ui.TextInput(c, &d.title).Label("任务标题").Placeholder("任务标题").
						FontSize(fmd(t)).FontWeight(500).AutoFocus()
					ui.TextArea(c, &d.body).Label("任务正文").Placeholder("添加任务描述…").MinHeight(t.Space(30))
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(3)).Children(func() {
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
						ui.Text(c, provenance(&d.snap)).FontSize(fxs(t)).TextColor(t.TextMuted).
							MaxLines(1).Ellipsis("…").Tooltip(provenance(&d.snap)).Grow(1).TextAlign(ui.End)
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
						ui.Column(c).FillWidth().Children(func() {
							ui.Column(c).FillWidth().Gap(t.Space(2)).Children(func() {
								for i := range d.comments {
									cm := &d.comments[i]
									if i > 0 {
										ui.Divider(c).BorderColor(t.Border)
									}
									ui.Column(c).Key("c-" + cm.ID).FillWidth().Gap(2).Children(func() {
										ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
											ui.Text(c, cm.Author).FontSize(fsm(t)).FontWeight(600).
												Grow(1).MaxLines(1).Ellipsis("…").Tooltip(cm.Author)
											ui.Spacer(c)
											ui.Text(c, cm.CreatedAt.Format("01-02 15:04")).FontSize(fxs(t)).TextColor(t.TextMuted)
										})
										ui.Text(c, cm.Text).FontSize(t.FontSize)
									})
								}
							})
						})
					}
				})
			})
			ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				input := ui.TextInput(c, &d.comment).Placeholder("写评论，回车发送").Label("写评论").
					Grow(1).Disabled(b.commenting)
				if b.commenting {
					loading.BouncingDots(c, loading.Options{Size: 12, Color: t.Accent}).Label("正在发送评论")
				}
				send := ui.Button(c, "发送").Label("发送评论").
					Disabled(b.commenting || strings.TrimSpace(d.comment) == "")
				if input.Submitted() || send.Clicked() {
					b.sendComment()
				}
			})

			ui.Divider(c).BorderColor(t.Border)
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
				if ui.PrimaryButton(c, "保存").Label("保存任务").Disabled(b.saving).Clicked() {
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

func (b *board) openForm() { b.form = &form{open: true, isTask: false} }

func (b *board) openTaskForm(columnID string) {
	if b.cur == "" || b.cur == curGlobal {
		b.toast("无法创建", "请先在侧栏选一个项目，再添加任务。")
		return
	}
	if columnID == "" {
		columnID = b.defaultTaskColumnID()
	}
	if columnID == "" {
		b.toast("无法创建", "当前看板没有可用的列。")
		return
	}
	b.form = &form{open: true, isTask: true, columnID: columnID}
}

// defaultTaskColumnID prefers 待办, else the first column.
func (b *board) defaultTaskColumnID() string {
	if id := b.columnID("待办"); id != "" {
		return id
	}
	if len(b.columns) > 0 {
		return b.columns[0].ID
	}
	return ""
}

func (b *board) openSettings() { b.settings = &settings{open: true} }

func (b *board) settingsView(c *ui.Context, t *ui.Theme) {
	s := b.settings
	open := s.open
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(t.Space(80)).Gap(t.Space(3)).Children(func() {
			ui.Text(c, "设置").FontSize(fmd(t)).FontWeight(600)
			if ui.Checkbox(c, &b.prefs.TitleOnlyCards, "卡片仅显示标题").
				Label("卡片仅显示标题").Changed() {
				_ = saveUIPrefs(b.prefs)
			}
			ui.Text(c, "开启后，看板卡片只保留标题，隐藏描述摘要与底部元信息。").
				FontSize(fsm(t)).TextColor(t.TextMuted)
			ui.Row(c).FillWidth().Gap(t.Space(2)).Justify(ui.End).Children(func() {
				if ui.Button(c, "关闭").Label("关闭设置").Clicked() {
					open = false
				}
			})
		})
	})
	if !open {
		b.settings = nil
		return
	}
	s.open = open
}


func (b *board) formView(c *ui.Context, t *ui.Theme) {
	f := b.form
	open := f.open
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(t.Space(80)).Gap(t.Space(3)).Children(func() {
			if f.isTask {
				ui.Text(c, "新建任务").FontSize(fmd(t)).FontWeight(600)
				colName := b.columnName(f.columnID)
				if colName != "" {
					ui.Text(c, "将添加到「"+colName+"」列 · 回车可连续添加").FontSize(fsm(t)).TextColor(t.TextMuted)
				} else {
					ui.Text(c, "回车可连续添加").FontSize(fsm(t)).TextColor(t.TextMuted)
				}
				input := ui.TextInput(c, &f.name).Label("任务标题").Placeholder("任务标题，回车继续").AutoFocus()
				ui.Row(c).FillWidth().Gap(t.Space(2)).Justify(ui.End).Children(func() {
					if ui.Button(c, "完成").Label("关闭新建任务").Clicked() {
						open = false
					}
					if ui.PrimaryButton(c, "创建").Clicked() || input.Submitted() {
						title := strings.TrimSpace(f.name)
						if title == "" {
							b.toast("无法创建", "任务标题不能为空。")
							return
						}
						// Stay open: clear the field so the next title can be typed right away.
						b.createTop(f.columnID, title)
						f.name = ""
					}
				})
			} else {
				ui.Text(c, "新建项目").FontSize(fmd(t)).FontWeight(600)
				ui.Text(c, "侧栏「+」只用于新建项目；新建任务用顶栏加号。").
					FontSize(fsm(t)).TextColor(t.TextMuted)
				ui.TextInput(c, &f.name).Label("项目名").Placeholder("项目名").AutoFocus()
				ui.Row(c).FillWidth().Gap(t.Space(2)).Justify(ui.End).Children(func() {
					if ui.Button(c, "取消").Clicked() {
						open = false
					}
					if ui.PrimaryButton(c, "创建").Clicked() {
						b.createProject()
					}
				})
			}
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
		// Take the server's copy: drop the local edits in the dialog.
		if d := b.detail; d != nil && d.id == cf.id {
			if fresh := b.task(cf.id); fresh != nil {
				d.title, d.body = fresh.Title, fresh.Description
				d.assignee, d.column = fresh.Assignee, b.columnName(fresh.ColumnID)
				d.snap, d.pushed = *fresh, nil
			}
		}
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

// provenance is a card's quiet history line: the version once the task
// has moved past v1, the agent that last wrote it when that wasn't this
// window, and always the time.
func provenance(task *model.Task) string {
	var parts []string
	if task.Version > 1 {
		parts = append(parts, "v"+strconv.Itoa(task.Version))
	}
	if task.UpdatedBy != "" && task.UpdatedBy != "local" {
		parts = append(parts, task.UpdatedBy)
	}
	if s := stamp(task.UpdatedAt); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, " · ")
}

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
