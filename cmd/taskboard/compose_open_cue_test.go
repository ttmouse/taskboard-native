package main

import (
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestComposeOpenCue: the cue rides the sheet surfacing, not the keystroke.
// ⌘N, the sidebar 新建任务 row and a column "+" all funnel through
// openTaskFormIn, so a sheet that opens rings once and a sheet that never
// appears — 「全部项目」 asks for a project instead — stays silent.
func TestComposeOpenCue(t *testing.T) {
	isolatePrefs(t)
	b := newBoard(seedBoard())
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return tt.HasText("把看板接上 SSE") })
	settle(t, tt)

	// Launch is 「全部项目」: no column to land in, so nothing surfaces.
	if b.cur != curGlobal {
		t.Fatalf("launch cur = %q, want curGlobal", b.cur)
	}
	lastCue = ""
	tt.Key(ui.Cmd, ui.KeyN)
	waitFor(t, tt, "新建提示", func() bool { return tt.HasText("请先在侧栏选一个项目") })
	if lastCue != "" {
		t.Fatalf("cue %q fired although no sheet opened", lastCue)
	}

	if err := tt.Click("切换到项目 local"); err != nil {
		t.Fatalf("choose project: %v", err)
	}
	waitFor(t, tt, "切换项目", func() bool { return b.cur == "local" })
	waitFor(t, tt, "看板列到达", func() bool { return len(b.columns) > 0 })
	settle(t, tt)

	lastCue = ""
	tt.Key(ui.Cmd, ui.KeyN)
	waitFor(t, tt, "新建任务浮层", func() bool { return b.form != nil && b.form.open })
	if lastCue != "open" {
		t.Fatalf("⌘N cue = %q, want open", lastCue)
	}

	// Closing is not an event of its own: one open, one ring.
	tt.Key(0, ui.KeyEscape)
	waitFor(t, tt, "浮层关闭", func() bool { return b.form == nil })
	if lastCue != "open" {
		t.Fatalf("closing re-fired a cue: %q", lastCue)
	}

	// The sidebar row takes the same door, so it rings the same cue.
	lastCue = ""
	b.openTaskForm(b.defaultTaskColumnID())
	tt.Frame()
	if lastCue != "open" {
		t.Fatalf("sidebar-row cue = %q, want open", lastCue)
	}
}

// Measure the actual waveform: each scheduled note must be audible, including
// the two harmonics after the three main notes have ended.
func TestOpenCueArpeggio(t *testing.T) {
	for _, note := range []struct {
		name           string
		freq, from, to float64
	}{
		{"C5", 523.25, 0.005, 0.055},
		{"E5", 659.25, 0.065, 0.115},
		{"G5", 783.99, 0.125, 0.175},
		{"C6 tail", 1046.5, 0.48, 0.65},
		{"E6 tail", 1318.5, 0.48, 0.65},
	} {
		t.Run(note.name, func(t *testing.T) {
			var re, im float64
			var n int
			for x := note.from; x < note.to; x += 1.0 / sfxRate {
				sample := openCueWave(x)
				phase := 2 * math.Pi * note.freq * x
				re += sample * math.Cos(phase)
				im += sample * math.Sin(phase)
				n++
			}
			if amplitude := 2 * math.Hypot(re, im) / float64(n); amplitude < 0.0015 {
				t.Fatalf("note missing: amplitude %.6f", amplitude)
			}
		})
	}
	for x := 0.0; x < openCueSeconds; x += 1.0 / sfxRate {
		if sample := openCueWave(x); math.IsNaN(sample) || math.Abs(sample) >= 1 {
			t.Fatalf("invalid sample at %.6f: %v", x, sample)
		}
	}
	if openCueWave(-0.01) != 0 || openCueWave(openCueSeconds) != 0 {
		t.Fatal("sound extends past scheduled notes")
	}
	if d := boardSFX().open.Duration(); d != 780*time.Millisecond {
		t.Fatalf("open cue duration = %v, want 780ms", d)
	}
}
