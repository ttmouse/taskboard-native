//go:build motionplayground

package main

import (
	"context"
	"time"
)

// One real task, one real HTTP move, one SSE-driven flight. This service
// belongs to the Playground scratch database, never the production board.
func (b *board) startSingleDemo() {
	b.stopDemo()
	b.variant = variants[1]
	b.singleDemo, b.running = true, true
	b.phase = "准备单张卡片…"
	ctx, cancel := context.WithCancel(context.Background())
	b.demoCancel = cancel
	run := b.run
	go func() {
		fail := func(err error) {
			b.post(func() {
				if b.run == run {
					b.running = false
					b.singleDemo = false
					b.phase = "单卡演示失败：" + err.Error()
				}
			})
		}
		wait := func(d time.Duration) bool {
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return false
			case <-timer.C:
				return true
			}
		}
		board, err := b.api.Board("local")
		if err != nil {
			fail(err)
			return
		}
		for _, task := range board.Tasks {
			if ctx.Err() != nil {
				return
			}
			if _, err = b.api.Archive(task.ID, task.Version); err != nil {
				fail(err)
				return
			}
		}
		cols := map[string]string{}
		for _, col := range board.Columns {
			cols[col.Name] = col.ID
		}
		if ctx.Err() != nil {
			return
		}
		card, err := b.api.CreateTask("local", cols["待办"], "一张卡片的旅程", "抬升 → 悬浮移动 → 落稳", "")
		if err != nil {
			fail(err)
			return
		}
		b.post(func() {
			if b.run == run {
				b.phase = "起点 · 即将抬升"
			}
		})
		if !wait(900 * time.Millisecond) {
			return
		}
		if _, err = b.api.Move(card.ID, card.Version, cols["进行中"], "", true); err != nil {
			fail(err)
			return
		}
		if !wait(1500 * time.Millisecond) {
			return
		}
		b.post(func() {
			if b.run == run {
				b.running = false
				b.singleDemo = false
				b.phase = "单卡演示完成 · 点击单卡按钮重播"
			}
		})
	}()
}
