//go:build motionplayground

package main

import (
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"taskboard-native/internal/model"
)

func TestColumnDropHighlightDoesNotMoveLayout(t *testing.T) {
	for _, target := range []string{"评审", "完成"} {
		t.Run(target, func(t *testing.T) {
			b := newBoard(seedBoard())
			b.variant = variants[1]
			defer b.stop()
			tt := newBoardTester(b)
			waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
			settle(t, tt)
			labels := []string{target, "在「" + target + "」中添加任务", "进行中", "在「进行中」中添加任务"}
			before := map[string]ui.Rect{}
			for _, label := range labels {
				r, ok := tt.Find(label)
				if !ok {
					t.Fatal("missing " + label)
				}
				before[label] = r
			}
			source, _ := tt.Find("打开任务：把看板接上 SSE")
			input := before[labels[1]]
			tt.Press(source.X+31, source.Y+23)
			tt.Move(source.X+43, source.Y+35)
			tt.Move(input.X+60, input.Y+110)
			for _, label := range labels {
				after, _ := tt.Find(label)
				if after != before[label] {
					t.Errorf("highlight moved %s: %+v -> %+v", label, before[label], after)
				}
			}
			tt.Key(0, ui.KeyEscape)
			tt.Release(input.X+60, input.Y+110)
		})
	}
}

