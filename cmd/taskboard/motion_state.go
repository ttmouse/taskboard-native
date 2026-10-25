package main

import (
	"github.com/egoist/mygo/ui"
	"time"
)

type motionState struct {
	playgroundState
	liftBatchAt    time.Time
	liftBatchCount int
	liftedFlights  map[string]*singleFlight
	dragging       map[string]bool
	previewSlots   map[string]previewSlot
	flights        map[string]time.Time
	dragGrabs      map[string]ui.Point
	dropStarts     map[string]dropStart
}

type dropStart struct {
	box ui.Rect
	at  time.Time
}
