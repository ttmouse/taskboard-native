package main

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"taskboard-native/internal/client"
	"taskboard-native/internal/model"
	"taskboard-native/internal/service"
)

// Exercise the production renderer through the real parallel HTTP/SSE
// path, using an isolated database rather than the user's board.
func TestMotionParallelHTTPAndSSERetarget(t *testing.T) {
	srv, err := service.Start("127.0.0.1:0", filepath.Join(t.TempDir(), "motion.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	cl := client.New(&client.Discover{Addr: srv.Addr}, "motion-regression")
	// A newly opened service starts projectless; seed the legacy「local」
	// board this regression drives.
	proj, err := cl.CreateProject("local")
	if err != nil {
		t.Fatal(err)
	}
	bd, err := cl.Board(proj.ID)
	if err != nil {
		t.Fatal(err)
	}
	cols := map[string]string{}
	for _, col := range bd.Columns {
		cols[col.Name] = col.ID
	}
	cards := make([]model.Task, 3)
	for i, name := range []string{"并行卡 A", "并行卡 B", "并行卡 C"} {
		cards[i], err = cl.CreateTask(proj.ID, cols["待办"], name, "", "")
		if err != nil {
			t.Fatal(err)
		}
	}
	b := newBoard(cl)
	defer b.stop()
	configureMotion(b, "B · 柔和弹簧")
	tt := newBoardTester(b)
	waitFor(t, tt, "真实 SSE 连接", func() bool { return !b.loading && b.status == client.Online && b.task(cards[2].ID) != nil })
	settle(t, tt)
	source, _ := tt.Find("打开任务：" + cards[0].Title)
	type result struct {
		index int
		task  model.Task
		err   error
	}
	results := make(chan result, 3)
	start := make(chan struct{})
	for i, card := range cards {
		go func(i int, card model.Task) {
			<-start
			task, e := cl.Move(card.ID, card.Version, cols["进行中"], "", true)
			results <- result{i, task, e}
		}(i, card)
	}
	close(start)
	for range cards {
		r := <-results
		if r.err != nil {
			t.Fatal(r.err)
		}
		cards[r.index] = r.task
	}
	waitFor(t, tt, "三个并行 HTTP 移动事件", func() bool {
		for _, card := range cards {
			if b.task(card.ID).ColumnID != cols["进行中"] {
				return false
			}
		}
		return true
	})
	if len(b.liftedFlights) != 3 {
		t.Fatalf("parallel SSE produced %d flights", len(b.liftedFlights))
	}
	starts := map[time.Time]bool{}
	for _, f := range b.liftedFlights {
		starts[f.at] = true
	}
	if len(starts) != 3 {
		t.Fatal("SSE batch lost its visual stagger")
	}
	tt.Advance(360 * time.Millisecond)
	tt.Frame()
	before, _ := tt.Find("打开任务：" + cards[0].Title)
	cards[0], err = cl.Move(cards[0].ID, cards[0].Version, cols["等你"], "", true)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, "真实 SSE 中途改道", func() bool { return b.task(cards[0].ID).ColumnID == cols["等你"] })
	after, _ := tt.Find("打开任务：" + cards[0].Title)
	if math.Abs(float64(before.X-after.X)) > 0.02 || math.Abs(float64(before.Y-after.Y)) > 0.02 {
		t.Fatalf("SSE retarget jumped: %+v -> %+v", before, after)
	}
	tt.Advance(2 * time.Second)
	tt.Frame()
	if len(b.liftedFlights) != 0 || len(b.flights) != 0 {
		t.Fatal("HTTP/SSE flights survived landing")
	}
	for _, card := range cards {
		rect, ok := tt.Find("打开任务：" + card.Title)
		if !ok || math.Abs(float64(rect.W-source.W)) > 1 {
			t.Fatalf("card failed to land at normal size: %s %+v", card.Title, rect)
		}
	}
	server, err := cl.Board(proj.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"进行中", "等你"} {
		var serverIDs []string
		positions := map[string]float64{}
		for _, task := range server.Tasks {
			if task.ColumnID == cols[name] {
				serverIDs = append(serverIDs, task.ID)
				positions[task.ID] = task.Position
			}
		}
		local := b.tasksIn(cols[name])
		for i, task := range local {
			if task.Position != positions[task.ID] {
				t.Fatalf("landed position differs from API for %s: local %.0f, server %.0f", task.Title, task.Position, positions[task.ID])
			}
			if task.ID != serverIDs[i] {
				t.Fatalf("landed order differs from API in %s: local %v, server %v", name, local, serverIDs)
			}
		}
	}
}
