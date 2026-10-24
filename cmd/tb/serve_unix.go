//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// waitForSignal blocks until SIGINT or SIGTERM.
func waitForSignal() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	signal.Stop(ch)
}

// detachedAttr makes the background service survive its parent's exit.
func detachedAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
