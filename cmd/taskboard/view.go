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
	"taskboard-native/internal/executor"
	"taskboard-native/internal/model"
	"taskboard-native/internal/store"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/glass"
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
	return t.Surface
}

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
// behind it. The accent appears when a drag hovers.

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
	// Cmd+F opens task search (same as the sidebar/top-bar search action).
	// Avoid stacking it over task details; window shortcuts wait while a
	// dialog is up.
	c.OnShortcut(ui.Cmd, ui.KeyF, func() {
		if b.detail == nil {
			b.searchOpen = true
		}
	})
	// Cmd+N opens the create-task sheet (same as the sidebar 新建任务 row).
	// Window shortcuts wait while a modal is up, so it never fires under
	// the detail dialog, search, or a form.
	c.OnShortcut(ui.Cmd, ui.KeyN, func() {
		if b.detail == nil {
			b.openTaskForm(b.defaultTaskColumnID())
		}
	})
	// Cmd+, opens the settings sheet, matching the standard macOS shortcut.
	c.OnShortcut(ui.Cmd, ui.KeyComma, func() { b.openSettings() })
	// Cmd+Shift+L cycles 外观 (跟随系统 → 浅色 → 深色), the same choice the
	// settings sheet's 外观 row carries, for turning it from the keyboard.
	// Like the other window shortcuts it waits while a dialog is up; the
	// sheet's own segmented control is the way to switch from inside one.
	c.OnShortcut(ui.Cmd|ui.Shift, ui.KeyL, b.cycleAppearance)
	content := func() {
		// The Playground can take over the pane with the static reply sample;
		// production always returns false and renders as usual.
		if b.replyDemoPage(c, t) {
			return
		}
		if b.detail != nil {
			// Task details replace the board inside its pane. The project rail
			// remains available on the left, while the detail uses the full
			// height and width of the right side of the window.
			b.detailView(c, t)
			return
		}
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
			if held {
				b.sidebarResizing = true
			}
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
				}
			} else if !held && b.sidebarResizing {
				b.sidebarResizing = false
				_ = saveUIPrefs(b.prefs)
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
	// show them briefly while scrolling, over the content.
	b.applyScrollbarTheme(c, theme)

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
	if b.confirmDelete != nil {
		b.confirmDeleteView(c, t)
	}
	if b.confirmDeleteComment != nil {
		b.confirmDeleteCommentView(c, t)
	}
	c.After(60 * time.Second)
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
		ui.Text(c, appVersion).FontSize(fxs(t)).TextColor(t.Text.Alpha(0.55))
		if b.cur != "" && b.cur != curGlobal {
			b.projectRunControl(c, t)
		}
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
		b.timeFilterControl(c, t)
		b.boardDisplayControls(c, t)
	})
}

// projectRunControl is the open project's continuous handoff switch.
// Off opens the channel menu; on turns the switch off.
func (b *board) projectRunControl(c *ui.Context, t *ui.Theme) {
	label, tip, action := "自动承接", "开启后持续承接：清空待办与进行中，结束的卡自动移入等你，一次一张", "开启自动承接"
	if runtime := b.projectRunRuntime[b.cur]; runtime != "" {
		channel := runtimeLabel(runtime)
		label, tip, action = channel+" 承接中", channel+" 正在承接这个项目，点击关闭", "关闭自动承接"
	}
	btn := ui.ButtonBase(c.Key("project-run")).Label(action).Tooltip(tip).
		Padding(t.Space(1), t.Space(2)).Gap(t.Space(1)).
		Radius(t.Radius).BorderWidth(0).
		Children(func() {
			ui.Icon(c, autoRun).Size(16, 16)
			ui.Text(c, label).FontSize(t.FontSize).FontWeight(500)
		})
	ink, face := t.TextMuted, ui.Transparent
	if b.projectRunRuntime[b.cur] != "" {
		ink = t.Text
	}
	switch {
	case btn.Pressed():
		ink, face = t.Text, t.SurfacePressed
	case btn.Hovered():
		ink, face = t.Text, t.Text.Alpha(0.14)
	}
	btn.Background(face).TextColor(ink)
	if b.projectRunRuntime[b.cur] != "" {
		if btn.Clicked() {
			b.stopProjectRun()
		}
		return
	}
	btn.Menu(func(m *ui.Menu) {
		for _, runtime := range executor.Runtimes() {
			if m.Item(runtime.Label).Chosen() {
				b.startProjectRun(runtime.ID)
			}
		}
	})
}

func (b *board) startProjectRun(runtime string) {
	projectID := b.cur
	label := runtime
	for _, channel := range executor.Runtimes() {
		if channel.ID == runtime {
			label = channel.Label
		}
	}
	for _, project := range b.projects {
		if project.ID == projectID && strings.TrimSpace(project.WorkspacePath) == "" {
			b.toast("无法承接", "项目未设置工作路径，先给项目设置工作目录")
			return
		}
	}
	go func() {
		run, err := b.api.StartProjectRun(projectID, runtime)
		b.post(func() {
			if err != nil {
				b.toast("自动承接未开启", err.Error())
				return
			}
			if b.projectRunRuntime == nil {
				b.projectRunRuntime = map[string]string{}
			}
			if run.Running() {
				b.projectRunRuntime[projectID] = run.Runtime
			} else {
				delete(b.projectRunRuntime, projectID)
			}
			b.okToast("自动承接已开启", label+" 会持续处理，自动把结束的卡移入等你，一次一张")
		})
	}()
}

func (b *board) stopProjectRun() {
	projectID := b.cur
	go func() {
		_, err := b.api.StopProjectRun(projectID)
		b.post(func() {
			if err != nil {
				b.toast("自动承接未能关闭", err.Error())
				return
			}
			if b.projectRunRuntime != nil {
				delete(b.projectRunRuntime, projectID)
			}
			b.okToast("自动承接已关闭", "当前这张卡会继续")
		})
	}()
}

func (b *board) loadProjectRun(projectID string) {
	if projectID == "" || projectID == curGlobal {
		return
	}
	go func() {
		run, err := b.api.ProjectRun(projectID)
		b.post(func() {
			if b.projectRunRuntime == nil {
				b.projectRunRuntime = map[string]string{}
			}
			if err == nil && run.Running() {
				b.projectRunRuntime[projectID] = run.Runtime
			} else {
				delete(b.projectRunRuntime, projectID)
			}
		})
	}()
}

// timeFilterControl is the top-right time dimension picker: 全部时间 by
// default, plus 今天 / 昨天 / 近7天 / 近30天. The board then shows only
// tasks updated inside the window, so「今天完成了哪些」is one click away.
func (b *board) timeFilterControl(c *ui.Context, t *ui.Theme) {
	label, tip := "全部时间", "时间筛选：全部任务"
	switch b.timeFilter {
	case "today":
		label, tip = "今天", "时间筛选：今天更新的任务"
	case "yesterday":
		label, tip = "昨天", "时间筛选：昨天更新的任务"
	case "7d":
		label, tip = "近7天", "时间筛选：最近 7 天更新的任务"
	case "30d":
		label, tip = "近30天", "时间筛选：最近 30 天更新的任务"
	}
	btn := ui.MenuButton(c, label, func(m *ui.Menu) {
		if m.Item("全部时间").Checked(b.timeFilter == "").Chosen() {
			b.timeFilter = ""
		}
		if m.Item("今天").Checked(b.timeFilter == "today").Chosen() {
			b.timeFilter = "today"
		}
		if m.Item("昨天").Checked(b.timeFilter == "yesterday").Chosen() {
			b.timeFilter = "yesterday"
		}
		if m.Item("近7天").Checked(b.timeFilter == "7d").Chosen() {
			b.timeFilter = "7d"
		}
		if m.Item("近30天").Checked(b.timeFilter == "30d").Chosen() {
			b.timeFilter = "30d"
		}
	}).Label("时间筛选").Tooltip(tip).BorderWidth(0)
	face, ink := ui.Transparent, t.TextMuted
	switch {
	case btn.Pressed():
		face, ink = t.SurfacePressed, t.Text
	case btn.Hovered():
		face, ink = t.Text.Alpha(0.14), t.Text
	}
	btn.Background(face).TextColor(ink)
}

// boardDisplayControls stays at the right edge with or without a sidebar.
// Each icon depicts the current value; the tooltip names the next action.
func (b *board) boardDisplayControls(c *ui.Context, t *ui.Theme) {
	cardIcon, cardTip := cardFull, "卡片：完整内容 · 点击仅显示标题"
	if b.prefs.TitleOnlyCards {
		cardIcon, cardTip = cardTitle, "卡片：仅标题 · 点击显示完整内容"
	}
	b.boardDisplayToggle(c, t, "卡片仅显示标题", cardTip, cardIcon, &b.prefs.TitleOnlyCards)
	layoutIcon, layoutTip := columnsHorizontal, "布局：横向 · 点击纵向排列状态列"
	if b.prefs.BoardVertical {
		layoutIcon, layoutTip = columnsVertical, "布局：纵向 · 点击横向排列状态列"
	}
	b.boardDisplayToggle(c, t, "纵向排列状态列", layoutTip, layoutIcon, &b.prefs.BoardVertical)
}

