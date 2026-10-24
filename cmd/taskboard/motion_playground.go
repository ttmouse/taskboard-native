//go:build motionplayground

// PROTOTYPE: not compiled into the production application.
package main

import (
	"github.com/egoist/mygo/ui"
	"strings"
	"taskboard-native/internal/model"
	"time"
)

// MyGo paints the pointer preview independently of the source's opacity.
// The source stays built for the drag engine, but leaves normal layout.
func (b *board) dragSourceOpacity() float32 { return 0 }

func (b *board) columnDropStyle(panel ui.Element, t *ui.Theme, over bool) {
	// Reserve the outline in both states, balancing padding so the old
	// content inset stays unchanged. Hover changes color, never geometry.
	edge := t.Space(0.5)
	panel.Border(edge, ui.Color{}).Padding(t.Space(3) - edge)
	if over {
		panel.BorderColor(t.Accent)
	}
}

func (b *board) cardView(c *ui.Context, t *ui.Theme, task *model.Task) {
	card := b.taskCard(c, t, task)
	b.observeDragGrab(task.ID, card)
	if card.Dragging() {
		if b.dragging == nil {
			b.dragging = map[string]bool{}
		}
		b.dragging[task.ID] = true
		box := b.bounds[task.ID]
		card.Absolute().Size(box.W, box.H).Opacity(0)
		return
	}
	if b.dragging[task.ID] {
		delete(b.dragging, task.ID)
		// Escape or release outside all columns returns from the current
		// pointer preview, using the same landing spring as a valid drop.
		b.rememberDrop(task.ID, card)
	}
	tr := b.cardTransition(t, task)
	tr.Match = cardMatch{ID: task.ID}
	until, flying := b.flights[task.ID]
	flying = flying && c.Now().Before(until) && !c.Preferences().ReduceMotion
	if !flying {
		delete(b.flights, task.ID)
		delete(b.dropStarts, task.ID)
		card.Transition(tr)
		return
	}
	// The transparent destination keeps its full layout and lets siblings
	// make room. Only the overlay owns the matched identity during flight.
	// The invisible slot must already be at its final layout position;
	// animating it too makes the visible card chase a moving destination.
	card.Transition(ui.ElementTransition{Colors: true}).Opacity(0).PassThrough().Label("")
	ui.Overlay(c, func() {
		flight := b.taskCard(c, t, task).Width(b.columnWidth(c, t) - 2*t.Space(3))
		b.observeDragGrab(task.ID, flight)
		tr := b.cardTransition(t, task)
		tr.Match = cardMatch{ID: task.ID}
		tr.Enter = nil
		if start, dropped := b.dropStarts[task.ID]; dropped {
			// The typed drag preview disappears on pointer-up. Start its
			// replacement at the same window position, keeping the grab offset.
			target := start.box
			if c.Now().After(start.at) {
				if measured := card.Bounds(); measured.W > 0 {
					target = measured
				}
			}
			animation := *tr.Animation
			animation.Delay = 0 // direct manipulation never waits for a stagger
			x := flight.AnimateTo(landingAxis{start.at, 'x'}, target.X, animation)
			y := flight.AnimateTo(landingAxis{start.at, 'y'}, target.Y, animation)
			flight.Absolute().Left(x).Top(y).Size(start.box.W, start.box.H)
			flight.Transition(ui.ElementTransition{Colors: true, Animation: &animation})
			if c.Now().Equal(start.at) {
				c.After(time.Millisecond)
			}
		} else {
			flight.AttachTo(card, ui.AnchorTopLeft, ui.AnchorTopLeft)
			flight.Transition(tr)
		}
	})
	c.After(until.Sub(c.Now()))
}

type landingAxis struct {
	at   time.Time
	axis byte
}

func (b *board) observeDragGrab(id string, card ui.Element) {
	if card.Pressed() && !card.Dragging() && !b.dragging[id] {
		x, y, _ := card.PointerPosition()
		if b.dragGrabs == nil {
			b.dragGrabs = map[string]ui.Point{}
		}
		b.dragGrabs[id] = ui.Point{X: x, Y: y}
	}
}

func (b *board) dropWindowY(panel ui.Element, y float32) float32 {
	return panel.Bounds().Y + y
}

func (b *board) rememberDrop(id string, panel ui.Element) {
	if b.frameCtx.Preferences().ReduceMotion {
		return
	}
	grab, ok := b.dragGrabs[id]
	if !ok {
		return
	}
	x, y, _ := panel.PointerPosition()
	box := b.bounds[id]
	box.X, box.Y = panel.Bounds().X+x-grab.X, panel.Bounds().Y+y-grab.Y
	if b.dropStarts == nil {
		b.dropStarts = map[string]dropStart{}
	}
	b.dropStarts[id] = dropStart{box: box, at: b.frameCtx.Now()}
	if b.flights == nil {
		b.flights = map[string]time.Time{}
	}
	b.flights[id] = b.frameCtx.Now().Add(900 * time.Millisecond)
	delete(b.dragGrabs, id)
}

