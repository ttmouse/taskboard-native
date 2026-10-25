//go:build unix

package executor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// dshFallbackPaths are probed when dsh is not on PATH — the macOS GUI app
// launches from Finder with a minimal PATH that misses ~/.npm-global/bin.
var dshFallbackPaths = []string{
	".npm-global/bin/dsh", // resolved against the home directory
	"/usr/local/bin/dsh",
	"/opt/homebrew/bin/dsh",
}

// resolveBin finds the dsh executable: PATH first, then the well-known
// install locations.
func resolveBin() (string, error) {
	if p, err := exec.LookPath(RuntimeDSH); err == nil {
		return p, nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, p := range dshFallbackPaths {
			if filepath.IsAbs(p) {
				if info, err := os.Stat(p); err == nil && !info.IsDir() {
					return p, nil
				}
				continue
			}
			cand := filepath.Join(home, p)
			if info, err := os.Stat(cand); err == nil && !info.IsDir() {
				return cand, nil
			}
		}
	}
	return "", errors.New("dsh not found on PATH or in known install locations")
}

// execCommand builds the dsh headless process for one task run. On unix the
// child gets its own process group so cancel terminates the whole tree
// (dsh is a node launcher that spawns helpers); Cancel sends SIGTERM to the
// group and WaitDelay escalates to the context's kill.
func execCommand(ctx context.Context, dir, taskText string) (*exec.Cmd, error) {
	bin, err := resolveBin()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, bin, "headless", taskText)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		}
		return os.ErrProcessDone
	}
	cmd.WaitDelay = 5 * time.Second
	return cmd, nil
}