func (b *board) boardDisplayToggle(c *ui.Context, t *ui.Theme, label, tip string, icon *ui.SVG, value *bool) {
	btn := ui.ButtonBase(c.Key("board-display-"+label)).
		Label(label+"，"+tip).Tooltip(tip).
		Size(t.Space(7), t.Space(7)).Padding(0).Gap(0).BorderWidth(0).
		Radius(t.Radius + 2).Center().
		Children(func() { ui.Icon(c, icon).Size(16, 16) })
	ink, face := t.TextMuted, ui.Transparent
	if *value {
		ink, face = t.Text, t.Text.Alpha(0.09)
	}
	switch {
	case btn.Pressed():
		ink, face = t.Text, t.SurfacePressed
	case btn.Hovered():
		ink, face = t.Text, t.Text.Alpha(0.14)
	}
	btn.Background(face).TextColor(ink)
	if btn.Clicked() {
		*value = !*value
		_ = saveUIPrefs(b.prefs)
	}
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
	// Resizing changes the rail and pane together on every pointer frame.
	// Retargeting the open animation here would make the divider lag behind
	// the pointer and leave the board pane briefly out of sync.
	if b.sidebarResizing {
		b.railNow, b.railAt, b.railTo = target, time.Time{}, target
		return target
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

// projectSortControl is the small sort dropdown left of 添加项目 in the
// sidebar header: 默认（服务端顺序）/ 按更新时间（最近活跃在前）/
// 按创建时间（最新在前）。
func (b *board) projectSortControl(c *ui.Context, t *ui.Theme, headerHovered bool) {
	label, tip := "排序", "项目排序：默认"
	switch b.projectSort {
	case "updated":
		label, tip = "按更新", "项目排序：按更新时间（最近活跃在前）"
	case "created":
		label, tip = "按创建", "项目排序：按创建时间（最新在前）"
	}
	control := ui.MenuButton(c, label, func(m *ui.Menu) {
		if m.Item("默认排序").Checked(b.projectSort == "").Chosen() {
			b.setProjectSort("")
		}
		if m.Item("按更新时间").Checked(b.projectSort == "updated").Chosen() {
			b.setProjectSort("updated")
		}
		if m.Item("按创建时间").Checked(b.projectSort == "created").Chosen() {
			b.setProjectSort("created")
		}
	}).Label("项目排序").Tooltip(tip).BorderWidth(0)
	if !headerHovered && !control.Hovered() && !control.Pressed() {
		control.Opacity(0).PassThrough()
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
					b.projectSortControl(c, t, hdr.Hovered())
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
						for _, p := range b.sortedProjects() {
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
								if m.Item("打开项目所在的目录").Chosen() {
									if strings.TrimSpace(p.WorkspacePath) == "" {
										b.toast("无法打开项目目录", "该项目尚未设置工作目录。")
									} else if err := openProjectDirectory(p.WorkspacePath); err != nil {
										b.toast("无法打开项目目录", err.Error())
									}
								}
								if m.Item("编辑项目").Chosen() {
									b.openProjectEdit(p)
								}
							})
							row.Children(func() {
								ui.Row(c).FillWidth().Height(projectRowH).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
									// A 8×8 dot wears the project's accent (or its
									// stable palette slot), matching the card bars
									// in 全部项目.
									ui.Box(c).Key("project-"+p.ID+"-dot").
										Size(t.Space(2), t.Space(2)).Radius(t.Space(2) / 2).
										Background(b.projectColor(p)).
										Label("项目颜色")
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
	if !b.prefs.BoardVertical {
		padL, padR = t.Space(2), t.Space(2)
	}
	colGap := float32(0) // flush columns; panel pad carries residual air
	boardScroll := b.columnScroll("_board")

	if b.prefs.BoardVertical {
		// Stack status columns as full-width rows, and let each row be as
		// tall as its cards need: the board scrolls vertically to reach the
		// rest. Giving a row a share of the viewport and clipping its list
		// cut cards off part-way instead of showing them whole.
		// The quick-add sits above the scroll so it stays put while the
		// rows move, and so an empty status row stays header-only.
		pane := ui.Column(c).Grow(1)
		if b.switching {
			pane.Opacity(0.55)
		}
		pane.Children(func() {
			b.quickAdd(c, t, padL, padR)
			board := ui.Scroll(c).Grow(1).Padding(0, padR, t.Space(2), padL).Gap(t.Space(2)).
				TrackScroll(&boardScroll.state)
			b.noteScroll(boardScroll)
			board.Children(func() {
				for i := range b.columns {
					b.columnView(c, t, &b.columns[i], avail-padL-padR, i)
				}
			})
		})
		return
	}

	width := b.columnWidth(c, t)
	board := ui.ScrollHorizontal(c).Grow(1).Padding(t.Space(1), padR, 0, padL).Gap(colGap).AlignItems(ui.Stretch).
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
	w := b.paneW
	room := w - 2*t.Space(2)
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
	panel := ui.Column(c.Key(col.ID)).Width(width)
	if !b.prefs.BoardVertical {
		panel = panel.FillHeight()
	}
	panel = panel.
		// The panel has no face of its own (colFace), so the inner pad is
		// air, not a frame: Y s2 keeps the first card off the header.
		// Air above the header comes from the board's own padding.
		Padding(t.Space(2), t.Space(1), 0, t.Space(1)).Gap(t.Space(2)).Radius(t.Radius + 4).
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
		// The stacked board folds a group from its header; the horizontal
		// board keeps the plain heading: its full-height columns do not fold.
		if b.prefs.BoardVertical {
			b.columnHeader(c, t, col, len(tasks))
		} else {
			ui.Row(c).Height(t.Space(7)).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, col.Name).FontSize(fmd(t)).FontWeight(600).
					Role(ui.RoleHeading).Level(2)
				ui.Text(c, strconv.Itoa(len(tasks))).FontSize(fsm(t)).TextColor(t.TextMuted).
					Label(fmt.Sprintf("%d 个任务", len(tasks)))
				ui.Spacer(c)
			})
		}

		// Empty stacked statuses keep their heading and drop target without
		// reserving space for an empty list or its bottom padding. A folded
		// group does the same with its cards still inside.
		if b.prefs.BoardVertical && (len(tasks) == 0 || b.prefs.CollapsedCols[col.ID]) {
			return
		}

		track := b.columnScroll(col.ID)
		// Keep cards full-width and move only the column thumb into the
		// existing gap between panels, outside the card surface.
		scroll := ui.Scroll(c).Grow(1).TrackScroll(&track.state)
		gutter := float32(0)
		if !b.prefs.BoardVertical {
			bar := t.ScrollbarWidth
			if bar <= 0 {
				bar = 6
			}
			gutter = bar + t.Space(1)
			// Extend the hit area into the gap as well as the painted thumb.
			// Matching list padding keeps the card width unchanged.
			scroll.Margin(0, -gutter, 0, 0)
		}
		scroll.Children(func() {
			// Bottom inset is intentional scroll padding: when the user
			// reaches the last card, the column still has air under it so
			// the card is not flush against the window edge.
			list := ui.Column(c).FillWidth().Gap(t.Space(2)).Padding(0, gutter, t.Space(6), 0).
				FocusGroup(ui.Vertical).Label(col.Name + "列任务")
			list.Children(func() {
				b.columnCards(c, t, col, tasks, panel, list, string(dragID), over)
			})
		})
		b.noteScroll(track)
	})
}

// columnHeader is one stacked group's header: chevron, name and count on a
// full-width button. Clicking folds the group down to this header — a folded
// 完成 keeps its count, so the row still says what it holds — or unfolds it.
// The state lives in uiPrefs, so folds survive a restart.
func (b *board) columnHeader(c *ui.Context, t *ui.Theme, col *model.Column, count int) {
	folded := b.prefs.CollapsedCols[col.ID]
	verb, icon := "折叠", chevronDown
	if folded {
		verb, icon = "展开", chevronRight
	}
	head := ui.Button(c.Key("col-head-"+col.ID), "").
		FillWidth().Height(t.Space(7)).
		Radius(t.Radius).BorderWidth(0).
		TextAlign(ui.Start).Justify(ui.Start).
		Label(verb + col.Name)
	face := ui.Transparent
	switch {
	case head.Pressed():
		face = t.SurfacePressed
	case head.Hovered():
		face = t.SurfaceHover
	}
	head.Background(face).TextColor(t.Text)
	head.Children(func() {
		ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon).Size(14, 14).TextColor(t.TextMuted)
			ui.Text(c, col.Name).FontSize(fmd(t)).FontWeight(600).
				Role(ui.RoleHeading).Level(2)
			ui.Text(c, strconv.Itoa(count)).FontSize(fsm(t)).TextColor(t.TextMuted).
				Label(fmt.Sprintf("%d 个任务", count))
			ui.Spacer(c)
		})
	})
	if head.Clicked() {
		if folded {
			delete(b.prefs.CollapsedCols, col.ID)
		} else {
			if b.prefs.CollapsedCols == nil {
				b.prefs.CollapsedCols = map[string]bool{}
			}
			b.prefs.CollapsedCols[col.ID] = true
		}
		_ = saveUIPrefs(b.prefs)
	}
}

// quickAdd is the one field at the top of the vertical list. Enter creates
// in the current project's 待办 and clears the field only after the service
// confirms it; a blank Enter does nothing, Escape clears, and 全部项目 keeps
// the draft. Horizontal boards never call this.
func (b *board) quickAdd(c *ui.Context, t *ui.Theme, padL, padR float32) {
	// A column panel pads its own content by s1 on each side, so the field
	// is inset by the same amount: it lines up with the cards below it,
	// not with the panel's edge, and never reads wider than them.
	ui.Row(c).FillWidth().
		Padding(t.Space(2), padR+t.Space(1), t.Space(2), padL+t.Space(1)).
		Children(func() {
			input := ui.TextInput(c.Key("quick-add"), &b.quickTitle).
				Grow(1).
				Placeholder("添加任务，回车创建").
				Label("在「待办」中添加任务").
				FontSize(t.FontSize).
				BorderWidth(0).
				ReadOnly(b.quickCreating).
				OnTextKey(func(k ui.TextKey) bool {
					if b.quickCreating {
						return true
					}
					if b.quickFileMentionOpen && k.Mods == 0 {
						query, _ := workspaceMentionQuery(k.Text())
						files := filterWorkspaceMentionTargets(b.quickFileMentionTargets, query, 20)
						switch k.Key {
						case ui.KeyDown:
							if len(files) > 0 {
								b.quickFileMentionIndex = (b.quickFileMentionIndex + 1) % len(files)
							}
							return true
						case ui.KeyUp:
							if len(files) > 0 {
								b.quickFileMentionIndex = (b.quickFileMentionIndex + len(files) - 1) % len(files)
							}
							return true
						case ui.KeyEnter:
							if len(files) > 0 {
								index := b.quickFileMentionIndex
								if index < 0 || index >= len(files) {
									index = 0
								}
								b.quickTitle = k.Text()
								pickWorkspaceMention(&b.quickTitle, &b.quickFileMentions, files[index].Path)
								k.SetText(b.quickTitle)
								b.quickFileMentionOpen = false
							}
							return true
						}
					}
					if k.Key == ui.KeyEscape && k.Mods == 0 {
						k.SetText("")
						return true
					}
					if k.Key != ui.KeyEnter || k.Mods != 0 {
						return false
					}
					title := strings.TrimSpace(k.Text())
					if title == "" && len(b.quickFileMentions) == 0 {
						return true
					}
					title = withWorkspaceMentions(title, b.quickFileMentions)
					b.quickCreating = true
					if !b.createTaskAndDispatch(b.cur, b.defaultTaskColumnID(), title, "", "", func(created bool) {
						b.quickCreating = false
						if created {
							b.quickTitle = ""
							b.quickFileMentions = nil
						}
					}) {
						b.quickCreating = false
					}
					return true
				})
			b.quickFileMentionMenu(c, t, input)
			if p, ok := b.projectByID(b.cur); ok {
				b.workspaceMentionChips(c, t, &b.quickFileMentions, p.WorkspacePath, "quick-add-file-chips")
			}
		})
}

// createTask creates a card at the bottom of its column.
func (b *board) createTask(projectID, columnID, title string) {
	b.createTaskAndDispatch(projectID, columnID, title, "", "", nil)
}

