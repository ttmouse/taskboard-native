package main

import (
	"github.com/egoist/mygo/ui"
	"taskboard-native/internal/model"
)

// columnCardOverscan is extra DIPs above/below the scroll viewport whose
// cards still get a full cardView. Farther rows keep a cheap height
// placeholder so 200-card columns do not rebuild every taskCard each frame.
const columnCardOverscan float32 = 240

// columnHeaderHeight approximates the plain (horizontal) column title row
// (Space(7) ≈ 28) plus the panel/gap air above the scroll viewport.
const columnHeaderHeight float32 = 40

// estimatedCardHeight is used before a card has been measured, and for
// off-screen placeholders so scroll extent stays stable.
func (b *board) estimatedCardHeight(task *model.Task) float32 {
	if task != nil {
		if h := b.cardHeights[task.ID]; h > 0 {
			return h
		}
		if h := b.bounds[task.ID].H; h > 0 {
			return h
		}
	}
	if b.prefs.TitleOnlyCards {
		return 52
	}
	// Full cards with project/summary/footer commonly land near ~118 DIP.
	return 118
}

// columnCards builds the cards (and drop ghost) inside a column's Scroll.
// Only rows intersecting the viewport ± overscan call cardView/taskCard;
// the rest are height placeholders. Drag/drop, keyboard focus on visible
// cards, and a11y labels on built cards are unchanged.
//
// Viewport height comes from list/panel Bounds and TrackScroll MaxY (the
// Scroll element's Bounds is often zero inside Children), cached on
// colScroll.lastViewH across frames.
func (b *board) columnCards(c *ui.Context, t *ui.Theme, col *model.Column, tasks []model.Task, panel, list ui.Element, dragID string, over bool) {
	var projected []model.Task
	for _, task := range tasks {
		if !over || task.ID != dragID {
			projected = append(projected, task)
		}
	}
	at := -1
	gap := t.Space(2)
	if over {
		// Hit-test in the list's local space so Scroll offsets are included.
		_, y, _ := list.PointerPosition()
		heights := make([]float32, len(projected))
		for i := range projected {
			heights[i] = b.estimatedCardHeight(&projected[i])
		}
		at = edgeInsertIndex(y, heights, gap)
		if b.previewSlots == nil {
			b.previewSlots = map[string]previewSlot{}
		}
		slot := previewSlot{id: dragID, atTop: at == 0}
		if at > 0 {
			slot.after = projected[at-1].ID
		}
		b.previewSlots[col.ID] = slot
	} else {
		delete(b.previewSlots, col.ID)
	}
	ghost := func() {
		h := b.estimatedCardHeight(b.task(dragID))
		ui.Box(c.Key("drop-preview")).FillWidth().Height(h).Radius(t.Radius+2).
			Background(t.Accent.Alpha(0.06)).Border(1, t.Accent.Alpha(0.28)).
			Label("任务落位预览").PassThrough()
	}
	// Keep the source under its original parent/key so MyGo can continue
	// painting its pointer preview. cardView takes it out of normal flow.
	if over {
		for i := range tasks {
			if tasks[i].ID == dragID {
				b.cardView(c, t, &tasks[i])
			}
		}
	}

	viewTop := float32(0)
	top, bottom := float32(0), float32(1e9)
	if track := b.columnScroll(col.ID); track != nil {
		viewTop = track.state.Y
		// list.Bounds().H is the *content* height inside Scroll — never the
		// viewport. Prefer content−MaxY, then panel minus header, then cache.
		var content float32
		for i := range projected {
			content += b.estimatedCardHeight(&projected[i]) + gap
		}
		if at >= 0 {
			content += b.estimatedCardHeight(b.task(dragID)) + gap
		}
		content += t.Space(6)
		var viewH float32
		if track.state.MaxY > 0 && content > track.state.MaxY {
			viewH = content - track.state.MaxY
		}
		if viewH <= 0 {
			if ph := panel.Bounds().H; ph > columnHeaderHeight+8 {
				viewH = ph - columnHeaderHeight
			}
		}
		if viewH <= 0 {
			viewH = track.lastViewH
		}
		// Reject a stale cache that looks like content height.
		if viewH > 0 && content > 0 && viewH >= content*0.9 && track.state.MaxY > 0 {
			viewH = content - track.state.MaxY
		}
		if viewH > 0 {
			track.lastViewH = viewH
			viewBottom := viewTop + viewH
			top = viewTop - columnCardOverscan
			bottom = viewBottom + columnCardOverscan
		}
	}

	cursor := float32(0)
	for i := range projected {
		if i == at {
			h := b.estimatedCardHeight(b.task(dragID))
			ghost()
			cursor += h + gap
		}
		task := &projected[i]
		h := b.estimatedCardHeight(task)
		cardTop, cardBottom := cursor, cursor+h
		inView := cardBottom >= top && cardTop <= bottom
		if inView {
			b.cardView(c, t, task)
			if measured := b.bounds[task.ID].H; measured > 0 {
				h = measured
			}
		} else {
			// Drop stale X/Y/W so layout tests do not use a pre-cull frame's
			// coordinates; cardHeights keeps H for stable scroll extent.
			delete(b.bounds, task.ID)
			ui.Box(c.Key(task.ID)).FillWidth().Height(h).
				PassThrough().Label("")
		}
		cursor += h + gap
	}
	if at == len(projected) {
		ghost()
	}
}
