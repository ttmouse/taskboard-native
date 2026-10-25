// The board's view: top bar, columns of cards, the task detail and the
// small forms. Every color comes from the theme; every measurement from
// the theme's spacing and type scale, as docs/design.md specifies.
package main

import (
	"fmt"
	"path/filepath"
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

// colFace is the column panel's fill: none. The board used to paint every
// column with a tone of its own, which on light became a second, larger
// block behind the cards. The columns are told apart by their headers, the
// space between them and the cards' own edges, so the window face runs
// uninterrupted from the top bar down.
func colFace(*ui.Theme) ui.Color { return ui.Transparent }

// cardFace is a resting card's fill, cardHover its fill under the pointer.
// sidebarSelected is the quiet neutral wash for the active project row
// (not Accent blue — sidebar chrome stays grayscale).
func sidebarSelected(t *ui.Theme, pressed bool) ui.Color {
	if pressed {
		return t.Text.Alpha(0.14)
	}
	return t.Text.Alpha(0.09)
}

func cardFace(t *ui.Theme) ui.Color {
	if t.Dark {
		return t.Surface
	}
	return t.Background
}

// cardEdge is a resting card's outline. A card that shares the window's
// face on light has to hold its shape by its edge, which the old column
// panel used to do for it.
func cardEdge(t *ui.Theme) ui.Color { return t.Border }

// cardHover is a resting card's fill under the pointer. It must never
// equal the column's own face: with no column tone, a hover that stepped to
// SurfaceHover in both appearances would be the only block on the board,
// so the fill moves one step off the card's own face — darker on light,
// lighter on dark.
func cardHover(t *ui.Theme) ui.Color {
	if t.Dark {
		return t.SurfaceHover
	}
	return t.Surface
}

// The column panel carries neither fill nor outline: the window face runs
// behind it, and the cards hold their own edges (cardEdge). The accent
// appears when a drag hovers.

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
	// Grok Bolt–style chrome: sidebar is the left unit (traffic lights +
	// search / new; settings at foot). Board pane on the right. Drag the
	// Drag the edge grip to resize; width persists in ui-prefs.
	b.ensureSidebarWidth(t)
	// Cmd+B toggles the project sidebar (same as the panel icon chip).
	c.OnShortcut(ui.Cmd, ui.KeyB, func() { b.toggleSidebar() })
	// Cmd+N opens the create-task sheet (same as the sidebar 新建任务 row).
	// Window shortcuts wait while a modal is up, so it never fires under
	// the detail dialog, search, or a form.
	c.OnShortcut(ui.Cmd, ui.KeyN, func() { b.openTaskForm(b.defaultTaskColumnID()) })
	// Cmd+Shift+L cycles 外观 (跟随系统 → 浅色 → 深色), the same choice the
	// settings sheet's 外观 row carries, for turning it from the keyboard.
	// Like the other window shortcuts it waits while a dialog is up; the
	// sheet's own segmented control is the way to switch from inside one.
	c.OnShortcut(ui.Cmd|ui.Shift, ui.KeyL, b.cycleAppearance)
	content := func() {
		ui.Column(c).Fill().Children(func() {
			b.topBar(c, t)
			if b.status == client.Offline {
				b.offlineBanner(c, t)
			}
			b.boardArea(c, t)
		})
	}
	// Custom split: one keyed rail and one keyed board pane, so both keep
	// their identity as the rail slides. The rail's box is the animated
	// value: it opens and collapses in ~160 ms, and its content is laid out
	// at the resting width the whole time — the box clips it — so it reads
	// as a drawer sliding under the edge, never as squeezed or re-flowed
	// content. The board pane takes the widening on the same beat, and
	// b.notePaneWidth tells the columns and cards to follow it without their
	// own positional tween (which would drag them out past the pane's clip).
	split := ui.Row(c).Fill().Label("侧栏与看板")
	rail := b.railWidth(split, c, t)
	pane := b.notePaneWidth(c, rail)
	split.Children(func() {
		if rail > 0 {
			ui.Box(c).Key("project-sidebar").Width(rail).FillHeight().Clip().Children(func() {
				// Content at its resting width inside the narrower box.
				ui.Box(c).Width(b.sidebarWidth(t)).FillHeight().Children(func() {
					b.projectSidebar(c, t)
				})
			})
		}
		ui.Box(c).Key("board-pane").Width(pane).FillHeight().Children(content)
		if b.sidebarW > 0 && rail >= b.sidebarW-0.5 {
			const grip = float32(7) // hit target; paint stays 1px
			handle := ui.Box(c).Key("sidebar-resize").Absolute().
				Top(0).Bottom(0).
				Left(rail - (grip-1)/2).Width(grip).
				Cursor(ui.CursorResizeEW).
				Role(ui.RoleSplitter).Label("调整侧栏宽度")
			dx, _, held := handle.Dragged()
			if held && dx != 0 {
				w := b.sidebarW + dx
				if w < 160 {
					w = 160
				}
				if w > 360 {
					w = 360
				}
				if w != b.sidebarW {
					b.sidebarW = w
					b.prefs.SidebarWidth = w
					_ = saveUIPrefs(b.prefs)
				}
			}
			showLine := held || handle.Hovered()
			handle.Draw(func(p *ui.Painter, r ui.Rect) {
				if !showLine {
					return
				}
				line := t.Text.Alpha(0.28)
				if held {
					line = t.Text.Alpha(0.42)
				}
				x := r.X + (r.W-1)/2
				p.Fill(ui.Rect{X: x, Y: r.Y, W: 1, H: r.H}, line, 0)
			})
		}
	})
	// After TrackScroll updates offsets, set thumb visibility for this paint.
	// MyGo would otherwise keep thumbs visible for the whole hover; we only
	// show them briefly while scrolling (gutter still reserved).
	b.applyScrollbarTheme(c, theme)

	if b.detail != nil {
		b.detailView(c, t)
	}
	if b.searchOpen {
		b.searchView(c, t)
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
	// Content chrome only. Traffic lights + search/new live in the sidebar rail
	// (Grok Bolt–style), and move here when the rail is collapsed. Search opens
	// an immersive overlay (searchView), not an inline field — keep this strip
	// borderless under the content.
	bar := c.TitleBar()
	h := t.Space(10) // 40 — aligns with sidebar icon rail
	// The bar lives inside the pane, so it needs nothing extra on the left
	// while the rail is there. As the rail slides away the pane reaches the
	// window edge and the traffic lights must not land on the crumbs: pad in
	// proportion to how far the rail has actually gone, not on one frame.
	padL := t.Space(2)
	padR := max(bar.Right, t.Space(2))
	if len(b.projects) > 0 {
		if gone := 1 - b.railFraction(); gone > 0 {
			need := bar.Left + t.Space(2)
			if need < t.Space(14) {
				need = t.Space(14)
			}
			padL += (need - padL) * gone
		}
	} else {
		padL = bar.Left + t.Space(2)
		if padL < t.Space(14) {
			padL = t.Space(14)
		}
	}
	railHidden := b.prefs.SidebarCollapsed && len(b.projects) > 0
	ui.Row(c).Height(h).Padding(0, padR, 0, padL).Gap(t.Space(2)).
		AlignItems(ui.Center).BorderWidth(0).DragWindow().Children(func() {
		if railHidden {
			b.sidebarCollapseChip(c, t, false)
		}
		if len(b.projects) > 0 {
			b.topCrumbs(c, t)
		}
		if b.switching {
			loading.Ring(c, loading.Options{Size: 12, Color: t.Accent}).Label("正在切换项目")
		}
		ui.Spacer(c)
		// The rail carries 新建任务 / 搜索 / 设置; with it hidden they move
		// here so collapsing never strands an action.
		if railHidden {
			b.topBarAction(c, t, "新建任务", taskCompose, func() {
				b.openTaskForm(b.defaultTaskColumnID())
			})
			b.topBarAction(c, t, "搜索任务", searchGlass, func() { b.searchOpen = true })
			b.topBarAction(c, t, "打开设置", settingsSliders, func() { b.openSettings() })
		}
		// Empty workspace: 新建项目 stands alone, and the rail is absent by
		// definition.
		if len(b.projects) == 0 {
			if ui.PrimaryButton(c, "新建项目").Label("新建项目").Clicked() {
				b.openForm()
			}
			b.topBarAction(c, t, "打开设置", settingsSliders, func() { b.openSettings() })
		}
	})
}

// railEase is how the sidebar arrives: fast out, so the board gets its room
// back almost at once, and short enough to read as a snap rather than a slide.
const (
	railOpenFor     = 170 * time.Millisecond
	railCollapseFor = 150 * time.Millisecond
)

// railWidth is the sidebar's width for this frame, animated as a drawer: the
// box narrows while its content keeps the resting width, so the rail reads as
// sliding under the window edge rather than as squeezing.
//
// The animation is driven off the board's own state, not off side effects, so
// it is safe to ask more than once in a frame (MyGo builds a view repeatedly):
// b.railAt is when the current slide began and b.railFrom where it began.
func (b *board) railWidth(split ui.Element, c *ui.Context, t *ui.Theme) float32 {
	b.ensureSidebarWidth(t)
	if len(b.projects) == 0 {
		// No rail at all, and nothing to animate from once one arrives.
		b.railArmed, b.railNow = false, 0
		return 0
	}
	open := b.sidebarVisible()
	target := float32(0)
	if open {
		target = b.sidebarW
	}
	// The rail's first appearance lands at once: the window opens with it in
	// place, where a slide from zero would leave the sidebar absent for
	// several frames. Arm only now, so the first real toggle animates.
	if !b.railArmed {
		b.railArmed, b.railNow = true, target
		return target
	}
	now := c.Now()
	if b.railNow == target {
		return target
	}
	if b.railAt.IsZero() || b.railTo != target {
		// A new slide starts here, from whatever is on screen.
		b.railAt, b.railFrom, b.railTo = now, b.railNow, target
	}
	d := railOpenFor
	if !open {
		d = railCollapseFor
	}
	elapsed := now.Sub(b.railAt)
	if elapsed >= d {
		b.railNow = target
		return target
	}
	p := float32(elapsed) / float32(d)
	v := b.railFrom + (target-b.railFrom)*ui.EaseOut(min(max(p, 0), 1))
	if absDiff(v, target) < 0.75 {
		v = target
	}
	b.railNow = v
	return v
}

// railFraction is how much of the rail is on screen, 0 to 1, as the frame
// being built shows it: the top bar uses it to take over the traffic lights'
// room gradually rather than on one frame.
func (b *board) railFraction() float32 {
	if b.sidebarW <= 0 {
		return 0
	}
	return min(max(b.railNow/b.sidebarW, 0), 1)
}

// railKey names the rail's animated width.
type railKeyType struct{}

var railKey railKeyType

// notePaneWidth gives the board pane the width the rail leaves it, and tells
// the columns and cards whether that width is sliding: while it is, they must
// not run their own Position+Size tween, or they lag behind the pane they live
// in and show clipped at its left edge.
func (b *board) notePaneWidth(c *ui.Context, rail float32) float32 {
	w, h := c.Size()
	b.frameH = h
	pane := w - rail
	b.paneResize = b.paneW > 0 && absDiff(pane, b.paneW) > 0.5
	b.paneW = pane
	return pane
}

// absDiff is |a-b| for the layout's float measures.
func absDiff(a, b float32) float32 {
	if a < b {
		return b - a
	}
	return a - b
}

// topBarAction is one icon-only action in the content bar: the same look the
// sidebar rows carry, sized for a 40 DIP strip.
func (b *board) topBarAction(c *ui.Context, t *ui.Theme, label string, icon *ui.SVG, onClick func()) {
	btn := ui.ButtonBase(c.Key("topbar-"+label)).
		Label(label).Tooltip(label).
		Size(t.Space(7), t.Space(7)).Padding(0).Gap(0).BorderWidth(0).
		Radius(t.Radius + 2).Center().
		Children(func() { ui.Icon(c, icon).Size(16, 16) })
	ink := t.TextMuted
	face := ui.Transparent
	switch {
	case btn.Pressed():
		ink, face = t.Text, t.SurfacePressed
	case btn.Hovered():
		ink, face = t.Text, t.Text.Alpha(0.10)
	}
	btn.Background(face).TextColor(ink)
	if btn.Clicked() {
		onClick()
	}
}

// topCrumbs is the board's path, as Finder's path bar: 首页, then the board
// this window shows, whose arrow opens a switcher for every board. It sits
// at the left of the content bar, directly above the board it names.
func (b *board) topCrumbs(c *ui.Context, t *ui.Theme) {
	home := ui.ButtonBase(c.Key("crumb-home")).
		Label("首页，回到全部项目").Tooltip("全部项目").
		Padding(t.Space(1), t.Space(1.5)).Gap(t.Space(1.5)).
		Radius(t.Radius).BorderWidth(0).
		Children(func() {
			ui.Icon(c, houseHome).Size(16, 16)
			ui.Text(c, "首页").FontSize(t.FontSize).FontWeight(500)
		})
	home.Background(crumbFace(t, home)).TextColor(t.TextMuted)
	if home.Clicked() && b.cur != curGlobal {
		b.openGlobal()
	}

	ui.Icon(c, chevronRight).Size(14, 14).TextColor(t.TextMuted.Alpha(0.7))

	name := b.projectName()
	switcher := ui.ButtonBase(c.Key("crumb-board")).
		Label("切换看板，当前 "+name).Tooltip(name).
		MaxWidth(t.Space(56)).
		Padding(t.Space(1), t.Space(1.5)).Gap(t.Space(1)).
		Radius(t.Radius).BorderWidth(0)
	switcher.Background(crumbFace(t, switcher))
	switcher.Children(func() {
		ui.Text(c, name).FontSize(t.FontSize).FontWeight(600).
			TextColor(t.Text).MaxLines(1).Ellipsis("…")
		ui.Icon(c, chevronDown).Size(14, 14).TextColor(t.TextMuted)
	})
	switcher.Menu(func(m *ui.Menu) {
		if m.Item("全部项目").Checked(b.cur == curGlobal).Chosen() {
			b.openGlobal()
		}
		for _, p := range b.projects {
			if m.Item(p.Name).Checked(b.cur == p.ID).Chosen() {
				b.openProject(p.ID)
			}
		}
	})
}

// crumbFace is a breadcrumb link's fill: quiet until the pointer is on it.
func crumbFace(t *ui.Theme, e ui.Element) ui.Color {
	switch {
	case e.Pressed():
		return t.SurfacePressed
	case e.Hovered():
		return t.Text.Alpha(0.10)
	}
	return ui.Transparent
}

// sidebarCollapseChip is an Orca-style panel icon on a ButtonBase chip.
// collapsing=true shows panel-left-close; false shows panel-left-open.
func (b *board) sidebarCollapseChip(c *ui.Context, t *ui.Theme, collapsing bool) {
	chipSize := float32(28)
	icon := panelLeftOpen
	label, tip := "展开侧栏", "展开侧栏 (⌘B)"
	if collapsing {
		icon = panelLeftClose
		label, tip = "收起侧栏", "收起侧栏 (⌘B)"
	}
	chip := ui.ButtonBase(c.Key("sidebar-collapse")).
		Label(label).Tooltip(tip).
		Size(chipSize, chipSize).Padding(0).Gap(0).BorderWidth(0).
		Radius(t.Radius + 2).Center().
		Children(func() {
			ui.Icon(c, icon).Size(16, 16)
		})
	ink := t.TextMuted
	face := ui.Transparent
	switch {
	case chip.Pressed():
		ink = t.Text
		face = t.SurfacePressed
	case chip.Hovered():
		ink = t.Text
		face = t.Text.Alpha(0.10)
	}
	chip.Background(face).TextColor(ink)
	if chip.Clicked() {
		b.toggleSidebar()
	}
}

// projectSidebar is a quiet project switcher on L0 (window face), not a
// second column panel — columns stay the only L1 surfaces.
func (b *board) projectSidebar(c *ui.Context, t *ui.Theme) {
	// Grok Bolt–style rail: traffic lights only in the title strip; primary
	// actions are full-width icon+label rows (like Grok「新对话 / 搜索」).
	// No standing right border — the resize grip paints a 1px line on hover.
	ui.Column(c).Fill().FillHeight().
		Background(t.Surface).Label("项目列表").Children(func() {
		bar := c.TitleBar()
		lightsW := bar.Left
		if lightsW < t.Space(16) {
			lightsW = t.Space(16) // ~64 — room for ●●●
		}
		ui.Row(c).FillWidth().Height(t.Space(10)).
			Padding(0, t.Space(1.5), 0, 0).
			AlignItems(ui.Center).DragWindow().Children(func() {
			ui.Box(c).Width(lightsW).Height(t.Space(8))
			ui.Spacer(c)
			b.sidebarCollapseChip(c, t, true)
		})
		ui.Column(c).FillWidth().Grow(1).
			Padding(t.Space(1), t.Space(2), t.Space(1.5), t.Space(2)).Gap(t.Space(1)).
			Children(func() {
				// Primary nav: icon + label, comfortable vertical padding.
				b.sidebarNavRow(c, t, "新建任务", taskCompose, func() {
					b.openTaskForm(b.defaultTaskColumnID())
				})
				b.sidebarNavRow(c, t, "搜索", searchGlass, func() {
					b.searchOpen = true
				})
				// Align「项目」with project-row icons (rows use Padding X Space(2)).
				hdr := ui.Row(c.Key("projects-hdr")).FillWidth().Height(t.Space(7)).
					AlignItems(ui.Center).Gap(t.Space(1)).
					Padding(t.Space(1), t.Space(2))
				hdr.Children(func() {
					ui.Text(c, "项目").FontSize(fsm(t)).FontWeight(600).TextColor(t.TextMuted).
						TextAlign(ui.Start).Grow(1)
					plus := ui.Button(c.Key("projects-hdr-add"), "+").Label("新建项目").Tooltip("新建项目").
						Padding(t.Space(1), t.Space(1.5)).Radius(t.Radius).BorderWidth(0)
					showPlus := hdr.Hovered() || plus.Hovered() || plus.Pressed()
					if !showPlus {
						plus.Opacity(0).PassThrough()
					}
					if plus.Clicked() {
						b.openForm()
					}
				})
				ui.Scroll(c).Grow(1).Children(func() {
					ui.Column(c).FillWidth().Gap(t.Space(1)).AlignItems(ui.Stretch).Children(func() {
						// Global entry: all projects' cards in one board (by column name).
						// Fixed Height: project rows always reserve a 24×24 hover "+" in layout
						// (Opacity 0 still takes space); without it, 全部项目 paints shorter.
						const projectRowH = float32(30) // compact row; workspace path lives in tooltip
						const projectPlus = float32(24)
						globalOn := b.cur == curGlobal
						gRow := ui.Button(c.Key("project-global"), "").
							FillWidth().Height(projectRowH).Radius(t.Radius).
							Padding(0, t.Space(2)).
							BorderWidth(0).
							TextAlign(ui.Start).Justify(ui.Start).
							AlignItems(ui.Center).
							Label("切换到全部项目视图").
							Children(func() {
								ui.Row(c).FillWidth().Height(projectRowH).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
									ui.Icon(c, layersAll).Size(14, 14).TextColor(t.TextMuted)
									ui.Text(c, "全部项目").FontSize(t.FontSize).FontWeight(500).
										TextColor(t.Text).Grow(1).MaxLines(1).Ellipsis("…")
									// Spacer = project-row "+" footprint so label column aligns.
									ui.Box(c).Width(projectPlus).Height(projectPlus)
								})
							})
						gFace := ui.Transparent
						switch {
						case globalOn && gRow.Pressed():
							gFace = sidebarSelected(t, true)
						case globalOn:
							gFace = sidebarSelected(t, false)
						case gRow.Pressed():
							gFace = t.SurfacePressed
						case gRow.Hovered():
							gFace = t.SurfaceHover
						}
						gRow.Background(gFace)
						if gRow.Clicked() && !globalOn {
							b.openGlobal()
						}
						for _, p := range b.projects {
							p := p
							active := b.cur != curGlobal && p.ID == b.cur
							addTask := false
							rowTip := p.Name
							if wp := strings.TrimSpace(p.WorkspacePath); wp != "" {
								rowTip = p.Name + " — " + wp
							}
							row := ui.Box(c.Key("project-"+p.ID)).FillWidth().Height(projectRowH).Radius(t.Radius).
								Padding(0, t.Space(2)).BorderWidth(0).
								Cursor(ui.CursorPointer).Role(ui.RoleButton).
								Label("切换到项目 " + p.Name).Tooltip(rowTip)
							face := ui.Transparent
							switch {
							case active && row.Pressed():
								face = sidebarSelected(t, true)
							case active:
								face = sidebarSelected(t, false)
							case row.Pressed():
								face = t.SurfacePressed
							case row.Hovered():
								face = t.SurfaceHover
							}
							row.Background(face)
							row.ContextMenu(func(m *ui.Menu) {
								if m.Item("编辑项目").Chosen() {
									b.openProjectEdit(p)
								}
							})
							row.Children(func() {
								ui.Row(c).FillWidth().Height(projectRowH).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
									ui.Icon(c, folderClosed).Size(14, 14).TextColor(t.TextMuted)
									ui.Text(c, p.Name).FontSize(t.FontSize).FontWeight(500).
										TextColor(t.Text).Grow(1).MaxLines(1).Ellipsis("…")
									plus := ui.Button(c.Key("project-"+p.ID+"-add"), "").
										Label("在此项目新建任务").Tooltip("新建任务").
										Width(projectPlus).Height(projectPlus).Padding(0).BorderWidth(0).
										Radius(t.Radius).
										AlignItems(ui.Center).Justify(ui.Center).
										Children(func() { ui.Icon(c, taskPlus).Size(14, 14) })
									showPlus := row.Hovered() || plus.Hovered() || plus.Pressed()
									ink := t.TextMuted
									chip := ui.Transparent
									switch {
									case plus.Pressed():
										ink, chip = t.Text, t.SurfacePressed
									case plus.Hovered():
										ink, chip = t.Text, t.Text.Alpha(0.10)
									}
									plus.Background(chip).TextColor(ink)
									if !showPlus {
										plus.Opacity(0).PassThrough()
									}
									if plus.Clicked() {
										addTask = true
										b.openTaskFormIn(p.ID, "")
									}
								})
							})
							if row.Clicked() && !addTask && !active {
								b.openProject(p.ID)
							}
						}
					})
				})
			})
		// Settings stays at the foot of the rail as the same icon+label row.
		ui.Column(c).FillWidth().
			Padding(t.Space(1), t.Space(2), t.Space(2), t.Space(2)).Children(func() {
			b.sidebarNavRow(c, t, "设置", settingsSliders, func() {
				b.openSettings()
			})
		})
	})
}

