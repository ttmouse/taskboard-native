package main

import (
	"math"
	"sync"

	"github.com/egoist/mygo/audio"
)

// Board cues are short soft synths (same approach as mygo's farm/mayoi
// examples): no asset files, quiet enough not to nag, distinct enough to
// tell create / move / done apart.
const sfxRate = 32000

type boardSounds struct {
	create, move, done *audio.Sound
}

var (
	sfxOnce sync.Once
	sfx     boardSounds
)

func boardSFX() boardSounds {
	sfxOnce.Do(func() {
		sfx = boardSounds{
			// Soft high tick — a task appeared.
			create: synth(0.07, func(t float64) float64 {
				return math.Sin(2*math.Pi*(980-420*t)*t) * math.Exp(-t*38) * 0.28
			}),
			// Quieter mid click — left one column for another.
			move: synth(0.055, func(t float64) float64 {
				return math.Sin(2*math.Pi*(520-180*t)*t) * math.Exp(-t*45) * 0.18
			}),
			// Two soft bell partials a fifth apart — landed in 完成.
			done: synth(0.32, func(t float64) float64 {
				bell := func(f, nt float64) float64 {
					if nt < 0 {
						return 0
					}
					return (math.Sin(2*math.Pi*f*nt) + 0.35*math.Sin(2*math.Pi*f*2.7*nt)*math.Exp(-nt*14)) *
						math.Exp(-nt*6)
				}
				return 0.22 * (bell(784, t) + bell(1175, t-0.07))
			}),
		}
	})
	return sfx
}

func synth(d float64, f func(t float64) float64) *audio.Sound {
	s := make([]float32, int(d*sfxRate))
	for i := range s {
		t := float64(i) / sfxRate
		edge := min(t/0.002, (d-t)/0.012, 1)
		s[i] = float32(f(t) * edge)
	}
	return audio.NewSound(sfxRate, 1, s)
}

func playCreateCue() {
	boardSFX().create.NewPlayer().SetVolume(0.45).Play()
}

func playMoveCue() {
	boardSFX().move.NewPlayer().SetVolume(0.4).Play()
}

func playDoneCue() {
	boardSFX().done.NewPlayer().SetVolume(0.5).Play()
}
