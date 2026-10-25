package main

import (
	"github.com/egoist/mygo/ui"
	"taskboard-native/internal/model"
	"time"
)

// MyGo paints the pointer preview independently of the source's opacity.
// The source stays built for the drag engine, but leaves normal layout.
func (b *board) dragSourceOpacity() float32 { return 0 }

func (b *board) cardView(c *ui.Context, t *ui.Theme, task *model.Task) {
	card := b.taskCard(c, t, task)
	if f := b.liftedFlights[task.ID]; f != nil {
		b.liftedCard(c, t, task, card, f)
		return
	}
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

// edgeInsertIndex returns where a dragged card should sit among projected
// siblings. Heights are natural (pre-ghost) card heights in list-local Y.
// Crossing a card's top edge makes that card yield for its whole body —
// insert before it until the pointer passes its bottom (not the midpoint).
func edgeInsertIndex(y float32, heights []float32, gap float32) int {
	cursor := float32(0)
	at := 0
	for _, h := range heights {
		if h <= 0 {
			h = 74
		}
		if y < cursor+h {
			return at
		}
		at++
		cursor += h + gap
	}
	return at
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
	animation := b.motionAnimation(t, task)
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
	if from.ColumnID != to.ColumnID {
		b.playColumnChangeCue(to.ColumnID)
	}
	if from.ColumnID == to.ColumnID || b.frameCtx == nil || b.frameCtx.Preferences().ReduceMotion {
		return
	}
	// Pointer-up already has a direct landing path; never lift it again.
	if _, dropped := b.dropStarts[to.ID]; dropped {
		return
	}
	now := b.frameCtx.Now()
	if b.liftedFlights == nil {
		b.liftedFlights = map[string]*singleFlight{}
	}
	f := b.liftedFlights[to.ID]
	if f == nil {
		source := b.bounds[from.ID]
		if source.W <= 0 || source.H <= 0 {
			return
		}
		start := now
		if !b.singleDemoMode() {
			if b.liftBatchAt.IsZero() || now.Sub(b.liftBatchAt) > 80*time.Millisecond {
				b.liftBatchAt, b.liftBatchCount = now, 0
			}
			// Bound the visual delay at 80ms even for larger batches. HTTP
			// requests and model updates remain independent of this schedule.
			start = maxTime(now, b.liftBatchAt.Add(time.Duration(min(b.liftBatchCount, 2))*40*time.Millisecond))
			b.liftBatchCount++
		}
		f = &singleFlight{id: to.ID, source: source, at: start, liftAt: start, landAt: start.Add(singleLand), endAt: start.Add(singleEnd)}
		b.liftedFlights[to.ID] = f
	} else {
		// Keep the position animation's identity and velocity. If already
		// settling, lift from the current height without a scale/shadow jump.
		if !now.Before(f.landAt) {
			f.liftFrom = f.elevation
			f.liftAt = now
		}
		if !now.Before(f.at) {
			f.landAt = now.Add(520 * time.Millisecond)
			f.endAt = now.Add(740 * time.Millisecond)
		}
	}
	if b.flights == nil {
		b.flights = map[string]time.Time{}
	}
	b.flights[to.ID] = f.endAt
}

// A projection is drawing state only; the API receives one move on release.
type previewSlot struct {
	id    string
	after string
	atTop bool
}

func (b *board) columnCards(c *ui.Context, t *ui.Theme, col *model.Column, tasks []model.Task, panel, list ui.Element, dragID string, over bool) {
	var projected []model.Task
	for _, task := range tasks {
		if !over || task.ID != dragID {
			projected = append(projected, task)
		}
	}
	at := -1
	if over {
		// Hit-test in the list's local space so Scroll offsets are included.
		// panel+window Y ignored scroll and made edge yields feel stuck.
		_, y, _ := list.PointerPosition()
		gap := t.Space(2)
		heights := make([]float32, len(projected))
		for i, task := range projected {
			heights[i] = b.bounds[task.ID].H
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
	// Empty columns stay blank — no reminder copy (列头「添加任务」已够).
}

func (b *board) dropOn(id string, col *model.Column, panel ui.Element) {
	slot, ok := b.previewSlots[col.ID]
	if !ok || slot.id != id {
		return
	}
	b.rememberDrop(id, panel)
	b.moveTask(id, col.ID, slot.after, slot.atTop)
	delete(b.previewSlots, col.ID)
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func (b *board) clearMotion() {
	b.liftBatchAt = time.Time{}
	b.liftBatchCount = 0
	b.flights = nil
	b.dragGrabs = nil
	b.dropStarts = nil
	b.previewSlots = nil
	b.dragging = nil
	b.liftedFlights = nil
}
func (b *board) forgetMotion(id string) {
	delete(b.flights, id)
	delete(b.liftedFlights, id)
	delete(b.dragGrabs, id)
	delete(b.dragging, id)
	delete(b.dropStarts, id)
}
