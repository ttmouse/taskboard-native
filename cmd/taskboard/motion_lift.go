package main

import (
	"github.com/egoist/mygo/ui"
	"taskboard-native/internal/model"
	"time"
)

type singleFlight struct {
	id                    string
	source                ui.Rect
	at                    time.Time
	liftAt, landAt, endAt time.Time
	liftFrom, elevation   float32
}

const singleLift = 160 * time.Millisecond
const singleLand = 680 * time.Millisecond
const singleEnd = 900 * time.Millisecond

func smoothUnit(elapsed, duration time.Duration) float32 {
	p := min(max(float32(elapsed)/float32(duration), 0), 1)
	return p * p * (3 - 2*p)
}

func (b *board) liftedCard(c *ui.Context, t *ui.Theme, task *model.Task, slot ui.Element, f *singleFlight) {
	elapsed := c.Now().Sub(f.at)
	if !c.Now().Before(f.endAt) || c.Preferences().ReduceMotion {
		delete(b.liftedFlights, task.ID)
		delete(b.flights, task.ID)
		b.motionPhase("落稳 · 单卡演示完成，可再次播放")
		return
	}
	// Only the overlay is visible. The real destination already occupies
	// its final geometry and cannot chase its own animated counterpart.
	slot.Opacity(0).PassThrough().Label("")
	elevation := f.liftFrom + (1-f.liftFrom)*smoothUnit(c.Now().Sub(f.liftAt), singleLift)
	if !c.Now().Before(f.landAt) {
		elevation = 1 - smoothUnit(c.Now().Sub(f.landAt), f.endAt.Sub(f.landAt))
	}
	f.elevation = elevation
	if b.singleDemoMode() {
		b.motionPhase("① 抬升 · 卡片离开面板")
		if elapsed >= singleLift {
			b.motionPhase("② 悬浮移动 · 保持高度")
		}
		if !c.Now().Before(f.landAt) {
			b.motionPhase("③ 落稳 · 阴影收拢")
		}
	}
	ui.Overlay(c, func() {
		flight := b.taskCard(c, t, task, task.ID+"-lifted-flight")
		target := f.source
		if elapsed >= singleLift && slot.Bounds().W > 0 {
			target = slot.Bounds()
		}
		animation := *b.cardTransition(t, task).Animation
		animation.Delay = 0 // the shared stage clock already owns the visual stagger
		x := flight.AnimateTo(landingAxis{f.at, 'x'}, target.X, animation)
		y := flight.AnimateTo(landingAxis{f.at, 'y'}, target.Y, animation)
		// Keep the source width so the flight starts where it was measured,
		// but let the content determine its height. A newly-created card can
		// still be in its collapsing Enter transition when an agent claims it;
		// copying that transient height into the flight clips the card until
		// the destination slot appears after landing.
		flight.Absolute().Left(x).Top(y).Width(f.source.W).
			Translate(0, -8*elevation).Scale(1+0.025*elevation, 1+0.025*elevation).
			Shadow(0, 16*elevation, 26*elevation, 0, ui.RGBA(0, 0, 0, 0.22*elevation)).
			PassThrough()
	})
	c.After(16 * time.Millisecond)
}
