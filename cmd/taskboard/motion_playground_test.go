//go:build motionplayground

package main

import (
	"math"
	"taskboard-native/internal/model"
	"testing"
	"time"
)

func configureMotion(b *board, variant string) { b.variant = variant }
func testMotionVariants() []string             { return variants[:2] }

func TestSingleCardLiftTravelLand(t *testing.T) {
	b := newBoard(seedBoard())
	b.variant = variants[1]
	defer b.stop()
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
	settle(t, tt)
	label := "打开任务：把看板接上 SSE"
	source, _ := tt.Find(label)
	card := *b.task("t-1")
	card.ColumnID = "c-doing"
	card.Version++
	b.post(func() {
		b.singleDemo = true
		b.applyEvent(model.Event{Type: model.EventTaskMoved, ProjectID: b.cur, By: b.agent, Task: &card})
	})
	tt.Frame()
	first, ok := tt.Find(label)
	if !ok || math.Abs(float64(first.X-source.X)) > 1 || math.Abs(float64(first.Y-source.Y)) > 1 {
		t.Fatalf("lift start jumped: %+v -> %+v", source, first)
	}
	tt.Advance(120 * time.Millisecond)
	lifted, _ := tt.Find(label)
	if lifted.Y >= source.Y-3 || lifted.W <= source.W {
		t.Fatalf("card did not lift/scale: %+v -> %+v", source, lifted)
	}
	if math.Abs(float64((lifted.X+lifted.W/2)-(source.X+source.W/2))) > 1 {
		t.Fatal("horizontal travel started before lift finished")
	}
	if b.phase != "① 抬升 · 卡片离开面板" {
		t.Fatal(b.phase)
	}
	tt.Advance(48 * time.Millisecond)
	tt.Advance(200 * time.Millisecond)
	moving, _ := tt.Find(label)
	if moving.X <= source.X+20 {
		t.Fatalf("card failed to travel: %+v", moving)
	}
	if b.phase != "② 悬浮移动 · 保持高度" {
		t.Fatal(b.phase)
	}
	tt.Advance(360 * time.Millisecond)
	if b.phase != "③ 落稳 · 阴影收拢" {
		t.Fatal(b.phase)
	}
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	if len(b.liftedFlights) != 0 {
		t.Fatal("flight did not finish")
	}
	landed, _ := tt.Find(label)
	// Anchor to a card already in 进行中 — same left edge as the landed card.
	peer, ok := tt.Find("打开任务：CLI 与 MCP 写入立刻反映到窗口")
	if !ok {
		t.Fatal("missing destination-column peer card")
	}
	if math.Abs(float64(landed.X-peer.X)) > 2 || math.Abs(float64(landed.W-source.W)) > 1 {
		t.Fatalf("did not restore destination geometry: landed %+v peer %+v", landed, peer)
	}
}