// sidebarNavRow is a full-width Grok-style action: leading icon + label,
// with enough vertical padding that the icon is not squashed.
func (b *board) sidebarNavRow(c *ui.Context, t *ui.Theme, label string, icon *ui.SVG, onClick func()) {
	a11y := label
	if label == "设置" {
		a11y = "打开设置"
	} else if label == "搜索" {
		a11y = "搜索任务"
	}
	row := ui.Button(c.Key("nav-"+label), "").
		FillWidth().Radius(t.Radius).
		Padding(t.Space(2.5), t.Space(2)).
		BorderWidth(0).
		TextAlign(ui.Start).Justify(ui.Start).
		Label(a11y).Tooltip(label)
	face := ui.Transparent
	switch {
	case row.Pressed():
		face = t.SurfacePressed
	case row.Hovered():
		face = t.SurfaceHover
	}
	row.Background(face).TextColor(t.Text)
	row.Children(func() {
		ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon).Size(16, 16).TextColor(t.TextMuted)
			ui.Text(c, label).FontSize(t.FontSize).FontWeight(500).
				TextColor(t.Text).Grow(1).MaxLines(1).Ellipsis("…")
		})
	})
	if row.Clicked() {
		onClick()
	}
}

func (b *board) defaultSidebarWidth(t *ui.Theme) float32 {
	return t.Space(52) // 208 when Spacing=4 — room for name + hover +
}