// The service renumbers the target column onto a 1000-step grid. Preview
// that same order so the optimistic slot and the SSE-confirmed slot agree.
func (b *board) previewMovePosition(task *model.Task, afterID string, atTop bool) {
	var others []model.Task
	for _, item := range b.tasksIn(task.ColumnID) {
		if item.ID != task.ID {
			others = append(others, item)
		}
	}
	at := len(others)
	if atTop {
		at = 0
	} else if afterID != "" {
		for i, item := range others {
			if item.ID == afterID {
				at = i + 1
				break
			}
		}
	}
	task.Position = float64((at + 1) * 1000)
	for i, item := range others {
		position := i + 1
		if i >= at {
			position++
		}
		if current := b.task(item.ID); current != nil {
			current.Position = float64(position * 1000)
		}
	}
}

func (b *board) cardTransition(t *ui.Theme, task *model.Task) ui.ElementTransition {
	// A damped spring preserves velocity when incoming events retarget a
	// card. No exit ghost is left behind when that card changes parents.
	animation := ui.Animation{Spring: ui.SpringOptions{Duration: 420 * time.Millisecond, Bounce: 0.04}}
	switch b.variant {
	case variants[0]:
		animation = ui.Animation{Duration: 260 * time.Millisecond, Ease: ui.EaseOut}
	case variants[2]:
		animation = ui.Animation{Spring: ui.SpringOptions{Duration: 480 * time.Millisecond, Bounce: 0.12}}
		for i, suffix := range []string{"A", "B", "C"} {
			if strings.HasSuffix(task.Title, suffix) {
				animation.Delay = time.Duration(i) * 55 * time.Millisecond
				break
			}
		}
	}
	tr := ui.ElementTransition{
		Animation: &animation,
		Position:  true,
		Size:      true,
		Colors:    true,
		Enter:     &ui.Motion{Y: t.Space(2), Collapse: true, Scale: 0.985},
	}
	return tr
}

func (b *board) noteMove(from, to model.Task) {
	if from.ColumnID == to.ColumnID || b.frameCtx == nil || b.frameCtx.Preferences().ReduceMotion {
		return
	}
	if b.flights == nil {
		b.flights = map[string]time.Time{}
	}
	b.flights[to.ID] = b.frameCtx.Now().Add(900 * time.Millisecond)
}

// A projection is drawing state only; the API receives one move on release.
type previewSlot struct {
	id    string
	after string
	atTop bool
}

func (b *board) columnCards(c *ui.Context, t *ui.Theme, col *model.Column, tasks []model.Task, panel, list ui.Element, dragID string, over bool, _ int) {
	var projected []model.Task
	for _, task := range tasks {
		if !over || task.ID != dragID {
			projected = append(projected, task)
		}
	}
	at := -1
	if over {
		_, localY, _ := panel.PointerPosition()
		y := b.dropWindowY(panel, localY)
		// Compare with natural slots, not animated card bounds or the ghost.
		// Otherwise the space created by the ghost feeds back into hit testing.
		cursor := list.Bounds().Y
		at = 0
		for _, task := range projected {
			h := b.bounds[task.ID].H
			if y < cursor+h/2 {
				break
			}
			at++
			cursor += h + t.Space(2)
		}
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
		h := b.bounds[dragID].H
		if h <= 0 {
			h = 74
		}
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
	for i := range projected {
		if i == at {
			ghost()
		}
		b.cardView(c, t, &projected[i])
	}
	if at == len(projected) {
		ghost()
	}
	if len(tasks) == 0 && !over {
		ui.Box(c).FillWidth().Padding(t.Space(4), t.Space(4)).Gap(t.Space(1)).Children(func() {
			ui.Text(c, "还没有任务").FontSize(fsm(t)).TextColor(t.TextMuted)
			ui.Text(c, "从上方新建，或拖入卡片").FontSize(fsm(t)).TextColor(t.TextMuted)
		})
	}
}

func (b *board) dropProjected(id string, col *model.Column, panel ui.Element) bool {
	slot, ok := b.previewSlots[col.ID]
	if !ok || slot.id != id {
		return true
	}
	b.rememberDrop(id, panel)
	b.moveTask(id, col.ID, slot.after, slot.atTop)
	delete(b.previewSlots, col.ID)
	return true
}
