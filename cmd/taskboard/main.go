//go:build !motionplayground

// Command taskboard opens the native kanban window. It starts the local
// service in-process, then talks to it only over HTTP and SSE: the window
// holds no database handle and never reads state that did not come from
// the service.
//
//	taskboard                 open the window (starting the service)
//	taskboard serve --port N  run the service alone
//
// The service listens on TASKBOARD_ADDR (127.0.0.1:47826 by default) and
// keeps its SQLite file at TASKBOARD_DB, then mygo's user-data directory.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"taskboard-native/internal/client"
	"taskboard-native/internal/service"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "serve":
			os.Exit(runServe(os.Args[2:]))
		case "help", "-h", "--help":
			fmt.Print(usage)
			return
		default:
			fmt.Fprintf(os.Stderr, "未知参数 %q\n\n%s", os.Args[1], usage)
			os.Exit(2)
		}
	}
	runWindow()
}

const usage = `taskboard — 原生看板窗口

  taskboard                 打开看板窗口（内建启动本地服务）
  taskboard serve [--port N] [--addr A] [--db PATH]
                            只跑本地服务（供 CLI / MCP 使用）

环境变量：TASKBOARD_ADDR（默认 127.0.0.1:47826）、TASKBOARD_DB、TASKBOARD_SLOW。
`

// runServe runs the HTTP service in the foreground, as `tb serve` does.
func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", "", "监听地址（默认 TASKBOARD_ADDR 或 127.0.0.1:47826）")
	port := fs.Int("port", 0, "监听端口（0 = 自动）")
	db := fs.String("db", "", "数据库路径（默认 TASKBOARD_DB 或用户数据目录）")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	listen := *addr
	if listen == "" && *port > 0 {
		listen = fmt.Sprintf("127.0.0.1:%d", *port)
	}
	srv, err := service.Start(listen, *db)
	if err != nil {
		fmt.Fprintln(os.Stderr, "启动失败：", err)
		return 1
	}
	defer srv.Close()
	fmt.Printf("taskboard 服务：%s（数据库 %s）\n", srv.URL, srv.DBPath)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sig:
	case <-srv.Done():
	}
	return 0
}

// runWindow starts the service, opens the board window and runs the loop.
func runWindow() {
	mygo.App.SetName("taskboard-native")

	// Signed updates come from the GitHub releases of updates.github in
	// mygo.json, so the window draws in native UI and needs no webview.
	mygo.Use(native.Plugin)

	// One instance owns the data file and the port.
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}

	srv, err := service.Start("", "")
	addr := ""
	if err != nil {
		// A separately launched `tb serve` already owns the default port.
		// Reuse that service instead of silently exiting; it is the sole
		// writer for the shared database, so do not start a second server.
		if !errors.Is(err, syscall.EADDRINUSE) {
			log.Fatal(err)
		}
		addr = service.DefaultListenAddr()
	} else {
		defer srv.Close()
		addr = srv.Addr
	}

	// The window signs every write with the saved 显示名, or with gui when
	// the user has not set one; newBoard keeps the client in step from here.
	cl := client.New(&client.Discover{Addr: addr}, loadUIPrefs().windowAgent())
	b := newBoard(cl)
	// The saved appearance (uiprefs' appearance) asks the whole app for it
	// before the window exists, so the first frame and the window chrome
	// open in it instead of flashing the desktop's appearance. Following the
	// desktop needs no call: it is mygo's default.
	if b.prefs.Appearance != appearSystem {
		mygo.Theme.SetSource(b.prefs.Appearance.source())
	}
	// TitleBarHiddenInset + empty Title: no macOS title text.
	// Traffic lights sit in the sidebar's top rail (Grok Bolt–style).
	// Y≈14 centers ~12px lights in the 40 DIP (Space(10)) rail; X≈14 is
	// the close-button origin inside the sidebar — not the content bar.
	lights := &mygo.Point{X: 14, Y: 14}

	mygo.App.WhenReady(func() {
		// The display's backing scale decides how dense a pasted screenshot is
		// assumed to be; only the main thread can ask for it.
		syncDisplayScale()
		b.win = mygo.NewWindow(mygo.WindowOptions{
			Title:                "",
			TitleBarStyle:        mygo.TitleBarHiddenInset,
			TrafficLightPosition: lights,
			TitleBarHeight:       32, // page title-bar band (helps Linux/Win; mac uses position)
			Width:                1280,
			Height:               800,
			MinWidth:             960,
			MinHeight:            560,
			Content:              ui.View(b.view),
		})
		// The saved ⌘⇧N global shortcut needs a running app to bind, so it
		// registers here rather than at board construction. The settings
		// toggle re-runs the same sync while the window lives.
		b.syncGlobalHotkey()
	})
	// Closing the last window quits the app. Registering even an empty
	// OnWindowAllClosed listener keeps mygo running forever, which left a
	// window-less process holding 127.0.0.1:47826 and the SQLite writer
	// after the user had closed the board. With no listener mygo quits, and
	// the deferred srv.Close / b.stop release the port and the file.
	defer b.stop()
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