func (b *board) createTaskAndDispatch(projectID, columnID, title, description, runtime string, afterCreate func(bool)) bool {
	proj := projectID
	if proj == "" {
		proj = b.cur
	}
	if proj == "" || proj == curGlobal {
		b.toast("无法创建", "请先在侧栏选一个项目，再添加任务。")
		return false
	}
	if runtime != "" {
		workspace := ""
		for _, project := range b.projects {
			if project.ID == proj {
				workspace = strings.TrimSpace(project.WorkspacePath)
				break
			}
		}
		if workspace == "" {
			b.toast("无法派发", "项目未设置工作路径，先给项目设置工作目录")
			return false
		}
	}
	complete := func(created bool) {
		if afterCreate != nil {
			b.post(func() { afterCreate(created) })
		}
	}
	go func() {
		var err error
		if runtime == "" {
			_, err = b.api.CreateTask(proj, columnID, title, description, "")
		} else {
			_, err = b.api.CreateTaskAndDispatch(proj, columnID, title, description, "", runtime)
		}
		if err != nil {
			b.toast("创建失败", err.Error())
			complete(false)
			return
		}
		complete(true)
	}()
	return true
}

func herdrExecutionLabel(status string, t *ui.Theme) (string, ui.Color) {
	switch status {
	case "submitting":
		return "正在接入", t.TextMuted
	case "running":
		return "运行中", t.Accent
	case "waiting_approval":
		return "等待处理", t.Warning
	case "returned", "succeeded":
		return "执行已返回", t.TextMuted
	case "failed":
		return "执行失败", t.Danger
	case "disconnected", "unknown":
		// Short capsule stays 「连接异常」; Tooltip / a11y use herdrExecutionDetail.
		return "连接异常", t.Warning
	case "detached":
		return "已分离", t.TextMuted
	case "released":
		return "已释放", t.TextMuted
	default:
		return "", t.TextMuted
	}
}

// herdrExecutionDetail is the long-form Tooltip / accessibility text for a
// Herdr execution badge. Capsules keep the short label; disconnected must
// spell out that the write lock is still held so people do not re-dispatch.
func herdrExecutionDetail(status, label string) string {
	switch status {
	case "disconnected", "unknown":
		return "Herdr 执行状态：连接异常（写锁仍占用，暂不可重派；约 2 分钟不可达后释放）"
	case "released":
		return "Herdr 执行状态：已释放写锁，可重新派发或评论跟进"
	default:
		if label == "" {
			return ""
		}
		return "Herdr 执行状态：" + label
	}
}

func herdrConversationCreated(execution *model.Execution) bool {
	if execution == nil {
		return false
	}
	sessionID := strings.TrimSpace(execution.SessionID)
	return strings.HasPrefix(sessionID, "herdr:") && sessionID != "herdr:" &&
		execution.Status != "submitting" && execution.Detail != executor.HerdrInitialTurnPendingDetail
}

// commentFromExecutionAgent is true when the comment author is a known
// dispatch channel (Codex / Claude / Pi / DSH). Those replies get a
// ConversationLink-style Herdr bubble at the bottom, matching DSH.
func commentFromExecutionAgent(author string) bool {
	author = strings.TrimSpace(author)
	if author == "" {
		return false
	}
	for _, channel := range executor.Runtimes() {
		if author == channel.Label || strings.EqualFold(author, channel.ID) {
			return true
		}
	}
	return false
}

func herdrExecutionIcon(status string) *ui.SVG {
	switch status {
	case "submitting":
		return herdrSubmitting
	case "running":
		return herdrRunning
	case "waiting_approval":
		return herdrWaiting
	case "returned", "succeeded":
		return herdrReturned
	case "failed":
		return herdrFailed
	case "disconnected", "unknown":
		return herdrDisconnected
	case "detached":
		return herdrDetached
	case "released":
		return herdrReleased
	default:
		return nil
	}
}

// cardView draws one card: title, summary, and its footer. The footer
// carries the card's provenance in one quiet line — version, the agent
// that wrote it last, and when — because who moved a task is this
// board's story.
func (b *board) taskCard(c *ui.Context, t *ui.Theme, task *model.Task) ui.Element {
	// 全部项目 names the card's project and wears its color: the project
	// dot beside the name and the 3px left-edge bar both use projectColor.
	projectName := ""
	var projectColor ui.Color
	projectColorSet := false
	if b.cur == curGlobal {
		projectName = b.projectNameByID(task.ProjectID)
		if p, ok := b.projectByID(task.ProjectID); ok {
			projectColor = b.projectColor(p)
			projectColorSet = true
		}
	}
	card := ui.Box(c.Key(task.ID)).FillWidth().
		Padding(t.Space(3.5)).Gap(t.Space(2)).Radius(t.Radius + 2).
		BorderWidth(0).
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
	// 全部项目 paints a 3px accent bar down the card's left edge in the
	// project's color, so cards from different projects read apart at a
	// glance without the color washing the card face.
	if projectColorSet {
		card.Draw(func(p *ui.Painter, r ui.Rect) {
			bar := ui.Rect{X: r.X + 2, Y: r.Y + 2, W: 3, H: r.H - 4}
			p.Fill(bar, projectColor, 1.5)
		})
	}
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
		m.Submenu("派发任务", func(sub *ui.Menu) {
			for _, runtime := range executor.Runtimes() {
				if sub.Item(runtime.Label).Chosen() {
					b.dispatchTask(task.ID, runtime.ID)
				}
			}
		})
		if task.Execution != nil && task.Execution.Channel() == executor.RuntimeCodex && strings.TrimSpace(task.Execution.SessionID) != "" &&
			(!strings.HasPrefix(strings.TrimSpace(task.Execution.SessionID), "herdr:") || herdrConversationCreated(task.Execution)) {
			active := task.Execution.Active() && !task.Execution.Stalled()
			openLabel := "在 Ghostty 打开 Codex 对话"
			if strings.HasPrefix(strings.TrimSpace(task.Execution.SessionID), "herdr:") {
				openLabel = "在 Herdr 中定位 Codex 对话"
				// Focusing a Herdr terminal does not resume or create a session,
				// so it is safe while the original agent is active.
				active = false
			}
			if m.Item(openLabel).Disabled(active).Chosen() {
				b.openTaskExecution(task)
			}
		}
		if m.Item("复制 ID").Chosen() {
			num := store.FormatTaskNumber(task.Number)
			mygo.Clipboard.WriteText(num)
			b.okToast("已复制 ID", num)
		}
		m.Separator()
		if m.Item("归档").Chosen() {
			b.archiveTask(task.ID)
		}
	})

	approve := false
	openExecution := false
	inReview := b.columnHasRole(task.ColumnID, model.ColumnRoleReview)
	drawExecutionStatus := func() {
		if task.Execution == nil || !strings.HasPrefix(strings.TrimSpace(task.Execution.SessionID), "herdr:") {
			return
		}
		status := task.Execution.Status
		showReturned := (status != "returned" && status != "succeeded") || inReview
		label, color := herdrExecutionLabel(status, t)
		icon := herdrExecutionIcon(status)
		if showReturned && label != "" && icon != nil {
			detail := herdrExecutionDetail(status, label)
			ui.Icon(c, icon).Size(14, 14).TextColor(color).
				Label(detail).
				Tooltip(detail)
		}
	}
	drawCardFooterControls := func() {
		ui.Spacer(c)
		if herdrConversationCreated(task.Execution) {
			button := ui.ButtonBase(c.Key(task.ID+"-herdr-open")).
				Size(t.Space(6), t.Space(6)).Padding(0).Gap(0).Radius(t.Radius + 2).
				BorderWidth(0).Center().
				Label("打开 Herdr 中的 Codex 对话").Tooltip("打开 Herdr 中的 Codex 对话")
			styleCardFooterIconButton(button, card, t)
			button.Children(func() { ui.Icon(c, herdrConversation).Size(16, 16) })
			if button.Clicked() {
				openExecution = true
				b.openTaskExecution(task)
			}
		}
		if inReview {
			b.reviewCheckButton(c, t, card, task, &approve)
		}
	}
	card.Children(func() {
		// Title body. Interactive controls live in the card footer so they stay
		// together and away from the title and project name.
		titleOnly := b.prefs.TitleOnlyCards
		// A stacked board shows cards whole: a line of title or body wraps
		// as far as it goes instead of being cut at two lines, so a row is
		// exactly as tall as what the card actually says.
		whole := b.prefs.BoardVertical
		titleLines, bodyLines := 2, 2
		if whole {
			titleLines, bodyLines = 0, 0
		}
		// 全部项目 names the card's project. Full cards and the horizontal
		// board put that name under the title. A stacked title-only row
		// keeps the name on the title's own line, right after the title's
		// words, so the name does not add a second line and the row stays
		// as tall as the title.
		projectInline := titleOnly && whole && projectName != ""
		ui.Row(c).FillWidth().Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
			// Title-only 评审 cards carry ✓ as a card overlay; reserve its
			// zone in the resting layout too, so hover never re-truncates
			// the line under the reader. s4 covers the chip (s1 inset + 24)
			// less the card padding the title already stops at.
			titleCol := ui.Column(c).Grow(1).Gap(t.Space(2))
			titleCol.Children(func() {
				drawTitle := func() {
					titleText := task.Title
					if p, ok := b.projectByID(task.ProjectID); ok {
						titleText = displayWorkspaceMentions(titleText, p.WorkspacePath)
					}
					title := ui.Text(c, titleText).FontSize(t.FontSize).FontWeight(500).
						Tooltip(titleText)
					if !projectInline {
						title = title.FillWidth()
					}
					if titleLines > 0 {
						title = title.MaxLines(titleLines).Ellipsis("…")
					}
				}
				drawProject := func() {
					if projectName == "" {
						return
					}
					ui.Row(c).FillWidth().Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
						// A 6×6 dot wears the project's color so the name's
						// owner reads at a glance even in a dense global view.
						if projectColorSet {
							ui.Box(c).Key(task.ID+"-dot").
								Size(t.Space(1.5), t.Space(1.5)).Radius(t.Space(1.5) / 2).
								Background(projectColor).
								Label("所属项目 " + projectName)
						}
						ui.Text(c, projectName).FontSize(fxs(t)).TextColor(t.TextMuted).
							MaxLines(1).Ellipsis("…").Label("所属项目 " + projectName)
					})
				}
				if projectInline {
					// One paragraph: the name follows the title's words and
					// wraps with them, instead of standing on a line of
					// its own or being pushed to the row's right end.
					ui.RichText(c).FillWidth().FontSize(t.FontSize).FontWeight(500).
						Children(func() {
							ui.Text(c, task.Title).Tooltip(task.Title)
							// The name reads as its own thing to assistive
							// technology, as it did on its own line.
							ui.Text(c, "  "+projectName).FontSize(fxs(t)).FontWeight(400).
								TextColor(t.TextMuted).Role(ui.RoleGroup).
								Label("所属项目 " + projectName)
						})
				} else {
					drawTitle()
					drawProject()
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
					// Footer actions use 24px square buttons. Matching the row to
					// that control height keeps full cards as tall as title-only
					// cards when a review or execution footer is the only extra row.
					ui.Row(c).Height(t.Space(6)).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
						drawExecutionStatus()
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
						if n := len(task.Relations.SubIssues); n > 0 {
							ui.Badge(c, fmt.Sprintf("子 %d", n)).Label(fmt.Sprintf("%d 个子任务", n))
						}
						if b.savingID == task.ID && (b.detail == nil || b.detail.id != task.ID) {
							loading.LinearDots(c, loading.Options{Size: 14, Color: t.TextMuted}).Label("正在保存任务")
						}
						drawCardFooterControls()
					})
				}
			})
		})
		if titleOnly {
			hasFooter := inReview || (task.Execution != nil && strings.HasPrefix(strings.TrimSpace(task.Execution.SessionID), "herdr:"))
			if hasFooter {
				ui.Row(c).FillWidth().Height(t.Space(6)).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
					drawExecutionStatus()
					drawCardFooterControls()
				})
			}
		}
	})
	b.bounds[task.ID] = card.Bounds()

	if card.Clicked() && !approve && !openExecution {
		b.openDetail(*task)
	}
	return card
}