// sidebarVisible reports whether the project rail occupies the left edge.
// Collapsed hides it outright — no narrow reopen rail — so the board takes
// the window and the top bar's panel chip (or ⌘B) brings it back.
func (b *board) sidebarVisible() bool {
	return len(b.projects) > 0 && !b.prefs.SidebarCollapsed
}

func (b *board) ensureSidebarWidth(t *ui.Theme) {
	if b.sidebarW > 0 {
		return
	}
	if b.prefs.SidebarWidth >= 160 {
		b.sidebarW = b.prefs.SidebarWidth
		return
	}
	b.sidebarW = b.defaultSidebarWidth(t)
}

// sidebarWidth is the rail's resting width in both states: it is what the
// rail's content is laid out at while the box animates, and what the board
// pane subtracts once the slide ends. Visibility is railWidth's business.
func (b *board) sidebarWidth(t *ui.Theme) float32 {
	b.ensureSidebarWidth(t)
	return b.sidebarW
}

func (b *board) toggleSidebar() {
	b.prefs.SidebarCollapsed = !b.prefs.SidebarCollapsed
	_ = saveUIPrefs(b.prefs)
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

	// Columns: horizontal (default) or vertical stack from prefs.BoardVertical.
	// The pane's width for this frame, not the resting one: while the rail
	// slides, laying the columns out for the resting width would push them
	// past the pane's right edge.
	avail := b.paneW
	padL, padR := t.Space(5), t.Space(5)
	if b.sidebarVisible() {
		padL = t.Space(2)
	}
	colGap := float32(0) // flush columns; panel pad carries residual air
	boardScroll := b.columnScroll("_board")

	if b.prefs.BoardVertical {
		// Stack status columns as full-width rows, and let each row be as
		// tall as its cards need: the board scrolls vertically to reach the
		// rest. Giving a row a share of the viewport and clipping its list
		// cut cards off part-way instead of showing them whole.
		board := ui.Scroll(c).Grow(1).Padding(t.Space(1), padR, t.Space(2), padL).Gap(t.Space(2)).
			TrackScroll(&boardScroll.state)
		b.noteScroll(boardScroll)
		if b.switching {
			board.Opacity(0.55)
		}
		board.Children(func() {
			for i := range b.columns {
				b.columnView(c, t, &b.columns[i], avail-padL-padR, i, b.verticalRowHeight(t, i))
			}
		})
		return
	}

	width := b.columnWidth(c, t)
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
			b.columnView(c, t, &b.columns[i], width, i, 0)
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
	w := b.paneW
	padL, padR := t.Space(5), t.Space(5)
	if b.sidebarVisible() {
		padL = t.Space(2)
	}
	room := w - padL - padR - float32(max(len(b.columns)-1, 0))*float32(0)
	if len(b.columns) == 0 {
		return 320
	}
	width := room / float32(len(b.columns))
	return min(max(width, 240), 400)
}

