//go:build windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// waitForSignal blocks until Ctrl+C.
func waitForSignal() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	<-ch
	signal.Stop(ch)
}

// detachedAttr detaches the background service from this console.
func detachedAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x00000008, // DETACHED_PROCESS
	}
}