// styleCardFooterIconButton keeps the chat and approval actions consistent:
// both appear when their card is hovered, use the same hover/press treatment,
// and disappear together at rest.
func styleCardFooterIconButton(button, card ui.Element, t *ui.Theme) {
	show := card.Hovered() || button.Hovered() || button.Pressed()
	ink, face := t.TextMuted, ui.Transparent
	switch {
	case button.Pressed():
		ink, face = t.Text, t.SurfacePressed
	case button.Hovered():
		ink, face = t.Text, t.Text.Alpha(0.10)
	}
	button.Background(face).TextColor(ink)
	if !show {
		button.Opacity(0).PassThrough()
	}
}

func (b *board) openTaskExecution(task *model.Task) {
	project, ok := b.projectByID(task.ProjectID)
	if !ok {
		b.toast("无法打开 Codex 对话", "找不到任务所属项目")
	} else if err := openCodexSession(project.WorkspacePath, task.Execution.SessionID); err != nil {
		b.toast("无法打开 Codex 对话", err.Error())
	}
}

// reviewCheckButton is the in-flow ✓ chip for full (non–title-only) cards.
func (b *board) reviewCheckButton(c *ui.Context, t *ui.Theme, card ui.Element, task *model.Task, approve *bool) {
	chipSize := t.Space(6)
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
	styleCardFooterIconButton(check, card, t)
	if check.Clicked() {
		*approve = true
		if done := b.columnIDByRole(model.ColumnRoleDone); done != "" {
			b.moveTask(task.ID, done, "", true)
		}
	}
}

// --- detail ---

