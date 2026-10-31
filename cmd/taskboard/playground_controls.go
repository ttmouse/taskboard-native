//go:build motionplayground

package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"taskboard-native/internal/model"
)

var variants = []string{"A · 快速缓出", "B · 柔和弹簧", "C · 错峰弹簧"}

type playgroundState struct {
	singleDemo, singleAuto      bool
	variant, phase              string
	running, autoStart, compare bool
	replyDemo                   bool // static review page for comment replies
	demoCancel                  context.CancelFunc
	nextStart                   time.Time
	run                         int
}

func variantName(id string) string {
	for _, name := range variants {
		if strings.HasPrefix(name, id) {
			return name
		}
	}
	return variants[1]
}

func (b *board) topBar(c *ui.Context, t *ui.Theme) {
	if b.singleAuto && !b.loading && !b.running {
		b.singleAuto = false
		b.startSingleDemo()
	}
	if b.autoStart && !b.loading && !b.running && !c.Now().Before(b.nextStart) {
		b.autoStart = false
		b.startDemo()
	}
	ui.Column(c).Padding(t.Space(4), t.Space(5)).Gap(t.Space(2)).Children(func() {
		ui.Row(c).FillWidth().Gap(t.Space(3)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Motion Playground").FontSize(flg(t)).FontWeight(600)
			// The playground is its own app, so it carries the build version on
			// screen instead of inheriting the production footer.
			ui.Text(c, playgroundVersion).FontSize(fxs(t)).TextColor(t.TextMuted)
			ui.Spacer(c)
			for _, name := range variants {
				button := ui.Button(c, name).Label("选择方案 " + name).Disabled(b.running)
				if b.variant == name {
					button.Background(t.Accent.Alpha(0.14)).FontWeight(600)
				}
				if button.Clicked() {
					b.variant = name
					b.clearMotion()
					b.phase = "等待播放 · 临时数据"
				}
			}
			if ui.PrimaryButton(c, "播放同一场景").Disabled(b.loading || b.running).Clicked() {
				b.compare = false
				b.startDemo()
			}
			if ui.Button(c, "停止").Disabled(!b.running).Clicked() {
				b.compare = false
				b.autoStart = false
				b.stopDemo()
				b.phase = "已停止"
			}
		})
		ui.Row(c).FillWidth().Gap(t.Space(3)).Children(func() {
			if ui.Button(c, "单卡 · 抬升 → 移动 → 落稳").Disabled(b.loading || b.running).Clicked() {
				b.compare, b.autoStart = false, false
				b.startSingleDemo()
			}
			if ui.Button(c, "评论回复样张").Label("打开评论回复静态样张").Clicked() {
				b.stopDemo()
				b.replyDemo = true
			}
			ui.Text(c, b.variant+"  ·  "+b.motionDescription()).FontSize(fsm(t)).TextColor(t.TextMuted)
			ui.Spacer(c)
			ui.Text(c, b.phase).FontSize(fsm(t)).TextColor(t.TextMuted)
		})
	})
}

func (b *board) motionDescription() string {
	switch b.variant {
	case variants[0]:
		return "260ms ease-out · 无错峰"
	case variants[2]:
		return "480ms spring · 0.12 回弹 · 抬升错峰 0/40/80ms"
	default:
		return "420ms spring · 0.04 回弹 · 无错峰"
	}
}

func (b *board) stopDemo() {
	if b.demoCancel != nil {
		b.demoCancel()
		b.demoCancel = nil
	}
	b.running = false
	b.liftBatchAt = time.Time{}
	b.liftBatchCount = 0
	b.singleDemo, b.singleAuto = false, false
	b.run++ // stale callbacks from a stopped run cannot change controls
}

// All variants use identical HTTP requests and event timing. Variant C
// only staggers the visual motion; it does not queue or delay SSE data.
func (b *board) startDemo() {
	b.stopDemo()
	ctx, cancel := context.WithCancel(context.Background())
	b.demoCancel = cancel
	b.running = true
	b.phase = "准备相同场景…"
	run, variant := b.run, b.variant
	go func() {
		setPhase := func(text string) {
			b.post(func() {
				if b.run == run {
					b.phase = text
				}
			})
		}
		fail := func(err error) {
			b.post(func() {
				if b.run == run {
					b.running = false
					b.compare = false
					b.phase = "演示失败：" + err.Error()
				}
			})
		}
		board, err := b.api.Board("local")
		if err != nil {
			fail(err)
			return
		}
		// This API belongs solely to the scratch database created by main.
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
		start := time.Now()
		wait := func(offset time.Duration) bool {
			timer := time.NewTimer(max(time.Until(start.Add(offset)), 0))
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return false
			case <-timer.C:
				return true
			}
		}
		var cards [3]model.Task
		setPhase("三卡连续新增 · 180ms 间隔")
		for i, letter := range []string{"A", "B", "C"} {
			if !wait(600*time.Millisecond + time.Duration(i)*180*time.Millisecond) {
				return
			}
			cards[i], err = b.api.CreateTask("local", cols["待办"], "卡片 "+letter, "", "")
			if err != nil {
				fail(err)
				return
			}
		}
		move := func(i int, name string, offset time.Duration) error {
			if !wait(offset) {
				return ctx.Err()
			}
			current := cards[i]
			updated, e := b.api.Move(current.ID, current.Version, cols[name], "", true)
			if e == nil {
				cards[i] = updated
			}
			return e
		}
		wave := func(name string, offset time.Duration) error {
			var wg sync.WaitGroup
			errs := make(chan error, 3)
			for i := range cards {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					if e := move(i, name, offset); e != nil {
						errs <- e
					}
				}(i)
			}
			wg.Wait()
			close(errs)
			for e := range errs {
				return e
			}
			return nil
		}
		for i, name := range []string{"进行中", "评审", "完成"} {
			setPhase("三卡抬升错峰 0/40/80ms → " + name + " · 并行 API")
			if err = wave(name, 1600*time.Millisecond+time.Duration(i)*1400*time.Millisecond); err != nil {
				if ctx.Err() == nil {
					fail(err)
				}
				return
			}
		}
		setPhase("两卡同时回到不同状态")
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for i, name := range []string{"进行中", "评审"} {
			wg.Add(1)
			go func(i int, name string) {
				defer wg.Done()
				if e := move(i, name, 6*time.Second); e != nil {
					errs <- e
				}
			}(i, name)
		}
		wg.Wait()
		close(errs)
		for e := range errs {
			if ctx.Err() == nil {
				fail(e)
			}
			return
		}
		setPhase("并行回归 → 完成")
		if err = wave("完成", 7400*time.Millisecond); err != nil {
			if ctx.Err() == nil {
				fail(err)
			}
			return
		}
		if !wait(9 * time.Second) {
			return
		}
		b.post(func() {
			if b.run != run {
				return
			}
			b.running = false
			b.phase = "播放完成 · 等待验收"
			fmt.Printf("PLAYGROUND_COMPLETE %s\n", variant)
			if b.compare {
				for i, name := range variants {
					if name == variant && i+1 < len(variants) {
						b.variant = variants[i+1]
						b.autoStart = true
						b.nextStart = b.frameCtx.Now().Add(1200 * time.Millisecond)
						b.frameCtx.After(1200 * time.Millisecond)
						return
					}
				}
				b.compare = false
			}
		})
	}()
}
