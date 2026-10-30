package main

import (
	"math"
	"sync"

	"github.com/egoist/mygo/audio"
)

// Board cues are short soft synths (same approach as mygo's farm/mayoi
// examples): no asset files, quiet enough not to nag, distinct enough to
// tell open / create / move / done apart.
const sfxRate = 32000

// Includes the last C6 tail (0.18 + 0.6 seconds).
const openCueSeconds = 0.78

type boardSounds struct {
	open, create, move, done *audio.Sound
}

var (
	sfxOnce sync.Once
	sfx     boardSounds
)

// lastCue names the cue that fired most recently. Audio has nothing to
// assert on, so tests read this to prove the right cue fired at the right
// moment (and that a rejected open stays quiet). Every play* writes it; no
// production code reads it. Cues fire only on the UI goroutine — the one
// the render tester drives — so a test needs no lock.
var lastCue string

func boardSFX() boardSounds {
	sfxOnce.Do(func() {
		sfx = boardSounds{
			// C5 → E5 → G5 with C6/E6 tails, matching AudioManager.playOpen.
			open: synth(openCueSeconds, openCueWave),
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

// openCueWave mirrors the Web Audio oscillator frequencies, offsets, levels
// and exponential gain ramps in the console's useSound.ts.
func openCueWave(t float64) float64 {
	note := func(freq, start, duration, volume float64, tail, triangle bool) float64 {
		elapsed := t - start
		if elapsed < 0 || elapsed >= duration {
			return 0
		}
		gain := volume * math.Pow(0.001/volume, elapsed/duration)
		if tail {
			switch {
			case elapsed <= duration*0.1:
				gain = volume
			case elapsed <= duration*0.4:
				gain = volume * 0.8 * math.Pow(0.3/0.8, (elapsed-duration*0.1)/(duration*0.3))
			default:
				gain = volume * 0.3 * math.Pow(0.001/(volume*0.3), (elapsed-duration*0.4)/(duration*0.6))
			}
		}
		wave := math.Sin(2 * math.Pi * freq * elapsed)
		if triangle {
			wave = 2 / math.Pi * math.Asin(wave)
		}
		return gain * wave
	}
	return note(523.25, 0, 0.25, 0.04, false, false) +
		note(659.25, 0.06, 0.25, 0.035, false, false) +
		note(783.99, 0.12, 0.35, 0.03, false, false) +
		note(1046.5, 0.18, 0.6, 0.02, true, false) +
		note(1318.5, 0.22, 0.5, 0.012, true, true)
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

// Cues are played through one helper so lastCue can never drift from what
// the speakers actually got.
func cue(name string, s *audio.Sound, volume float64) {
	lastCue = name
	s.NewPlayer().SetLoop(false).SetVolume(volume).Play()
}

func playOpenCue() {
	cue("open", boardSFX().open, 1)
}

func playCreateCue() {
	cue("create", boardSFX().create, 0.45)
}

func playMoveCue() {
	cue("move", boardSFX().move, 0.4)
}

func playDoneCue() {
	cue("done", boardSFX().done, 0.5)
}