// verticalRowHeight is the floor a stacked status row gets on the board:
// one share of the viewport (min 220), so a short column still reads as a
// band of its own. The row grows past it whenever its cards need more.
func (b *board) verticalRowHeight(t *ui.Theme, index int) float32 {
	h := b.frameH
	n := max(len(b.columns), 1)
	chrome := t.Space(12) // top bar approx
	return max((h-chrome)/float32(n), 220)
}

// columnView is one column: header and its cards. A column
// enters by rising a step, its neighbors delayed in turn, so a board
// arriving reads as one motion and a project switch replays it.
func (b *board) columnView(c *ui.Context, t *ui.Theme, col *model.Column, width float32, index int, rowHeight float32) {
	tasks := b.tasksIn(col.ID)
	panel := ui.Column(c.Key(col.ID)).Width(width)
	if rowHeight > 0 {
		// A stacked row is as tall as its cards: the height stays a hint
		// for the row, never a frame the cards are cut off by.
		panel = panel.MinHeight(rowHeight)
	} else {
		panel = panel.FillHeight()
	}
	panel = panel.
		// The panel has no face of its own (colFace), so the inner pad is
		// air, not a frame: Y s2 keeps the first card off the header, X s1
		// keeps cards off the scrollbar gutter. Vertical air between the
		// panel edge and the header comes from the board's own padding.
		Padding(t.Space(2), t.Space(1)).Gap(t.Space(2)).Radius(t.Radius + 4).
		Background(colFace(t)).
		Role(ui.RoleGroup).
		Label(fmt.Sprintf("%s列", col.Name))
	// A column re-flows with the board. When the sidebar just opened or
	// collapsed, the width jump is not a transition but a new layout: the
	// column must land at once, or its tween paints it outside the pane.
	if !b.paneResize {
		panel.Transition(ui.ElementTransition{
			Duration: 280 * time.Millisecond,
			Ease:     ui.EaseOut,
			Enter:    &ui.Motion{Y: t.Space(3), Opacity: 0},
			Animation: &ui.Animation{Duration: 280 * time.Millisecond, Ease: ui.EaseOut,
				Delay: time.Duration(index) * 40 * time.Millisecond},
		})
	}

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
func (b *board) createTop(projectID, columnID, title string) {
	proj := projectID
	if proj == "" {
		proj = b.cur
	}
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
	// The column has no tone of its own, so the card's 1 DIP edge is what
	// draws it on the window (cardEdge).
	card := ui.Box(c.Key(task.ID)).FillWidth().
		Padding(t.Space(3.5)).Gap(t.Space(2)).Radius(t.Radius + 2).
		Border(1, cardEdge(t)).
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
		if m.Item("派发 DSH 执行").Chosen() {
			b.dispatchTask(task.ID)
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
		// Title row + optional ✓ in-flow (not Overlay). Overlay sat outside the
		// card's hit target, so hovering the icon dropped card.Hovered(), tore
		// the icon down, and flickered — clicks never stuck.
		// Title body. In title-only mode the ✓ must NOT sit in-flow: its 28×28
		// chip was stretching 评审 cards taller than other columns. Overlay it.
		titleOnly := b.prefs.TitleOnlyCards
		// A stacked board shows cards whole: a line of title or body wraps
		// as far as it goes instead of being cut at two lines, so a row is
		// exactly as tall as what the card actually says.
		whole := b.prefs.BoardVertical
		titleLines, bodyLines := 2, 2
		if whole {
			titleLines, bodyLines = 0, 0
		}
		ui.Row(c).FillWidth().Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
			// Title-only 评审 cards carry ✓ as a card overlay; reserve its
			// zone in the resting layout too, so hover never re-truncates
			// the line under the reader. s4 covers the chip (s1 inset + 24)
			// less the card padding the title already stops at.
			titleCol := ui.Column(c).Grow(1).Gap(t.Space(2))
			if inReview && titleOnly {
				titleCol = titleCol.MarginEnd(t.Space(4))
			}
			titleCol.Children(func() {
				title := ui.Text(c, task.Title).FontSize(t.FontSize).FontWeight(500).
					FillWidth().Tooltip(task.Title)
				if titleLines > 0 {
					title = title.MaxLines(titleLines).Ellipsis("…")
				}
				if b.cur == curGlobal {
					if name := b.projectNameByID(task.ProjectID); name != "" {
						ui.Text(c, name).FontSize(fxs(t)).TextColor(t.TextMuted).
							MaxLines(1).Ellipsis("…").Label("所属项目 " + name)
					}
				}
				// Title-only mode keeps the title alone so denser columns fit more cards.
				if !titleOnly {
					summary := firstLines(task.Description, bodyLines)
					if whole {
						summary = cardText(task.Description)
					}
					if summary != "" {
						text := ui.Text(c, summary).FontSize(fsm(t)).TextColor(t.TextMuted).FillWidth()
						if !whole {
							text = text.MaxLines(bodyLines).Ellipsis("…")
						}

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
			// Full cards: ✓ stays in-flow beside the title stack.
			if inReview && !titleOnly {
				b.reviewCheckButton(c, t, card, task, &approve)
			}
		})
		// Title-only: ✓ overlays the card corner so height matches other columns.
		if inReview && titleOnly {
			b.reviewCheckButtonOverlay(c, t, card, task, &approve)
		}
	})
	b.bounds[task.ID] = card.Bounds()

	if card.Clicked() && !approve {
		b.openDetail(*task)
	}
	return card
}

// reviewCheckButton is the in-flow ✓ chip for full (non–title-only) cards.
func (b *board) reviewCheckButton(c *ui.Context, t *ui.Theme, card ui.Element, task *model.Task, approve *bool) {
	chipSize := float32(24)
	// ButtonBase + Center: themed Button keeps horizontal padding that
	// pulls the Lucide check toward the top-left of a square chip.
	check := ui.ButtonBase(c.Key(task.ID+"-approve")).
		Label("验收通过，移到完成").
		Size(chipSize, chipSize).Padding(0).Gap(0).BorderWidth(0).
		Radius(t.Radius + 2).Center().
		Children(func() {
			// Lucide check mass sits left/high; 1px nudge for optical center.
			ui.Icon(c, approveCheck).Size(16, 16).Margin(1, 0, 0, 1)
		})
	show := card.Hovered() || check.Hovered() || check.Pressed()
	ink := t.TextMuted
	chip := ui.Transparent
	switch {
	case check.Pressed():
		ink = t.Text
		chip = t.SurfacePressed
	case check.Hovered():
		ink = t.Text
		chip = t.Text.Alpha(0.10)
	case show:
		ink = t.TextMuted
		chip = ui.Transparent
	}
	check.Background(chip).TextColor(ink)
	if !show {
		check.Opacity(0).PassThrough()
	}
	if check.Clicked() {
		*approve = true
		if done := b.columnID("完成"); done != "" {
			b.moveTask(task.ID, done, "", true)
		}
	}
}

// reviewCheckButtonOverlay pins ✓ just inside the card's right edge,
// centered on the card's middle so it lines up with the title row of
// title-only 评审 cards (in-flow was the stretch that made them taller
// than other columns).
//
// Attach's second anchor is the chip's own point: AnchorRight pins the
// chip's right edge to the card's, and Right(s1) draws it in from there.
// AnchorCenter — what LOCAL-32 first shipped — pins the chip's center to
// the card's edge, leaving half the chip hanging outside over the column
// gap, which is the defect this issue reports.
func (b *board) reviewCheckButtonOverlay(c *ui.Context, t *ui.Theme, card ui.Element, task *model.Task, approve *bool) {
	chipSize := float32(24)
	check := ui.ButtonBase(c.Key(task.ID+"-approve")).
		Label("验收通过，移到完成").
		Absolute().Attach(ui.AnchorRight, ui.AnchorRight).Right(t.Space(1)).
		Size(chipSize, chipSize).Padding(0).Gap(0).BorderWidth(0).
		Radius(t.Radius + 2).Center().
		Children(func() {
			ui.Icon(c, approveCheck).Size(16, 16).Margin(1, 0, 0, 1)
		})
	show := card.Hovered() || check.Hovered() || check.Pressed()
	ink := t.TextMuted
	chip := ui.Transparent
	switch {
	case check.Pressed():
		ink = t.Text
		chip = t.SurfacePressed
	case check.Hovered():
		ink = t.Text
		chip = t.Text.Alpha(0.10)
	case show:
		ink = t.TextMuted
		chip = ui.Transparent
	}
	check.Background(chip).TextColor(ink)
	if !show {
		check.Opacity(0).PassThrough()
	}
	if check.Clicked() {
		*approve = true
		if done := b.columnID("完成"); done != "" {
			b.moveTask(task.ID, done, "", true)
		}
	}
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

					// Title / body stay as TextInput / TextArea always (borderless,
					// transparent). Swapping Text ↔ input on hover shifted layout
					// because the widgets disagree on intrinsic padding.
					//
					// Unfilled title/body fields align directly with the sheet header.
					ui.TextInput(c, &d.title).Label("任务标题").Placeholder("任务标题").
						FillWidth().FontSize(fmd(t)).FontWeight(500).
						BorderWidth(0).Background(ui.Transparent).
						Padding(0, 0)
					d.editTitle = false

					bodyInput := ui.TextArea(c, &d.body).Label("任务正文").Placeholder("添加任务描述，可粘贴图片…").
						FillWidth().MinHeight(t.Space(30)).
						BorderWidth(0).Background(ui.Transparent).
						Padding(0, 0)
					b.imageInput(bodyInput, d, &d.body, &d.bodyImage, mygo.Clipboard.ReadImage)
					b.imagePreview(c.Key("body-preview"), &d.body, &d.bodyImage, "正文")
					d.editBody = false

					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(3)).Children(func() {
						names := b.columnNames()
						if d.column == "" {
							if t := b.task(d.id); t != nil {
								d.column = b.columnName(t.ColumnID)
							} else {
								d.column = names[0]
							}
						}
						// Keep the complete rounded border inside the scroll viewport.
						if ui.Select(c, &d.column, names).Label("移动到列").Changed() {
							if colID := b.columnID(d.column); colID != "" {
								b.moveTask(d.id, colID, "", false)
							}
						}
						ui.Text(c, provenance(&d.snap)).FontSize(fxs(t)).TextColor(t.TextMuted).
							MaxLines(1).Ellipsis("…").Tooltip(provenance(&d.snap)).Grow(1).TextAlign(ui.End)
					})

					// Comments: section label + Gap only (no hairline divider).
					ui.Row(c).FillWidth().Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "评论").FontSize(fxs(t)).FontWeight(600).TextColor(t.TextMuted)
						if n := len(d.comments); n > 0 {
							ui.Text(c, strconv.Itoa(n)).FontSize(fxs(t)).TextColor(t.TextMuted).
								Label(strconv.Itoa(n) + " 条评论")
						}
						ui.Spacer(c)
						if d.loading {
							loading.LinearDots(c, loading.Options{Size: 12, Color: t.TextMuted}).Label("正在载入评论")
						}
					})
					if len(d.comments) == 0 && !d.loading {
						ui.Text(c, "还没有评论").FontSize(fsm(t)).TextColor(t.TextMuted)
					}
					if len(d.comments) > 0 {
						ui.Column(c).FillWidth().Gap(t.Space(2)).Children(func() {
							for i := range d.comments {
								cm := &d.comments[i]
								// Align the surface with the sheet; inset its content equally.
								ui.Box(c.Key("c-"+cm.ID)).FillWidth().
									Padding(t.Space(2.5), t.Space(3)).Gap(t.Space(1.5)).
									Radius(t.Radius + 2).BorderWidth(0).
									Background(t.SurfaceHover.Alpha(0.55)).Children(func() {
									ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
										ui.Avatar(c, cm.Author, nil).Size(20, 20)
										ui.Text(c, cm.Author).FontSize(fsm(t)).FontWeight(600).
											Grow(1).MaxLines(1).Ellipsis("…").Tooltip(cm.Author)
										ui.Text(c, cm.CreatedAt.Format("01-02 15:04")).
											FontSize(fxs(t)).TextColor(t.TextMuted)
									})
									b.detailMarkdown(c, cm.Text)
								})
							}
						})
					}
				})
			})
			// The outer surface supplies equal insets; the input adds none.
			ui.Box(c).FillWidth().Padding(t.Space(2), t.Space(2.5)).Gap(t.Space(2)).
				Radius(t.Radius + 2).BorderWidth(0).Background(t.SurfaceHover.Alpha(0.4)).
				Children(func() {
					ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
						input := ui.TextArea(c, &d.comment).Placeholder("写评论或粘贴图片，回车发送").Label("写评论").
							Lines(1, 4).MinHeight(0).Grow(1).Disabled(b.commenting).BorderWidth(0).Background(ui.Transparent).
							Padding(0, 0)
						b.imageInput(input, d, &d.comment, &d.commentImage, mygo.Clipboard.ReadImage)
						input.OnTextKey(func(key ui.TextKey) bool {
							if key.Key != ui.KeyEnter || key.Mods&ui.Shift != 0 {
								return false
							}
							d.comment = key.Text()
							b.sendComment()
							key.SetText(d.comment)
							return true
						})
						if b.commenting {
							loading.BouncingDots(c, loading.Options{Size: 12, Color: t.Accent}).Label("正在发送评论")
						}
						send := ui.Button(c, "发送").Label("发送评论").
							Padding(t.Space(1.5), t.Space(2.5)).BorderWidth(0).
							Disabled(b.commenting || d.commentImage.busy() || strings.TrimSpace(d.comment) == "")
						if input.Submitted() || send.Clicked() {
							b.sendComment()
						}
					})
					ui.Scroll(c).FillWidth().MaxHeight(t.Space(40)).Children(func() {
						b.imagePreview(c.Key("comment-preview"), &d.comment, &d.commentImage, "评论")
					})
				})

			// Archive / save footer: Gap only, no hairline divider.
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
				if ui.PrimaryButton(c, "保存").Label("保存任务").Disabled(b.saving || d.bodyImage.busy() || d.commentImage.busy()).Clicked() {
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

// searchView is an immersive centered search overlay (same spirit as
// taskComposeImmersive): large input + matching tasks listed underneath.
// Esc / backdrop / 关闭 clears the query and closes.
func (b *board) searchView(c *ui.Context, t *ui.Theme) {
	open := b.searchOpen
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(t.Space(140)).Gap(t.Space(3)).
			AlignItems(ui.Center).Children(func() {
			ui.Text(c, "搜索任务").FontSize(fsm(t)).TextColor(t.TextMuted)
			shell := ui.Column(c).FillWidth().
				Padding(t.Space(4), t.Space(4)).
				Gap(t.Space(2.5)).
				Radius(t.Radius + 10).
				Background(t.Surface).
				BorderWidth(0)
			shell.Children(func() {
				ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, searchGlass).Size(20, 20).TextColor(t.TextMuted)
					ui.TextInput(c, &b.searchQuery).Label("搜索任务").
						Placeholder("搜索任务…").
						Grow(1).
						MinHeight(t.Space(12)).
						Padding(t.Space(1), t.Space(1)).
						BorderWidth(0).
						Background(ui.Transparent).
						FontSize(flg(t)).
						AutoFocus()
					if ui.Button(c, "关闭").Label("关闭搜索").
						Padding(t.Space(1), t.Space(2)).BorderWidth(0).
						TextColor(t.TextMuted).Clicked() {
						open = false
					}
				})
				hits := b.searchHits()
				if strings.TrimSpace(b.searchQuery) == "" {
					ui.Text(c, "输入标题或描述中的关键词").
						FontSize(fsm(t)).TextColor(t.TextMuted)
				} else if len(hits) == 0 {
					ui.Text(c, "没有匹配的任务").
						FontSize(fsm(t)).TextColor(t.TextMuted)
				} else {
					ui.Scroll(c).FillWidth().MaxHeight(t.Space(80)).Children(func() {
						ui.Column(c).FillWidth().Gap(t.Space(1)).Children(func() {
							for _, hit := range hits {
								hit := hit
								row := ui.Button(c.Key("search-hit-"+hit.ID), "").
									FillWidth().Radius(t.Radius).
									Padding(t.Space(2), t.Space(2.5)).
									BorderWidth(0).
									TextAlign(ui.Start).Justify(ui.Start).
									Label("打开任务 " + hit.Title)
								face := ui.Transparent
								switch {
								case row.Pressed():
									face = t.SurfacePressed
								case row.Hovered():
									face = t.SurfaceHover
								}
								row.Background(face)
								row.Children(func() {
									ui.Column(c).FillWidth().Gap(t.Space(0.5)).Children(func() {
										ui.Text(c, hit.Title).FontSize(t.FontSize).FontWeight(500).
											TextColor(t.Text).FillWidth().MaxLines(1).Ellipsis("…")
										meta := b.columnName(hit.ColumnID)
										if snip := firstLines(hit.Description, 1); snip != "" {
											if meta != "" {
												meta = meta + " · " + snip
											} else {
												meta = snip
											}
										}
										if meta != "" {
											ui.Text(c, meta).FontSize(fxs(t)).TextColor(t.TextMuted).
												FillWidth().MaxLines(1).Ellipsis("…")
										}
									})
								})
								if row.Clicked() {
									b.openDetail(hit)
									open = false
								}
							}
						})
					})
				}
			})
		})
	})
	if !open {
		b.searchQuery = ""
		b.searchOpen = false
		return
	}
	b.searchOpen = open
}