func (b *board) detailView(c *ui.Context, t *ui.Theme) {
	d := b.detail
	open := d.open
	pane := ui.Column(c).Key("task-detail").Fill().Gap(t.Space(4)).Padding(t.Space(5), t.Space(6)).
		Background(t.Background)
	pane.Children(func() {
		ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "任务详情").FontSize(fmd(t)).FontWeight(600)
			if num := store.FormatTaskNumber(d.snap.Number); num != "" {
				ui.Text(c, "#"+num).FontSize(fsm(t)).TextColor(t.TextMuted).
					Label("任务号 " + num).Tooltip("任务号 " + num)
				if ui.Button(c.Key("detail-copy-id"), "复制").Label("复制 ID "+num).
					FontSize(fxs(t)).Padding(t.Space(0.5), t.Space(1.5)).
					BorderWidth(0).Radius(t.Space(2)).
					Background(t.SurfaceHover.Alpha(0.5)).TextColor(t.TextMuted).Clicked() {
					mygo.Clipboard.WriteText(num)
					b.okToast("已复制 ID", num)
				}
			}
			ui.Spacer(c)
			if b.saving && b.savingID == d.id {
				loading.LinearDots(c, loading.Options{Size: 14, Color: t.TextMuted}).Label("正在保存任务")
			}
			if ui.Button(c, "关闭").Label("关闭详情").Clicked() {
				open = false
			}
		})
		// The content is the sheet's one flexible row: it takes what
		// the fixed header and comment composer leave, and scrolls
		// what does not fit. A height computed from the window alone
		// ignored those rows, so on a short window the sheet overflowed.
		ui.Scroll(c).FillWidth().Grow(1).Children(func() {
			// A scroll container lays its content out with an unbounded
			// main size, so every row keeps its own height and the
			// viewport scrolls it: nothing is squashed into anything
			// else when the sheet is capped by a short window.
			ui.Column(c).FillWidth().Gap(t.Space(4)).Children(func() {
				if d.pushed != nil && d.dirty() {
					ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
						ui.Text(c, fmt.Sprintf("该任务已被 %s 更新到 v%d，你的编辑尚未保存。",
							agentName(d.pushBy), d.pushed.Version)).
							FontSize(fsm(t)).TextColor(t.Warning)
					})
				}

				// Title stays directly editable. The body reads as Markdown so an
				// image appears in place of its source; clicking the body opens
				// its source editor without a separate edit control.
				// TextInput is single-line; TextArea + Lines soft-wraps long titles.
				titleInput := ui.TextArea(c, &d.title).Label("任务标题").Placeholder("任务标题").
					FillWidth().FontSize(fmd(t)).FontWeight(500).
					BorderWidth(0).Background(ui.Transparent).
					Padding(0, 0)
				titleInput.Lines(1, 12)
				// Enter should not insert hard newlines into a title; soft wrap still applies.
				titleInput.OnTextKey(func(k ui.TextKey) bool {
					return k.Key == ui.KeyEnter && k.Mods == 0
				})
				d.editTitle = false
				ui.Text(c, "任务正文").FontSize(fsm(t)).TextColor(t.TextMuted)
				if d.editBody {
					bodyInput := ui.TextArea(c, &d.body).Label("正文编辑器").Placeholder("添加任务描述，可粘贴图片…").
						FillWidth().MinHeight(detailBodyMinHeight(d.body, t)).
						BorderWidth(0).Background(ui.Transparent).
						Padding(0, 0)
					if localImageMarkdown.MatchString(d.body) {
						// TextArea defaults to three lines even when MinHeight is
						// smaller. Start image-bearing descriptions at one line and
						// let the outer detail scroll hold the full body.
						bodyInput.Lines(1, 1000)
					} else {
						bodyInput.Lines(0, 0)
					}
					// A fresh TextArea reveals its caret, placed at the end;
					// move it to the head once so a long description opens
					// showing its first line instead of its tail.
					if d.bodyTop {
						bodyInput.SetTextSelection(0, 0)
						d.bodyTop = false
					}
					bodyInput.OnTextKey(func(k ui.TextKey) bool {
						if k.Key == ui.KeyEscape && k.Mods == 0 {
							d.editBody = false
							d.bodyEditSeenFocus = false
							return true
						}
						return false
					})
					if bodyInput.Focused() {
						d.bodyEditSeenFocus = true
					}
					// Blur (and leave) returns to Markdown so tables/images render again.
					if d.bodyEditSeenFocus && !bodyInput.Focused() && !bodyInput.Hovered() {
						d.editBody = false
						d.bodyEditSeenFocus = false
					}
					b.imageInput(bodyInput, d, &d.body, &d.bodyImage, mygo.Clipboard.ReadImage)
					b.imageRemoveControls(c, &d.body, &d.bodyImage, "正文")
				} else {
					b.detailBodyView(c, d)
				}

				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(3)).Children(func() {
					names := b.columnNames()
					currentTask := b.task(d.id)
					if d.column == "" {
						if currentTask != nil {
							d.column = b.columnName(currentTask.ColumnID)
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
					if currentTask != nil && b.columnHasRole(currentTask.ColumnID, model.ColumnRoleReview) &&
						ui.PrimaryButton(c, "完成").Label("结束任务").Clicked() {
						if done := b.columnIDByRole(model.ColumnRoleDone); done != "" {
							b.moveTaskWithSuccess(d.id, done, "", true, func() {
								if b.detail == d {
									d.open = false
									b.detail = nil
								}
							})
						}
					}
					ui.Text(c, provenance(&d.snap)).FontSize(fxs(t)).TextColor(t.TextMuted).
						MaxLines(1).Ellipsis("…").Tooltip(provenance(&d.snap)).Grow(1).TextAlign(ui.End)
				})

				currentTask := b.task(d.id)
				if currentTask != nil {
					ui.Column(c).FillWidth().Gap(t.Space(1)).Children(func() {
						subs := currentTask.Relations.SubIssues
						blocks := currentTask.Relations.Blocks
						blockedBy := currentTask.Relations.BlockedBy
						relCount := len(subs) + len(blocks) + len(blockedBy)
						// Quiet fold: click the section heading (not a full-width heavy button).
						headLabel := "关系"
						if relCount > 0 {
							headLabel = fmt.Sprintf("关系 · %d", relCount)
						}
						if d.relationExpanded {
							headLabel += "  ∨"
						} else {
							headLabel += "  ›"
						}
						// Left-align like other section titles (Column Stretch was centering the label).
						ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
							head := ui.ButtonBase(c.Key("rel-section-toggle")).
								Padding(0, 0).Radius(0).BorderWidth(0).
								FontSize(fxs(t)).FontWeight(600).TextColor(t.TextMuted).
								Label(headLabel).Tooltip("展开或收起关系")
							head.Background(ui.Transparent)
							head.Children(func() {
								ui.Text(c, headLabel).FontSize(fxs(t)).FontWeight(600).TextColor(t.TextMuted)
							})
							if head.Clicked() {
								d.relationExpanded = !d.relationExpanded
								if !d.relationExpanded {
									d.relationPickKind = ""
									d.relationNewTitle = ""
								}
							}
						})
						if !d.relationExpanded {
							return
						}

						renderLinked := func(items []model.TaskRelationSummary, removeKind string, keyPrefix string) {
							for _, relation := range items {
								relation := relation
								ui.Row(c).FillWidth().Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
									if ui.Button(c, keyPrefix+"open"+relation.ID).Label(fmt.Sprintf("#%d %s", relation.Number, relation.Title)).Clicked() {
										if other := b.task(relation.ID); other != nil {
											b.openDetail(*other)
										}
									}
									version := currentTask.Version
									taskID := d.id
									relatedID := relation.ID
									if removeKind == "parent" {
										// removing a sub-issue: delete parent on the child
										taskID = relation.ID
										relatedID = d.id
										version = 1
										if child := b.task(relation.ID); child != nil {
											version = child.Version
										}
									}
									if ui.Button(c, keyPrefix+"rm"+relation.ID).Label("×").Clicked() {
										b.changeTaskRelation(taskID, version, removeKind, relatedID, true)
									}
								})
							}
						}

						renderPlus := func(kind, menuKey string) {
							// ButtonBase + Menu: only "+" shows; menu pops on click (no standing chevron).
							cands := b.relationCandidateTasks(*currentTask, kind)
							plus := ui.ButtonBase(c.Key(menuKey)).
								Padding(t.Space(0.5), t.Space(1)).Radius(t.Radius).
								BorderWidth(0).FontSize(fsm(t)).TextColor(t.TextMuted).
								Label("+").Tooltip("添加")
							plus.Children(func() {
								ui.Text(c, "+").FontSize(fsm(t)).TextColor(t.TextMuted)
							})
							plus.Menu(func(m *ui.Menu) {
								if m.Item("新建…").Chosen() {
									d.relationPickKind = "create-" + kind
									d.relationNewTitle = ""
								}
								if len(cands) > 0 {
									m.Separator()
								}
								for _, candidate := range cands {
									candidate := candidate
									label := relationTaskLabel(candidate)
									if m.Item(label).Chosen() {
										switch kind {
										case "sub":
											b.changeTaskRelation(candidate.ID, candidate.Version, "parent", d.id, false)
										case "blocks":
											b.changeTaskRelation(d.id, currentTask.Version, "blocks", candidate.ID, false)
										case "blocker":
											b.changeTaskRelation(d.id, currentTask.Version, "blocked_by", candidate.ID, false)
										}
										d.relationPickKind = ""
									}
								}
								if len(cands) == 0 {
									m.Item("没有可关联的已有任务").Disabled(true)
								}
							})
							face, ink := ui.Transparent, t.TextMuted
							switch {
							case plus.Pressed():
								face, ink = t.SurfacePressed, t.Text
							case plus.Hovered():
								face, ink = t.Text.Alpha(0.14), t.Text
							}
							plus.Background(face).TextColor(ink)
						}

						renderCreate := func(kind string) {
							if d.relationPickKind != "create-"+kind {
								return
							}
							ui.Row(c).FillWidth().Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
								ui.TextInput(c.Key("rel-new-"+kind), &d.relationNewTitle).
									Placeholder("新卡片标题…").Label("新建关联卡片").Grow(1).
									OnSubmit(func() { b.createRelatedTask(d.id, kind, d.relationNewTitle) })
								if ui.Button(c, "确认新建"+kind).Label("创建").Clicked() {
									b.createRelatedTask(d.id, kind, d.relationNewTitle)
								}
								if ui.Button(c, "取消新建"+kind).Label("取消").Clicked() {
									d.relationPickKind = ""
									d.relationNewTitle = ""
								}
							})
						}

						// 副任务 (children)
						ui.Row(c).FillWidth().Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
							ui.Text(c, "副任务").FontSize(fsm(t)).TextColor(t.TextMuted).Grow(1) // 副(assistant), not 负
							renderPlus("sub", "rel-plus-sub")
						})
						renderLinked(subs, "parent", "sub-")
						renderCreate("sub")

						// 阻塞 (this blocks others) — voice「阻塞与」→ DSH「阻塞」
						ui.Row(c).FillWidth().Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
							ui.Text(c, "阻塞与").FontSize(fsm(t)).TextColor(t.TextMuted).Grow(1)
							renderPlus("blocks", "rel-plus-blocks")
						})
						renderLinked(blocks, "blocks", "blocks-")
						renderCreate("blocks")

						// 阻塞于
						ui.Row(c).FillWidth().Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
							ui.Text(c, "阻塞于").FontSize(fsm(t)).TextColor(t.TextMuted).Grow(1)
							renderPlus("blocker", "rel-plus-blocker")
						})
						renderLinked(blockedBy, "blocked_by", "blocker-")
						renderCreate("blocker")
					})
				}

				// Automatic handoff timeline: always show the section header (even when empty).
				ui.Row(c).FillWidth().Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "自动承接").FontSize(fxs(t)).FontWeight(600).TextColor(t.TextMuted)
					if sum := handoffSummaryLine(d.handoffLog.Summary); sum != "" {
						ui.Text(c, sum).FontSize(fxs(t)).TextColor(t.TextMuted).
							MaxLines(1).Ellipsis("…").Tooltip(sum).Grow(1)
					} else {
						ui.Spacer(c)
					}
					if d.handoffLogLoading {
						loading.LinearDots(c, loading.Options{Size: 12, Color: t.TextMuted}).Label("正在载入自动承接日志")
					}
					if len(d.handoffLog.Events) > 0 {
						label := fmt.Sprintf("查看 %d 条记录", len(d.handoffLog.Events))
						if d.handoffLogExpanded {
							label = "收起记录"
						}
						toggle := ui.ButtonBase(c.Key("handoff-log-toggle")).
							Padding(t.Space(0.5), t.Space(1)).Radius(t.Radius).
							BorderWidth(0).FontSize(fxs(t)).TextColor(t.TextMuted).
							Label(label).Tooltip(label)
						face := ui.Transparent
						switch {
						case toggle.Pressed():
							face = t.SurfacePressed
						case toggle.Hovered():
							face = t.SurfaceHover
						}
						toggle.Background(face)
						toggle.Children(func() { ui.Text(c, label).FontSize(fxs(t)).TextColor(t.TextMuted) })
						if toggle.Clicked() {
							d.handoffLogExpanded = !d.handoffLogExpanded
						}
					}
				})
				if d.handoffLogFailed {
					ui.Text(c, "自动承接日志暂时无法读取").FontSize(fsm(t)).TextColor(t.TextMuted)
				} else if d.handoffLogExpanded && len(d.handoffLog.Events) > 0 {
					ui.Column(c).FillWidth().Gap(t.Space(0.5)).Children(func() {
						for _, ev := range d.handoffLog.Events {
							line := handoffEventLine(ev)
							ui.Text(c, line).FontSize(fxs(t)).TextColor(t.TextMuted).
								MaxLines(1).Ellipsis("…").Tooltip(line)
						}
					})
				}

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
					// Comments live in the outer Grow scroll (above the fixed composer).
					// A nested MaxHeight(h-Space(140)) overestimated remaining room and
					// clipped the bottom of the list under the composer — same class of
					// bug the outer sheet already fixed by using Grow instead of window math.
					ui.Column(c).FillWidth().Gap(t.Space(2)).
						Padding(0, 0, t.Space(2), 0).Children(func() {
						b.commentThreads(c, t, d)
					})
				}
			})
		})
		// A trailing @ starts the combined channel/file picker. File query
		// text remains in the draft until a result is chosen.
		if !d.mentionOpen && justTypedMention(d.comment) {
			d.comment = strings.TrimSuffix(d.comment, "@")
			d.mentionOpen = true
			d.mentionIndex = 0
			d.mentionSearchBase = d.comment
			d.mentionSearch = ""
		}
		if !d.mentionOpen {
			if query, ok := workspaceMentionQuery(d.comment); ok && query != "" {
				d.mentionOpen = true
				d.mentionIndex = 0
				d.mentionSearchBase = strings.TrimRight(d.comment[:strings.LastIndex(d.comment, "@")], " \t\n")
				d.mentionSearch = query
			}
		}
		if d.mentionOpen && strings.HasPrefix(d.comment, d.mentionSearchBase) {
			search := strings.TrimSpace(strings.TrimPrefix(d.comment, d.mentionSearchBase))
			if !strings.ContainsAny(search, " \t\n") {
				d.mentionSearch = search
			}
		}
		// Compact one-line composer uses horizontal insets only. When pasted
		// image thumbs appear above the field, use equal insets so the strip
		// is not flush against the rounded box edge.
		padY, padX := float32(0), t.Space(2.5)
		if len(d.commentImages) > 0 || d.commentImage.uploading {
			padY = t.Space(2.5)
		}
		composer := ui.Box(c).FillWidth().Padding(padY, padX).Gap(t.Space(1.5)).
			Radius(t.Radius + 2).BorderWidth(0).Background(t.SurfaceHover.Alpha(0.4)).
			Label("评论输入容器")
		b.mentionMenu(c, t, d, composer)
		composer.
			Children(func() {
				b.commentThumbStrip(c, t, d)
				d.comment, d.commentFiles = adoptWorkspaceMentionTokens(d.comment, d.commentFiles)
				// Give a one-line draft a taller row so AlignItems centers it
				// vertically; multiline drafts grow the row with their own height.
				ui.Row(c).FillWidth().Gap(t.Space(2)).MinHeight(t.Space(8)).
					AlignItems(ui.Center).Label("评论输入区").Children(func() {
					if d.commentParentID != "" {
						if ui.Button(c.Key("reply-target-chip"), "回复 @"+d.commentParentAuthor+" ×").
							Label("取消回复目标").Tooltip("正在回复 @"+d.commentParentAuthor+"；点击取消").
							FontSize(fxs(t)).Padding(t.Space(0.5), t.Space(1)).
							BorderWidth(0).Radius(t.Space(2)).
							Background(t.Accent.Alpha(0.16)).TextColor(t.Accent).Clicked() {
							d.commentParentID, d.commentParentAuthor = "", ""
						}
					}
					if d.commentMention != "" {
						if ui.Button(c.Key("mention-chip"), "@"+runtimeLabel(d.commentMention)).
							Label("取消提醒 "+runtimeLabel(d.commentMention)).
							Tooltip("发送后交给 "+runtimeLabel(d.commentMention)+"；点击取消").
							FontSize(fxs(t)).Padding(t.Space(0.5), t.Space(1)).
							BorderWidth(0).Radius(t.Space(2)).
							Background(t.Accent.Alpha(0.16)).TextColor(t.Accent).Clicked() {
							d.commentMention = ""
						}
					}
					autoFollowup := taskHasReturnedSession(d.snap)
					originalRuntime := ""
					if d.snap.Execution != nil {
						originalRuntime = d.snap.Execution.Channel()
					}
					input := ui.TextArea(c, &d.comment).Placeholder(commentPlaceholder(d.commentMention, autoFollowup, originalRuntime)).Label("写评论").
						Lines(1, 4).MinHeight(0).Grow(1).
						Disabled(b.commenting).BorderWidth(0).Background(ui.Transparent).
						Padding(0, 0)
					// Read here, as the field is built: an element cannot be
					// read again from the key handler, which runs later.
					if start, end := input.TextSelection(); start == 0 && end == 0 {
						d.commentCaretStart = true
					} else {
						d.commentCaretStart = false
					}
					if d.commentFocus > 0 {
						// Focus the frame the button vanishes, not just after: a
						// disappearing node otherwise hands the keys elsewhere.
						input.Focus()
						d.commentFocus--
					}
					b.commentImageInput(input, d, mygo.Clipboard.ReadImage)
					input.OnTextKey(func(key ui.TextKey) bool {
						if d.mentionOpen && key.Mods == 0 {
							query, hasFileQuery := commentMentionSearch(d)
							showChannels := !hasFileQuery || query == ""
							files := b.filteredWorkspaceMentions(d, query)
							count := mentionOptionCount(len(executor.Runtimes()), len(files), showChannels)
							switch key.Key {
							case ui.KeyDown:
								if count > 0 {
									d.mentionIndex = (d.mentionIndex + 1) % count
								}
								return true
							case ui.KeyUp:
								if count > 0 {
									d.mentionIndex = (d.mentionIndex + count - 1) % count
								}
								return true
							case ui.KeyEnter:
								if count > 0 {
									index := d.mentionIndex
									if index < 0 || index >= count {
										index = 0
									}
									b.selectMentionOption(d, index)
								}
								return true
							}
						}
						// The address sits before the draft, so Backspace with
						// the caret at the start deletes the address, as a
						// token in a chat composer does, and leaves the text.
						if key.Key == ui.KeyBackspace && d.commentCaretStart && d.commentMention != "" && key.Mods == 0 {
							d.commentMention = ""
							key.SetText(key.Text())
							return true
						}
						if key.Key != ui.KeyEnter || key.Mods&ui.Shift != 0 {
							return false
						}
						d.comment = key.Text()
						b.sendComment()
						key.SetText(d.comment)
						return true
					})
					if p, ok := b.projectByID(d.snap.ProjectID); ok {
						b.workspaceMentionChips(c, t, &d.commentFiles, p.WorkspacePath, "comment-file-chips")
					}
					if b.commenting {
						loading.BouncingDots(c, loading.Options{Size: 12, Color: t.Accent}).Label("正在发送评论")
					}
					if input.Submitted() {
						b.sendComment()
					}
				})
			})
	})
	if pane.OverlayShortcut(0, ui.KeyEscape) {
		// Prefer leaving an in-place body/table/comment editor over dismissing the sheet.
		if d.editTable {
			d.commitTableEdit()
		} else if d.commentBlockID != "" {
			b.commitCommentBlockEdit()
		} else if d.editBody {
			d.editBody = false
			d.bodyEditSeenFocus = false
		} else {
			open = false
		}
	}
	if b.detail != d {
		return // Saving can dismiss this detail while its view is being built.
	}
	if !open && d.dirty() {
		// Closing the detail sheet commits title/body edits. This keeps the
		// composer as the only bottom control without dropping task edits.
		d.open = true
		b.saveDetail()
		return
	}
	if !open || !d.open {
		b.detail = nil
		return
	}
	d.open = open
}