func TestDragHidesOriginalKeepsPreviewAndRestoresOnCancel(t *testing.T) {
	b := newBoard(seedBoard())
	b.variant = variants[1]
	defer b.stop()
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
	settle(t, tt)
	source, _ := tt.Find("打开任务：把看板接上 SSE")
	tt.Press(source.X+31, source.Y+23)
	tt.Move(source.X+43, source.Y+35)
	tt.Move(720, 470)
	img := tt.Image()
	x, y := int(source.X+source.W/2), int(source.Y+source.H-5)
	column := colFace(b.curTheme)
	pixel := img.RGBAAt(x, y)
	preview := img.RGBAAt(int(720-31+source.W/2), int(470-23+source.H-5))
	if preview.R == column.R && preview.G == column.G && preview.B == column.B {
		t.Fatal("hiding the source also hid the pointer preview")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Release(720, 470)
	tt.Advance(400 * time.Millisecond)
	if b.task("t-1").ColumnID != "c-todo" {
		t.Fatal("cancel changed the task status")
	}
	face := cardFace(b.curTheme)
	pixel = tt.Image().RGBAAt(x, y)
	if pixel.R != face.R || pixel.G != face.G || pixel.B != face.B {
		t.Fatalf("cancel did not restore source: %v", pixel)
	}
}

func TestDropStartsAtReleasedPreview(t *testing.T) {
	for _, variant := range variants[:2] {
		for _, tc := range []struct {
			name, task               string
			grabX, grabY, endX, endY float32
		}{
			{"cross-column", "t-1", 31, 23, 720, 470},
			{"right-to-left", "t-5", 60, 40, 90, 410},
			{"same-column", "t-1", 30, 20, 170, 480},
			{"empty-column", "t-1", 230, 35, 1100, 300},
		} {
			t.Run(variant+"/"+tc.name, func(t *testing.T) {
				b := newBoard(seedBoard())
				b.variant = variant
				defer b.stop()
				tt := newBoardTester(b)
				waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
				settle(t, tt)
				label := "打开任务：" + b.task(tc.task).Title
				source, _ := tt.Find(label)
				// Grab away from the center: the release must retain this offset.
				grabX, grabY := tc.grabX, tc.grabY
				endX, endY := tc.endX, tc.endY
				tt.Press(source.X+grabX, source.Y+grabY)
				tt.Move(source.X+grabX+12, source.Y+grabY+12)
				tt.Move(endX, endY)
				tt.Release(endX, endY)
				after, ok := tt.Find(label)
				if !ok {
					t.Fatal("released card vanished")
				}
				wantX, wantY := endX-grabX, endY-grabY
				if math.Abs(float64(after.X-wantX)) > 2 || math.Abs(float64(after.Y-wantY)) > 2 {
					t.Fatalf("release jumped: got (%.1f, %.1f), want preview origin (%.1f, %.1f)", after.X, after.Y, wantX, wantY)
				}
				var points []ui.Rect
				for frame := 0; frame < 12; frame++ {
					tt.Advance(16 * time.Millisecond)
					point, _ := tt.Find(label)
					points = append(points, point)
				}
				tt.Advance(2 * time.Second)
				tt.Frame()
				final, _ := tt.Find(label)
				total := math.Hypot(float64(final.X-wantX), float64(final.Y-wantY))
				previous := after
				for frame, point := range points {
					step := math.Hypot(float64(point.X-previous.X), float64(point.Y-previous.Y))
					if step > math.Max(3, total*0.3) {
						t.Fatalf("frame %d jumped %.1f of %.1f DIP: %+v -> %+v", frame, step, total, previous, point)
					}
					if point.X < min(wantX, final.X)-8 || point.X > max(wantX, final.X)+8 || point.Y < min(wantY, final.Y)-8 || point.Y > max(wantY, final.Y)+8 {
						t.Fatalf("frame %d left the release-to-slot path: %+v -> %+v, got %+v", frame, after, final, point)
					}
					previous = point
				}
			})
		}
	}
}

// A matched card must stay painted while crossing the gutter, rather
// than disappearing behind its destination column's scroll clip.
func TestMovingCardStaysVisibleAcrossColumns(t *testing.T) {
	b := newBoard(seedBoard())
	b.variant = "B · 柔和弹簧"
	defer b.stop()
	tt := newBoardTester(b)
	tt.SetDark(true)
	waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
	settle(t, tt)
	card := *b.task("t-1")
	card.ColumnID, card.Version = "c-doing", card.Version+1
	b.post(func() {
		b.applyEvent(model.Event{Type: model.EventTaskMoved, ProjectID: b.cur, By: b.agent, Task: &card})
	})
	tt.Frame()
	for frame := 0; frame < 8; frame++ {
		tt.Advance(16 * time.Millisecond)
		r, ok := tt.Find("打开任务：" + card.Title)
		if !ok {
			t.Fatal("moving card lost its identity")
		}
		// Sample the blank lower band, including the middle of the gutter.
		x, y := int(r.X+r.W/2), int(r.Y+r.H-5)
		pixel := tt.Image().RGBAAt(x, y)
		face := cardFace(b.curTheme)
		if pixel.R != face.R || pixel.G != face.G || pixel.B != face.B {
			t.Fatalf("frame %d: moving card clipped at (%d,%d): got %v, want card face %v; bounds %+v", frame, x, y, pixel, face, r)
		}
	}
}

func TestParallelFlightsAndRetarget(t *testing.T) {
	b := newBoard(seedBoard())
	b.variant = "B · 柔和弹簧"
	defer b.stop()
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
	settle(t, tt)
	move := func(id, column string) {
		card := *b.task(id)
		card.ColumnID, card.Version = column, card.Version+1
		b.post(func() {
			b.applyEvent(model.Event{Type: model.EventTaskMoved, ProjectID: b.cur, By: b.agent, Task: &card})
		})
		tt.Frame()
	}
	move("t-1", "c-review")
	tt.Advance(80 * time.Millisecond)
	move("t-2", "c-doing")
	tt.Advance(80 * time.Millisecond)
	move("t-3", "c-done")
	if len(b.flights) != 3 {
		t.Fatalf("parallel flights = %d, want 3", len(b.flights))
	}
	before, _ := tt.Find("打开任务：把看板接上 SSE")
	move("t-1", "c-done")
	after, _ := tt.Find("打开任务：把看板接上 SSE")
	if before.X != after.X || before.Y != after.Y {
		t.Fatalf("retarget jumped: %+v -> %+v", before, after)
	}
	tt.Advance(2 * time.Second)
	tt.Frame()
	if len(b.flights) != 0 {
		t.Fatalf("flights did not finish: %v", b.flights)
	}
	for _, id := range []string{"t-1", "t-2", "t-3"} {
		label := "打开任务：" + b.task(id).Title
		count := 0
		for _, text := range tt.Texts() {
			if text == label {
				count++
			}
		}
		if count != 1 {
			t.Errorf("%s has %d visible copies", id, count)
		}
	}
}

func TestReducedMotionDoesNotLiftCards(t *testing.T) {
	b := newBoard(seedBoard())
	b.variant = "B · 柔和弹簧"
	defer b.stop()
	tt := newBoardTester(b)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
	card := *b.task("t-1")
	card.ColumnID, card.Version = "c-doing", card.Version+1
	b.post(func() { b.applyEvent(model.Event{Type: model.EventTaskMoved, ProjectID: b.cur, Task: &card}) })
	tt.Frame()
	if len(b.flights) != 0 {
		t.Fatal("reduced motion created an overlay flight")
	}
}

func TestDragProjectionReflowsAndCancels(t *testing.T) {
	for _, target := range []string{"完成", "进行中"} {
		t.Run(target, func(t *testing.T) {
			b := newBoard(seedBoard())
			b.variant = variants[1]
			defer b.stop()
			tt := newBoardTester(b)
			waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
			settle(t, tt)
			source, _ := tt.Find("打开任务：把看板接上 SSE")
			sibling, _ := tt.Find("打开任务：对齐设计规格的间距与字号")
			input, _ := tt.Find("在「" + target + "」中添加任务")
			x, y := input.X+40, input.Y+input.H+25
			tt.Press(source.X+31, source.Y+23)
			tt.Move(source.X+43, source.Y+35)
			tt.Move(x, y)
			tt.Advance(time.Second)
			tt.Frame()
			moved, _ := tt.Find("打开任务：对齐设计规格的间距与字号")
			if math.Abs(float64(moved.Y-source.Y)) > 2 {
				t.Fatalf("source did not close gap: source %+v sibling %+v -> %+v", source, sibling, moved)
			}
			ghost, ok := tt.Find("任务落位预览")
			if !ok || math.Abs(float64(ghost.H-source.H)) > 1 {
				t.Fatalf("missing full-height ghost: %+v vs %+v", ghost, source)
			}
			if b.task("t-1").ColumnID != "c-todo" {
				t.Fatal("hover committed task status")
			}
			for i := 0; i < 12; i++ {
				tt.Advance(40 * time.Millisecond)
				next, _ := tt.Find("任务落位预览")
				if next != ghost {
					t.Fatalf("stationary hover oscillates: %+v -> %+v", ghost, next)
				}
			}
			tt.Key(0, ui.KeyEscape)
			tt.Release(x, y)
			tt.Advance(2 * time.Second)
			tt.Frame()
			restored, _ := tt.Find("打开任务：对齐设计规格的间距与字号")
			if math.Abs(float64(restored.Y-sibling.Y)) > 2 {
				t.Fatalf("cancel did not restore order: %+v -> %+v", sibling, restored)
			}
			if _, ok := tt.Find("任务落位预览"); ok {
				t.Fatal("ghost survived cancel")
			}
		})
	}
}

func TestProjectedDropCommitsDisplayedSlot(t *testing.T) {
	for _, target := range []string{"待办", "进行中", "完成"} {
		t.Run(target, func(t *testing.T) {
			b := newBoard(seedBoard())
			b.variant = variants[1]
			defer b.stop()
			tt := newBoardTester(b)
			waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
			settle(t, tt)
			source, _ := tt.Find("打开任务：把看板接上 SSE")
			input, _ := tt.Find("在「" + target + "」中添加任务")
			x, y := input.X+40, input.Y+input.H+350
			tt.Press(source.X+31, source.Y+23)
			tt.Move(source.X+43, source.Y+35)
			tt.Move(x, y)
			tt.Advance(time.Second)
			tt.Frame()
			ghost, ok := tt.Find("任务落位预览")
			if !ok {
				t.Fatal("missing ghost")
			}
			tt.Release(x, y)
			tt.Advance(2 * time.Second)
			tt.Frame()
			landed, _ := tt.Find("打开任务：把看板接上 SSE")
			if math.Abs(float64(landed.Y-ghost.Y)) > 2 {
				t.Fatalf("drop differs from preview: %+v -> %+v", ghost, landed)
			}
			if _, ok := tt.Find("任务落位预览"); ok {
				t.Fatal("ghost survived release")
			}
			// The source's next card must close the gap permanently after a cross-column drop.
			if target != "待办" {
				next, _ := tt.Find("打开任务：对齐设计规格的间距与字号")
				if math.Abs(float64(next.Y-source.Y)) > 2 {
					t.Fatalf("source retained hole after release: %+v", next)
				}
			} else if b.tasksIn("c-todo")[1].ID != "t-1" {
				t.Fatal("same-column reorder ignored preview")
			}
		})
	}
}

func TestMiddleCardLeavesNoHole(t *testing.T) {
	f := seedBoard()
	f.add(model.Task{ID: "t-6", ProjectID: "local", ColumnID: "c-todo", Title: "第三张", Position: 3000, Version: 1})
	b := newBoard(f)
	b.variant = variants[1]
	defer b.stop()
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return b.task("t-6") != nil })
	settle(t, tt)
	first, _ := tt.Find("打开任务：把看板接上 SSE")
	middle, _ := tt.Find("打开任务：对齐设计规格的间距与字号")
	tt.Press(middle.X+31, middle.Y+23)
	tt.Move(middle.X+43, middle.Y+35)
	tt.Move(1100, 320)
	tt.Advance(time.Second)
	tt.Frame()
	bottom, _ := tt.Find("打开任务：第三张")
	if math.Abs(float64(bottom.Y-middle.Y)) > 2 {
		t.Fatalf("middle hole remained: %+v vs %+v", bottom, middle)
	}
	unchanged, _ := tt.Find("打开任务：把看板接上 SSE")
	if unchanged != first {
		t.Fatalf("preceding card moved: %+v -> %+v", first, unchanged)
	}
	ghost, _ := tt.Find("任务落位预览")
	if math.Abs(float64(ghost.H-middle.H)) > 1 {
		t.Fatal("ghost ignored dragged card height")
	}
	tt.Key(0, ui.KeyEscape)
	after, _ := tt.Find("打开任务：对齐设计规格的间距与字号")
	if math.Abs(float64(after.X-(1100-31))) > 2 || math.Abs(float64(after.Y-(320-23))) > 2 {
		t.Fatalf("cancel jumped from pointer preview: %+v", after)
	}
	tt.Release(1100, 320)
	tt.Advance(2 * time.Second)
	tt.Frame()
}