// --- forms ---

func (b *board) openForm() { b.form = &form{open: true, isTask: false} }

func (b *board) openTaskForm(columnID string) {
	b.openTaskFormIn(b.cur, columnID)
}

// openTaskFormIn opens the create-task sheet for a concrete project
// (used by the sidebar row "+" while another board may be selected).
func (b *board) openTaskFormIn(projectID, columnID string) {
	if projectID == "" || projectID == curGlobal {
		b.toast("无法创建", "请先在侧栏选一个项目，再添加任务。")
		return
	}
	if columnID == "" {
		columnID = b.projectColumnID(projectID, "待办")
	}
	if columnID == "" {
		b.toast("无法创建", "该项目没有可用的列。")
		return
	}
	b.form = &form{open: true, isTask: true, columnID: columnID, projectID: projectID}
}

// projectColumnID finds a named column on a project (falls back to first).
func (b *board) projectColumnID(projectID, name string) string {
	for _, p := range b.projects {
		if p.ID != projectID {
			continue
		}
		for _, c := range p.Columns {
			if c.Name == name || c.ID == name {
				return c.ID
			}
		}
		if len(p.Columns) > 0 {
			return p.Columns[0].ID
		}
	}
	return ""
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

// askAppearance hands an appearance to mygo, which turns the whole app:
// the frame, the window chrome and traffic lights, menus, and the standard
// controls. It is a variable because mygo's appearance needs a running app,
// and a render test builds frames without one; the test swaps it for a
// recorder.
var askAppearance = func(source mygo.ThemeSource) { mygo.Theme.SetSource(source) }

// setAppearance makes a the chrome's 外观, now and on the next launch. The
// frame follows from the next build on, since a frame asks the app's
// appearance every time, so the board, its dialogs and this sheet all turn
// on the same click.
func (b *board) setAppearance(a appearance) {
	if !a.valid() || a == b.prefs.Appearance {
		return
	}
	b.prefs.Appearance = a
	askAppearance(a.source())
	_ = saveUIPrefs(b.prefs)
	if b.frameCtx != nil {
		b.frameCtx.Invalidate()
	}
}

// cycleAppearance steps 跟随系统 → 浅色 → 深色 → 跟随系统, as ⌘⇧L does.
func (b *board) cycleAppearance() { b.setAppearance(b.prefs.Appearance.next()) }

// appearanceIndex is the appearance in appearanceChoices, for the
// segmented control, which works in indexes.
func (b *board) appearanceIndex() int {
	for i, choice := range appearanceChoices {
		if choice.value == b.prefs.Appearance {
			return i
		}
	}
	return 0
}

// settingsView is the preferences sheet. Every row says what it sets; the
// controls beside them carry the values, so no row repeats its own name
// inside the control.
func (b *board) settingsView(c *ui.Context, t *ui.Theme) {
	s := b.settings
	open := s.open
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(t.Space(80)).Gap(t.Space(3)).Children(func() {
			ui.Text(c, "设置").FontSize(fmd(t)).FontWeight(600)
			// 外观: the segmented control turns the chrome at once, and the
			// row's right-click menu offers the same three choices by name.
			row := ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "外观").FontSize(t.FontSize)
				ui.Spacer(c)
				selected := b.appearanceIndex()
				labels := make([]string, len(appearanceChoices))
				for i, choice := range appearanceChoices {
					labels[i] = choice.label
				}
				if ui.Segmented(c, &selected, labels...).
					Label("外观，当前" + b.prefs.Appearance.label()).Changed() {
					b.setAppearance(appearanceChoices[selected].value)
				}
			})
			row.ContextMenu(func(m *ui.Menu) {
				for _, choice := range appearanceChoices {
					item := m.Item(choice.label).Checked(choice.value == b.prefs.Appearance)
					if item.Chosen() {
						b.setAppearance(choice.value)
					}
				}
			})
			if ui.Checkbox(c, &b.prefs.TitleOnlyCards, "卡片仅显示标题").
				Label("卡片仅显示标题").Changed() {
				_ = saveUIPrefs(b.prefs)
			}
			if ui.Checkbox(c, &b.prefs.BoardVertical, "纵向排列状态列").
				Label("纵向排列状态列").Changed() {
				_ = saveUIPrefs(b.prefs)
			}
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
	// DialogBase so the outer card radius can be much larger than inner chips.
	ui.DialogBase(c, &open, func(back, panel ui.Element) {
		back.Background(ui.RGBA(0, 0, 0, 0.7))
		if f.isTask {
			w, _ := c.Size()
			// Chrome-free wrapper: the pill row inside carries the surface.
			panel.Padding(0).Gap(0).
				Radius(0).
				Background(ui.Transparent).
				BorderWidth(0).
				MaxWidth(w - t.Space(10))
			b.taskComposeImmersive(c, t, f, &open)
			return
		}
		panel.Padding(t.Space(5)).Gap(t.Space(3)).
			Radius(t.Space(5)).
			Background(t.Surface).
			BorderWidth(0).
			Width(t.Space(90)).
			Shadow(0, 10, 30, 0, ui.RGBA(0, 0, 0, 0.3))
		editing := f.editingProjectID != ""
		title, hint, saveLabel := "新建项目", "回车创建并关闭 · Shift+回车连续创建", "创建"
		emptyToast := "无法创建"
		if editing {
			title, hint, saveLabel, emptyToast = "编辑项目", "回车保存并关闭", "保存", "无法保存"
		}
		ui.Column(c).FillWidth().Gap(t.Space(3)).Children(func() {
			ui.Text(c, title).FontSize(fmd(t)).FontWeight(600)
			ui.Text(c, hint).
				FontSize(fsm(t)).TextColor(t.TextMuted)
			input := ui.TextInput(c, &f.name).Label("项目名").
				Placeholder("项目名").AutoFocus().
				OnTextKey(func(k ui.TextKey) bool {
					if k.Key != ui.KeyEnter {
						return false
					}
					name := strings.TrimSpace(k.Text())
					if name == "" {
						b.toast(emptyToast, "项目名不能为空。")
						return true
					}
					f.name = name
					if editing {
						k.SetText("")
						b.saveProjectEdit()
						open = false
						return true
					}
					stay := k.Mods&ui.Shift != 0
					k.SetText("")
					b.createProject(stay)
					if !stay {
						open = false
					}
					return true
				})
			ui.Column(c).FillWidth().Gap(t.Space(1)).Children(func() {
				ui.Text(c, "工作目录").FontSize(fxs(t)).FontWeight(600).TextColor(t.TextMuted)
				ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
					pathLabel := "选择本机文件夹…"
					if f.workspacePath != "" {
						pathLabel = f.workspacePath
					}
					ui.Text(c, pathLabel).FontSize(fsm(t)).TextColor(t.TextMuted).
						Grow(1).MaxLines(1).Ellipsis("…").Tooltip(f.workspacePath)
					if ui.Button(c, "浏览…").Label("选择工作目录").
						Padding(t.Space(1), t.Space(2)).BorderWidth(0).Clicked() {
						dirs, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
							Title:             "选择项目工作目录",
							ButtonLabel:       "选择",
							Directory:         true,
							CreateDirectories: true,
							DefaultPath:       f.workspacePath,
						})
						if err != nil {
							b.toast("无法选择目录", err.Error())
						} else if len(dirs) > 0 {
							f.workspacePath = dirs[0]
							if strings.TrimSpace(f.name) == "" {
								f.name = filepath.Base(dirs[0])
							}
						}
					}
					if f.workspacePath != "" {
						if ui.Button(c, "清除").Label("清除工作目录").
							Padding(t.Space(1), t.Space(1.5)).BorderWidth(0).
							TextColor(t.TextMuted).Clicked() {
							f.workspacePath = ""
						}
					}
				})
			})
			ui.Row(c).FillWidth().Gap(t.Space(2)).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Clicked() {
					open = false
				}
				if ui.PrimaryButton(c, saveLabel).Clicked() {
					if editing {
						b.saveProjectEdit()
					} else {
						b.createProject(false)
					}
					open = false
				}
			})
			_ = input
		})
	})
	if !open {
		b.form = nil
		return
	}
	f.open = open
}