func handoffSummaryLine(sum executor.HandoffSummary) string {
	var parts []string
	if sum.Runtime != "" {
		parts = append(parts, runtimeLabel(sum.Runtime))
	}
	if sum.Outcome != "" {
		parts = append(parts, sum.Outcome)
	} else if sum.LastLabel != "" {
		parts = append(parts, sum.LastLabel)
	}
	if d := executor.FormatHandoffDuration(sum.DurationMs); d != "" {
		parts = append(parts, "耗时 "+d)
	}
	if sum.Reason != "" {
		parts = append(parts, sum.Reason)
	}
	return strings.Join(parts, " · ")
}

func handoffEventLine(ev executor.HandoffEvent) string {
	parts := []string{commentTimestamp(ev.Timestamp), executor.HandoffEventLabel(ev.Event)}
	if ev.Runtime != "" {
		parts = append(parts, runtimeLabel(ev.Runtime))
	}
	if ev.ExecutionState != "" {
		parts = append(parts, ev.ExecutionState)
	} else if ev.RunStatus != "" {
		parts = append(parts, ev.RunStatus)
	}
	if ev.Reason != "" {
		parts = append(parts, ev.Reason)
	}
	return strings.Join(parts, " · ")
}

// commentThreads renders each thread with its replies below their parent:
// threads newest first, replies chronological, so a conversation reads
// downward. A reply sits inside its parent's rail instead of being pushed
// right by indentation alone, and quotes the block it answers.
func (b *board) commentThreads(c *ui.Context, t *ui.Theme, d *detail) {
	roots, children, byID := commentTree(d.comments)
	var draw func(model.Comment, int)
	draw = func(cm model.Comment, depth int) {
		ui.Box(c.Key("c-"+cm.ID)).FillWidth().
			Padding(t.Space(2.5), t.Space(3)).Gap(t.Space(1.5)).
			Radius(t.Radius + 2).BorderWidth(0).
			Background(t.SurfaceHover.Alpha(0.55)).Children(func() {
			ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				ui.Avatar(c, cm.Author, nil).Size(20, 20)
				ui.Text(c, cm.Author).FontSize(fsm(t)).FontWeight(600).
					MaxLines(1).Ellipsis("…").Tooltip(cm.Author)
				if cm.Mention != "" {
					ui.Text(c, "@"+runtimeLabel(cm.Mention)).FontSize(fxs(t)).
						TextColor(t.Accent).MaxLines(1).Ellipsis("…").
						Tooltip("这条评论指定由 " + runtimeLabel(cm.Mention) + " 处理")
				}
				ui.Spacer(c)
				ui.Text(c, commentTimestamp(cm.CreatedAt)).
					FontSize(fxs(t)).TextColor(t.TextMuted)
				if ui.Button(c.Key("reply-comment-"+cm.ID), "回复").Label("回复评论").
					Tooltip("回复这条评论").Clicked() {
					b.beginCommentReply(cm)
				}
				if cm.Author == b.agent {
					more := ui.ButtonBase(c.Key("comment-more-"+cm.ID)).
						Label("更多评论操作").Tooltip("更多评论操作").
						Padding(t.Space(1), t.Space(1)).BorderWidth(0).Radius(t.Radius).
						Disabled(b.commenting)
					more.Menu(func(m *ui.Menu) {
						if m.Item("编辑").Chosen() {
							b.beginCommentEdit(cm)
						}
						if m.Item("删除").Chosen() {
							copy := cm
							b.confirmDeleteComment = &copy
						}
					})
					face := ui.Transparent
					if more.Pressed() {
						face = t.SurfacePressed
					} else if more.Hovered() {
						face = t.SurfaceHover
					}
					more.Background(face).Children(func() {
						ui.Icon(c, commentMore).Size(14, 14).TextColor(t.TextMuted)
					})
				}
			})
			if cm.ParentCommentID != "" {
				parent, ok := byID[cm.ParentCommentID]
				commentQuote(c, t, "reply-context-"+cm.ID, parent.Author, parent.Text, ok)
			}
			if d.commentEditID == cm.ID {
				ui.TextArea(c, &d.commentEditText).Label("编辑评论内容").FillWidth().MinHeight(t.Space(12))
				ui.Row(c).Gap(t.Space(2)).Children(func() {
					if ui.Button(c.Key("save-comment-"+cm.ID), "保存").Disabled(b.commenting).Clicked() {
						b.saveCommentEdit()
					}
					if ui.Button(c.Key("cancel-comment-"+cm.ID), "取消").Disabled(b.commenting).Clicked() {
						d.commentEditID, d.commentEditText = "", ""
				}
				})
			} else {
				b.commentBodyView(c, d, cm)
			}
			// Per-reply ConversationLink (DSH pattern): Herdr bubble under
			// AI replies, not in the detail title bar.
			if task := b.task(d.id); task != nil && commentFromExecutionAgent(cm.Author) && herdrConversationCreated(task.Execution) {
				ui.Row(c).FillWidth().Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
					button := ui.ButtonBase(c.Key("comment-herdr-open-"+cm.ID)).
						Size(t.Space(6), t.Space(6)).Padding(0).Gap(0).Radius(t.Radius + 2).
						BorderWidth(0).Center().
						Label("在 Herdr 中打开此回复的对话").Tooltip("在 Herdr 中打开此回复的对话")
					face := ui.Transparent
					if button.Pressed() {
						face = t.SurfacePressed
					} else if button.Hovered() {
						face = t.Text.Alpha(0.10)
					}
					button.Background(face).Children(func() {
						ui.Icon(c, herdrConversation).Size(16, 16)
					})
					if button.Clicked() {
						b.openTaskExecution(task)
					}
				})
			}
			if cm.Author == b.agent && cm.Mention == "" {
				// Hug content width: bare Button in a Stretch column fills the row.
				ui.Row(c).Gap(t.Space(2)).Children(func() {
					if ui.Button(c.Key("continue-"+cm.ID), "让 AI 跟进").Label("跟进评论：" + cm.ID).
						Tooltip("把这条评论交回任务的原始 AI 会话").Disabled(b.commenting).Clicked() {
						b.continueComment(d.id, cm.ID)
					}
				})
			}
		})
		// Replies hang off their parent inside a rail, in store order, so the
		// thread reads top-down instead of jumping between reversed runs.
		// Past two levels the rail narrows the card to unreadable width, so
		// deeper replies keep the quote but drop the extra nesting.
		replies := children[cm.ID]
		if len(replies) == 0 {
			return
		}
		if depth >= 2 {
			for _, reply := range replies {
				draw(reply, depth+1)
			}
			return
		}
		ui.Row(c.Key("thread-" + cm.ID)).FillWidth().Gap(t.Space(2)).Children(func() {
			ui.Column(c).Width(2).FillHeight().Radius(1).MarginStart(t.Space(2.5)).
				Background(t.Border)
			ui.Column(c).Grow(1).Gap(t.Space(2)).Children(func() {
				for _, reply := range replies {
					draw(reply, depth+1)
				}
			})
		})
	}
	for i := len(roots) - 1; i >= 0; i-- {
		draw(roots[i], 0)
	}
}

