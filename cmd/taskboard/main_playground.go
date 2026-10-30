//go:build motionplayground

// PROTOTYPE: a separate native app and scratch HTTP/SSE service.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"taskboard-native/internal/client"
	"taskboard-native/internal/service"
)

func main() {
	variant := flag.String("variant", "B", "animation variant: A, B, C")
	auto := flag.Bool("autoplay", false, "play the API scenario on launch")
	compare := flag.Bool("compare", false, "play A, B and C with identical API timings")
	single := flag.Bool("single", false, "show one card lifting, moving and settling via API")
	flag.Parse()
	mygo.App.SetName("Taskboard Motion Playground")
	dir, err := os.MkdirTemp("", "taskboard-motion-playground-")
	if err != nil {
		log.Fatal(err)
	}
	srv, err := service.Start("127.0.0.1:0", filepath.Join(dir, "prototype.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer srv.Close()
	b := newBoard(client.New(&client.Discover{Addr: srv.Addr}, "playground"))
	b.variant = variantName(*variant)
	b.autoStart, b.compare = *auto || *compare, *compare
	b.singleAuto = *single
	if *single {
		b.autoStart, b.compare = false, false
	}
	if *compare {
		b.variant = variants[0]
	}
	b.phase = "等待播放 · 临时数据"
	fmt.Printf("Playground API: %s\nScratch database: %s\n", srv.URL, srv.DBPath)
	mygo.App.WhenReady(func() {
		syncDisplayScale()
		b.win = mygo.NewWindow(mygo.WindowOptions{
			Title: "Motion Playground · 待验收", Width: 1280, Height: 800,
			MinWidth: 1060, MinHeight: 600, Content: ui.View(b.view),
		})
	})
	mygo.App.OnWindowAllClosed(func() {})
	defer b.stop()
	defer b.stopDemo()
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