// taskComposeImmersive is a single-row pill prompt: input fills the bar,
// circular ↑ button nested at the right end — Enter creates+closes,
// Shift+Enter creates and stays for the next title.
func (b *board) taskComposeImmersive(c *ui.Context, t *ui.Theme, f *form, open *bool) {
	projName := b.projectNameByID(f.projectID)
	colName := b.columnName(f.columnID)
	if colName == "" || colName == f.columnID {
		for _, p := range b.projects {
			if p.ID != f.projectID {
				continue
			}
			for _, col := range p.Columns {
				if col.ID == f.columnID {
					colName = col.Name
					break
				}
			}
		}
	}
	dest := "新任务"
	switch {
	case projName != "" && colName != "" && colName != f.columnID:
		dest = projName + " · " + colName
	case projName != "":
		dest = projName
	case colName != "" && colName != f.columnID:
		dest = colName
	}

	ui.Column(c).Width(t.Space(140)).Gap(t.Space(1.5)).
		AlignItems(ui.Center).Children(func() {
		ui.Text(c, dest).FontSize(fsm(t)).TextColor(t.Text.Alpha(0.6))
		// Single-row pill (reference style): input fills the bar, circular
		// ↑ button nested at the right end. Backdrop click / Esc closes.
		ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).
			Padding(t.Space(1.5), t.Space(3)).
			Radius(t.Space(5.5)).
			Background(t.Surface).
			BorderWidth(0).
			Shadow(0, 12, 36, 0, ui.RGBA(0, 0, 0, 0.35)).
			Children(func() {
				input := ui.TextInput(c, &f.name).Label("任务标题").
					Placeholder("写下一件要做的事…").
					Grow(1).
					BorderWidth(0).
					Background(ui.Transparent).
					FontSize(flg(t)).
					AutoFocus().
					OnTextKey(func(k ui.TextKey) bool {
						if k.Key != ui.KeyEnter {
							return false
						}
						title := strings.TrimSpace(k.Text())
						if title == "" {
							b.toast("无法创建", "任务标题不能为空。")
							return true
						}
						stay := k.Mods&ui.Shift != 0
						k.SetText("")
						f.name = ""
						// The card goes in from here, not from the submit
						// observer: clearing the field would make that one
						// read an empty title and skip the create.
						b.createTop(f.projectID, f.columnID, title)
						if !stay {
							// Plain Enter: the engine marks this key as a
							// submit right after the callback, so the build
							// below closes the sheet on it — Enter dismisses
							// the prompt instead of only clearing the field.
							b.pendingCompose = &composeSubmit{projectID: f.projectID, columnID: f.columnID, title: title}
						}
						// Shift+Enter consumes the key to stay for the next
						// title; plain Enter lets it through to the engine.
						return stay
					})
				// The engine's Enter arrives as a submit as well as through
				// the key callback, which already took the title out of the
				// field and left it in b.pendingCompose. Reading the submit
				// here closes the sheet on the frame the key landed, so Enter
				// dismisses the prompt instead of only clearing it.
				// takeComposeSubmit runs every build so a submit that never
				// arrives cannot leave a stale title behind.
				submit := b.takeComposeSubmit()
				if input.Submitted() && submit != nil {
					f.name = ""
					*open = false
				}
				send := ui.Button(c, "").Label("创建任务").
					Tooltip("回车创建并关闭 · Shift+回车继续").
					Width(t.Space(8)).Height(t.Space(8)).
					Padding(0).BorderWidth(0).Radius(t.Space(4)).
					Background(t.Text.Alpha(0.12)).
					AlignItems(ui.Center).Justify(ui.Center).
					Children(func() {
						ui.Text(c, "↑").FontSize(fmd(t)).FontWeight(600).TextColor(t.Text)
					})
				if send.Clicked() {
					title := strings.TrimSpace(f.name)
					if title == "" {
						b.toast("无法创建", "任务标题不能为空。")
					} else {
						b.createTop(f.projectID, f.columnID, title)
						f.name = ""
						*open = false
					}
				}
			})
	})
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

// cardText is a card's summary on a stacked board: every line, with the
// description's local images named rather than drawn, so the card reads as
// the whole note.
func cardText(s string) string {
	s = localImageMarkdown.ReplaceAllString(s, "[图片]")
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, " ")
}

// firstLines returns the first n non-empty lines of s.
func firstLines(s string, n int) string {
	s = localImageMarkdown.ReplaceAllString(s, "[图片]")
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