func (b *board) searchView(c *ui.Context, t *ui.Theme) {
	open := b.searchOpen
	ui.DialogBase(c, &open, func(back, panel ui.Element) {
		w, h := c.Size()
		textInset := t.Space(4) + 18 + t.Space(2.5)
		back.Background(ui.RGBA(0, 0, 0, 0.4))
		// DialogBase supplies focus trapping and dismissal without Modal's
		// extra background and padding around the search surface.
		panel.Width(t.Space(140)).MaxWidth(w-t.Space(10)).
			Padding(0).Gap(0).Radius(t.Space(4)).
			Background(t.Surface).BorderWidth(0).
			Shadow(0, 12, 36, 0, ui.RGBA(0, 0, 0, 0.24)).Label("任务搜索面板")
		ui.Column(c).FillWidth().Gap(0).Children(func() {
			ui.Row(c).FillWidth().Gap(t.Space(2.5)).Padding(t.Space(3), t.Space(4)).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, searchGlass).Size(18, 18).TextColor(t.TextMuted)
				ui.TextInput(c, &b.searchQuery).Label("搜索任务").
					Placeholder("搜索任务…").
					Grow(1).
					MinHeight(t.Space(9)).
					Padding(t.Space(1), 0).
					BorderWidth(0).
					Background(ui.Transparent).
					FontSize(t.FontSize + 5).
					AutoFocus().
					OnTextKey(func(key ui.TextKey) bool {
						if key.Mods != 0 {
							return false
						}
						hits := b.searchHits()
						switch key.Key {
						case ui.KeyDown, ui.KeyUp:
							if len(hits) == 0 {
								return true
							}
							if b.searchSelectionQuery != b.searchQuery {
								b.searchSelectionQuery = b.searchQuery
								b.searchSelection = -1
							}
							if key.Key == ui.KeyDown {
								b.searchSelection = (b.searchSelection + 1) % len(hits)
							} else if b.searchSelection < 0 {
								b.searchSelection = len(hits) - 1
							} else {
								b.searchSelection = (b.searchSelection + len(hits) - 1) % len(hits)
							}
							return true
						case ui.KeyEnter:
							if b.searchSelection >= 0 && b.searchSelection < len(hits) {
								b.openDetail(hits[b.searchSelection])
								open = false
								b.searchOpen = false
								b.searchQuery = ""
								b.searchSelection = -1
								b.searchSelectionQuery = ""
								return true
							}
						}
						return false
					})
				close := ui.Button(c, "关闭").Label("关闭搜索").
					MinHeight(t.Space(8)).Padding(t.Space(1), t.Space(2)).
					Radius(t.Space(2)).BorderWidth(0).
					FontSize(fxs(t)).TextColor(t.TextMuted)
				closeFace := ui.Transparent
				switch {
				case close.Pressed():
					closeFace = t.SurfacePressed
				case close.Hovered():
					closeFace = t.SurfaceHover
				}
				close.Background(closeFace)
				if close.Clicked() {
					open = false
				}
			})
			ui.Divider(c).FillWidth().Background(t.Border.Alpha(0.5))
			hits := b.searchHits()
			if b.searchSelectionQuery != b.searchQuery {
				b.searchSelectionQuery = b.searchQuery
				b.searchSelection = -1
			}
			if b.searchSelection >= len(hits) {
				b.searchSelection = -1
			}
			if len(hits) == 0 || strings.TrimSpace(b.searchQuery) == "" {
				message := "没有匹配的任务"
				if strings.TrimSpace(b.searchQuery) == "" {
					message = "输入标题或描述中的关键词"
				}
				ui.Text(c, message).
					FillWidth().Padding(t.Space(5), t.Space(4), t.Space(5), textInset).
					FontSize(fsm(t)).TextColor(t.TextMuted)
			} else {
				ui.Scroll(c).FillWidth().MaxHeight(min(t.Space(80), h-t.Space(40))).Children(func() {
					ui.Column(c).FillWidth().Padding(t.Space(2)).Gap(t.Space(0.5)).Children(func() {
						for i, hit := range hits {
							hit := hit
							// Align result text with the input, leaving its icon gutter clear.
							row := ui.Button(c.Key("search-hit-"+hit.ID), "").
								FillWidth().Radius(t.Radius).
								Padding(t.Space(2.5), t.Space(3), t.Space(2.5), textInset-t.Space(2)).
								BorderWidth(0).
								TextAlign(ui.Start).Justify(ui.Start).
								Label("打开任务 " + hit.Title)
							face := ui.Transparent
							switch {
							case row.Pressed():
								face = t.SurfacePressed
							case i == b.searchSelection:
								face = t.Selection
							case row.Hovered():
								face = t.SurfaceHover
							}
							row.Background(face)
							if i == b.searchSelection {
								row.ScrollIntoView()
							}
							row.Children(func() {
								ui.Column(c).FillWidth().Gap(t.Space(0.5)).Children(func() {
									ui.Text(c, hit.Title).FontSize(t.FontSize + 1).FontWeight(500).
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
	if !open {
		b.searchQuery = ""
		b.searchSelection = -1
		b.searchSelectionQuery = ""
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
		projectID = b.prefs.LastTaskProjectID
		if projectID == "" || b.projectColumnIDByRole(projectID, model.ColumnRoleIntake) == "" {
			b.toast("无法创建", "请先在侧栏选一个项目，再添加任务。")
			return
		}
		columnID = ""
	}
	if columnID == "" {
		columnID = b.projectColumnIDByRole(projectID, model.ColumnRoleIntake)
	}
	if columnID == "" {
		b.toast("无法创建", "该项目没有可用的列。")
		return
	}
	b.form = &form{open: true, isTask: true, columnID: columnID, projectID: projectID}
	b.prefs.LastTaskProjectID = projectID
	_ = saveUIPrefs(b.prefs)
	// Surfacing is the moment worth hearing: sheet and cue arrive together.
	// Every entry point (⌘N, the sidebar 新建任务 row, a column "+") lands
	// here, so the cue tracks the sheet rather than the keystroke that
	// opened it; the rejected opens above return before any sound.
	playOpenCue()
}

// projectColumnIDByRole finds the column playing role on a project (falls
// back to that project's first column, as the old name lookup did).
func (b *board) projectColumnIDByRole(projectID, role string) string {
	for _, p := range b.projects {
		if p.ID != projectID {
			continue
		}
		if c, ok := p.ColumnByRole(role); ok {
			return c.ID
		}
		if len(p.Columns) > 0 {
			return p.Columns[0].ID
		}
	}
	return ""
}

// defaultTaskColumnID prefers the intake column, else the first column.
func (b *board) defaultTaskColumnID() string {
	if id := b.columnIDByRole(model.ColumnRoleIntake); id != "" {
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

// applyDisplayName saves the 显示名 the settings field carries and hands the
// new identity to the service client, so writes land under it from the next
// request on and the board stops reading its own writes as another agent's.
// A blank name falls back to gui.
func (b *board) applyDisplayName() {
	b.agent = b.prefs.windowAgent()
	if s, ok := b.api.(agentSetter); ok {
		s.SetAgent(b.agent)
	}
	_ = saveUIPrefs(b.prefs)
}

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
			// 显示名: the identity every write carries, and what a card's
			// last-writer line shows. The field is the value, so a blank one
			// falls back to gui rather than writing as nobody.
			ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "显示名").FontSize(t.FontSize)
				ui.Spacer(c)
				if ui.TextInput(c, &b.prefs.DisplayName).
					Label("显示身份名").
					Placeholder("未设置时为 " + defaultAgent).
					Width(t.Space(40)).
					Changed() {
					b.applyDisplayName()
				}
			})
			// 全局新建热键: when on, ⌘⇧N opens the create-task sheet even
			// while the app sits in the background (Carbon global shortcut,
			// no accessibility permission needed). The toggle registers or
			// releases the system-wide accelerator at once. A registration
			// failure (another app owns ⌘⇧N) flips the switch back and
			// leaves hotkeyErr for this row to render — launch-time
			// failures arrive before the first frame, when toasts have no
			// context to attach to, so the error must be readable here.
			ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "全局新建任务 ⌘⇧N").FontSize(t.FontSize)
				ui.Spacer(c)
				if ui.Switch(c, &b.prefs.GlobalNewTaskHot).
					Label("全局热键新建任务").
					Changed() {
					_ = saveUIPrefs(b.prefs)
					b.syncGlobalHotkey()
				}
			})
			if b.hotkeyErr != "" {
				ui.Row(c).FillWidth().Gap(t.Space(2)).Children(func() {
					ui.Text(c, b.hotkeyErr).FontSize(t.FontSize).TextColor(c.Theme().Danger)
				})
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

const composeLiftPercent float32 = 14

func (b *board) formView(c *ui.Context, t *ui.Theme) {
	f := b.form
	open := f.open
	// DialogBase so the outer card radius can be much larger than inner chips.
	ui.DialogBase(c, &open, func(back, panel ui.Element) {
		back.Material(glass.Blur{Radius: 8}).
			Background(ui.RGBA(0, 0, 0, 0.58)).
			Transition(ui.ElementTransition{
				Duration: 260 * time.Millisecond,
				Ease:     ui.EaseOut,
				Enter:    &ui.Motion{Opacity: 0},
			})
		if f.isTask {
			panel.Transition(ui.ElementTransition{
				Duration: 380 * time.Millisecond,
				Ease:     ui.EaseOut,
				Enter:    &ui.Motion{Y: t.Space(12), Opacity: 0},
				Animation: &ui.Animation{Spring: ui.SpringOptions{
					Duration: 380 * time.Millisecond,
					Bounce:   0.02,
				}},
			})
			w, h := c.Size()
			// Chrome-free wrapper: the pill row inside carries the surface.
			panel.Padding(0).Gap(0).Translate(0, -h*composeLiftPercent/100).
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
				Placeholder("项目名").AutoFocus().ReadOnly(f.creating).
				OnTextKey(func(k ui.TextKey) bool {
					if k.Key != ui.KeyEnter {
						return false
					}
					if f.creating {
						return true
					}
					name := strings.TrimSpace(k.Text())
					if name == "" {
						b.toast(emptyToast, "项目名不能为空。")
						return true
					}
					f.name = name
					if editing {
						b.saveProjectEdit()
						return true
					}
					b.createProject(k.Mods&ui.Shift != 0)
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
					// The native sheet runs on its own goroutine and reports back
					// through b.post: awaiting it inside this render callback would
					// stall the frame that is asking for it.
					if ui.Button(c, "浏览…").Label("选择工作目录").Disabled(f.creating || f.pickingWorkspace).
						Padding(t.Space(1), t.Space(2)).BorderWidth(0).Clicked() {
						b.pickWorkspace(f)
					}
					if f.workspacePath != "" {
						if ui.Button(c, "清除").Label("清除工作目录").
							Padding(t.Space(1), t.Space(1.5)).BorderWidth(0).
							TextColor(t.TextMuted).Disabled(f.creating).Clicked() {
							f.workspacePath = ""
						}
					}
				})
			})
			ui.Column(c).FillWidth().Gap(t.Space(1)).Children(func() {
				ui.Text(c, "卡片颜色").FontSize(fxs(t)).FontWeight(600).TextColor(t.TextMuted)
				ui.Row(c).FillWidth().Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
					// A swatch row: clicking a chip adopts that accent for the
					// project (cards in 全部项目 carry it); 自动 uses the
					// project's stable palette slot instead.
					for _, color := range projectColorPalette {
						selected := f.color == color
						chip := ui.ButtonBase(c.Key("project-color-"+strings.TrimPrefix(color, "#"))).
							Size(t.Space(5), t.Space(5)).Radius(t.Space(5) / 2).
							Background(ui.Hex(color)).
							Tooltip(color).
							Label("项目颜色 " + color).
							Disabled(f.creating)
						if chip.Clicked() {
							f.color = color
						}
						chip.Draw(func(p *ui.Painter, r ui.Rect) {
							if selected {
								stroke := t.Text.Alpha(0.9)
								p.Stroke(ui.Rect{X: r.X - 1, Y: r.Y - 1, W: r.W + 2, H: r.H + 2}, stroke, 1, 0)
							}
						})
					}
					auto := f.color == ""
					autoChip := ui.ButtonBase(c.Key("project-color-auto")).
						Size(t.Space(5), t.Space(5)).Radius(t.Space(5) / 2).
						Background(t.Text.Alpha(0.12)).
						Tooltip("自动（稳定配色槽）").
						Label("项目颜色 自动").
						Disabled(f.creating)
					if autoChip.Clicked() {
						f.color = ""
					}
					autoChip.Draw(func(p *ui.Painter, r ui.Rect) {
						if auto {
							stroke := t.Text.Alpha(0.9)
							p.Stroke(ui.Rect{X: r.X - 1, Y: r.Y - 1, W: r.W + 2, H: r.H + 2}, stroke, 1, 0)
						}
						// A slash reads as "unset".
						center := ui.Rect{X: r.X, Y: r.Y + r.H/2 - 0.5, W: r.W, H: 1}
						p.Fill(center, t.Text.Alpha(0.35), 0)
					})
				})
			})
			if editing {
				ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "验收完成时自动提交 Git").FontSize(t.FontSize).Grow(1)
					_ = ui.Switch(c, &f.gitCommitOnDone).Label("验收完成时自动提交 Git").Disabled(f.creating)
				})
			}
			if editing {
				ui.Row(c).FillWidth().Justify(ui.Start).Children(func() {
					if ui.Button(c, "删除项目").Label("删除项目").
						TextColor(t.Danger).Disabled(f.creating).Clicked() {
						if p, ok := b.projectByID(f.editingProjectID); ok {
							pp := p
							b.confirmDelete = &pp
						}
					}
				})
			}
			ui.Row(c).FillWidth().Gap(t.Space(2)).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Disabled(f.creating).Clicked() {
					open = false
				}
				if ui.PrimaryButton(c, saveLabel).Disabled(f.creating).Clicked() {
					if editing {
						b.saveProjectEdit()
					} else {
						b.createProject(false)
					}
				}
			})
			_ = input
		})
	})
	if f.creating {
		open = true
	}
	if !open || !f.open {
		b.form = nil
		return
	}
	f.open = open
}

// taskComposeImmersive is a single-row pill prompt: input fills the bar,
// circular ↑ button nested at the right end — Enter creates+closes,
// Shift+Enter creates and stays for the next title.
func (b *board) taskComposeImmersive(c *ui.Context, t *ui.Theme, f *form, open *bool) {
	channels := executor.Runtimes()
	labels := make([]string, len(channels))
	for i, channel := range channels {
		labels[i] = channel.Label
	}
	if f.runtimeLabel == "" {
		f.runtimeLabel = labels[0]
		for _, channel := range channels {
			if channel.ID == b.prefs.LastTaskRuntime {
				f.runtimeLabel = channel.Label
				break
			}
		}
	}
	// These labels sit directly on the dark modal backdrop in both appearances.
	hintColor := t.Text
	if !t.Dark {
		hintColor = t.Background
	}
	projName := b.projectNameByID(f.projectID)
	projectTarget := projName
	if columnName := b.columnName(f.columnID); columnName != "" && columnName != f.columnID {
		projectTarget += " · " + columnName
	}
	startCreate := func(title, runtime string, stay bool) {
		f.creating = true
		title = withWorkspaceMentions(title, f.fileMentions)
		desc := buildComposeDescription(f.composeImages)
		if !b.createTaskAndDispatch(f.projectID, f.columnID, title, desc, runtime, func(created bool) {
			f.creating = false
			if created && b.form == f {
				f.name = ""
				f.fileMentions = nil
				f.composeImages = nil
				f.composeImage.pending = nil
				if !stay {
					f.open = false
				}
			}
		}) {
			f.creating = false
		}
	}
	ui.Column(c).Width(t.Space(140)).Gap(t.Space(1.5)).
		AlignItems(ui.Center).Children(func() {
		ui.Row(c).Gap(t.Space(3)).AlignItems(ui.Center).Children(func() {
			projectPicker := ui.ButtonBase(c.Key("compose-project")).
				Label("目标项目").Tooltip("选择任务所属项目").
				Padding(t.Space(1), t.Space(1)).Gap(t.Space(1)).
				BorderWidth(0).Radius(t.Radius)
			projectPicker.Background(crumbFace(t, projectPicker)).Children(func() {
				ui.Text(c, projectTarget).FontSize(fsm(t)).TextColor(hintColor.Alpha(0.9))
				ui.Icon(c, chevronDown).Size(12, 12).TextColor(hintColor.Alpha(0.75))
			})
			projectPicker.Menu(func(m *ui.Menu) {
				for _, project := range b.projects {
					p := project
					if m.Item(p.Name).Checked(f.projectID == p.ID).Chosen() {
						f.projectID = p.ID
						b.prefs.LastTaskProjectID = p.ID
						_ = saveUIPrefs(b.prefs)
						if len(p.Columns) > 0 {
							f.columnID = p.Columns[0].ID
						}
					}
				}
			})
			channelPicker := ui.ButtonBase(c.Key("compose-channel")).
				Label("执行渠道").Tooltip("选择创建并派发的执行渠道").
				Padding(t.Space(1), t.Space(1)).Gap(t.Space(1)).
				BorderWidth(0).Radius(t.Radius)
			channelPicker.Background(crumbFace(t, channelPicker)).Children(func() {
				ui.Text(c, f.runtimeLabel).FontSize(fsm(t)).TextColor(hintColor.Alpha(0.9))
				ui.Icon(c, chevronDown).Size(12, 12).TextColor(hintColor.Alpha(0.75))
			})
			channelPicker.Menu(func(m *ui.Menu) {
				for _, channel := range channels {
					if m.Item(channel.Label).Checked(f.runtimeLabel == channel.Label).Chosen() {
						f.runtimeLabel = channel.Label
						b.prefs.LastTaskRuntime = channel.ID
						_ = saveUIPrefs(b.prefs)
					}
				}
			})
		})
		if len(f.composeImages) > 0 || f.composeImage.uploading {
			b.composeThumbStrip(c, t, f)
		}
		f.name, f.fileMentions = adoptWorkspaceMentionTokens(f.name, f.fileMentions)
		// The input pill owns its entrance motion so the upward travel is visible,
		// independent of DialogBase's panel positioning.
		ui.Row(c).Key("compose-input-pill").Transition(ui.ElementTransition{
			Duration:  420 * time.Millisecond,
			Enter:     &ui.Motion{Y: t.Space(10), Opacity: 0},
			Animation: &ui.Animation{Spring: ui.SpringOptions{Duration: 420 * time.Millisecond, Bounce: 0.04}},
		}).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).
			Padding(t.Space(1.5), t.Space(3)).
			Radius(t.Space(5.5)).
			Background(ui.RGB(255, 255, 255)).
			BorderWidth(0).
			Shadow(0, 12, 36, 0, ui.RGBA(0, 0, 0, 0.35)).
			Children(func() {
				input := ui.TextInput(c, &f.name).Label("任务标题").
					Placeholder("写下一件要做的事…").
					Grow(1).
					BorderWidth(0).
					Background(ui.Transparent).
					TextColor(ui.RGB(31, 31, 36)).
					FontSize(flg(t)).
					AutoFocus().
					ReadOnly(f.creating || f.composeImage.uploading).
					OnTextKey(func(k ui.TextKey) bool {
						if f.fileMentionOpen && k.Mods == 0 {
							query, _ := workspaceMentionQuery(k.Text())
							files := filterWorkspaceMentionTargets(f.fileMentionTargets, query, 20)
							switch k.Key {
							case ui.KeyDown:
								if len(files) > 0 {
									f.fileMentionIndex = (f.fileMentionIndex + 1) % len(files)
								}
								return true
							case ui.KeyUp:
								if len(files) > 0 {
									f.fileMentionIndex = (f.fileMentionIndex + len(files) - 1) % len(files)
								}
								return true
							case ui.KeyEnter:
								if len(files) > 0 {
									index := f.fileMentionIndex
									if index < 0 || index >= len(files) {
										index = 0
									}
									f.name = k.Text()
									pickWorkspaceMention(&f.name, &f.fileMentions, files[index].Path)
									k.SetText(f.name)
									f.fileMentionOpen = false
								}
								return true
							}
						}
						if k.Key != ui.KeyEnter {
							return false
						}
						if f.creating || f.composeImage.busy() {
							return true
						}
						title := strings.TrimSpace(k.Text())
						if title == "" && len(f.fileMentions) == 0 {
							b.toast("无法创建", "任务标题不能为空。")
							return true
						}
						if k.Mods&ui.Cmd != 0 {
							for _, channel := range channels {
								if channel.Label == f.runtimeLabel {
									startCreate(title, channel.ID, false)
									break
								}
							}
							return true
						}
						stay := k.Mods&ui.Shift != 0
						startCreate(title, "", stay)
						return true
					})
				b.taskFileMentionMenu(c, t, f, input)
				b.composePaste(input, f)
				if p, ok := b.projectByID(f.projectID); ok {
					b.workspaceMentionChips(c, t, &f.fileMentions, p.WorkspacePath, "compose-file-chips")
				}
				// mygo's RGBA alpha is 0–1. A byte-scale 18 clamps to opaque
				// black, which hides this dark ↑ on the white pill.
				send := ui.Button(c, "").Label("创建任务").
					Disabled(f.creating || f.composeImage.busy()).
					Tooltip("回车创建并关闭 · Shift+回车继续 · ⌘Enter 创建并派发").
					Width(t.Space(8)).Height(t.Space(8)).
					Padding(0).BorderWidth(0).Radius(t.Space(4)).
					Background(ui.RGBA(0, 0, 0, 0.12)).
					AlignItems(ui.Center).Justify(ui.Center).
					Children(func() {
						ui.Text(c, "↑").FontSize(fmd(t)).FontWeight(600).TextColor(ui.RGB(31, 31, 36))
					})
				if send.Clicked() {
					title := strings.TrimSpace(f.name)
					if title == "" && len(f.fileMentions) == 0 {
						b.toast("无法创建", "任务标题不能为空。")
					} else {
						startCreate(title, "", false)
					}
				}
			})
	})
}

// --- conflict ---

// confirmDeleteView asks once before the irreversible delete; Esc and
// clicking outside both cancel.
func (b *board) confirmDeleteView(c *ui.Context, t *ui.Theme) {
	p := b.confirmDelete
	open := true
	choice := ui.AlertDialog(c, &open, "删除项目「"+p.Name+"」",
		"将删除该项目及其全部卡片（含已归档），不可恢复。", "删除", "取消")
	switch {
	case choice == 0:
		b.confirmDelete = nil
		b.deleteProject(*p)
	case !open:
		b.confirmDelete = nil
	}
}

func (b *board) confirmDeleteCommentView(c *ui.Context, t *ui.Theme) {
	cm := b.confirmDeleteComment
	if cm == nil {
		return
	}
	open := true
	choice := ui.AlertDialog(c, &open, "删除这条评论？", "删除后无法恢复。若评论已交给 AI 处理，删除不会撤回已发送内容。", "删除", "取消")
	switch {
	case choice == 0:
		b.confirmDeleteComment = nil
		b.deleteComment(*cm)
	case !open:
		b.confirmDeleteComment = nil
	}
}

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
// has moved past v1, the agent that completed it (or its last writer), and
// always the time. Manual moves must not obscure successful execution.
func provenance(task *model.Task) string {
	var parts []string
	if task.Version > 1 {
		parts = append(parts, "v"+strconv.Itoa(task.Version))
	}
	if task.CompletedBy != "" {
		parts = append(parts, task.CompletedBy+" 完成")
	} else if task.UpdatedBy != "" && task.UpdatedBy != "local" {
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
	beijing := t.In(beijingLocation)
	now := time.Now().In(beijingLocation)
	if d := now.Sub(beijing); d >= 0 && d < time.Minute {
		return "刚刚"
	}
	if now.Sub(beijing) < 24*time.Hour && beijing.Year() == now.Year() && beijing.YearDay() == now.YearDay() {
		return beijing.Format("15:04")
	}
	return beijing.Format("01-02")
}

var beijingLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

// commentTimestamp uses Beijing time regardless of the machine's local zone.
func commentTimestamp(t time.Time) string {
	return t.In(beijingLocation).Format("01-02 15:04")
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
