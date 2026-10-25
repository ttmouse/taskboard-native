//go:build !unix

package executor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

// dshFallbackPaths are probed when dsh is not on PATH.
var dshFallbackPaths = []string{
	"/usr/local/bin/dsh",
}

// resolveBin finds the dsh executable: PATH first, then fallbacks.
func resolveBin() (string, error) {
	if p, err := exec.LookPath(RuntimeDSH); err == nil {
		return p, nil
	}
	for _, p := range dshFallbackPaths {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("dsh not found on PATH or in known install locations")
}

// execCommand builds the dsh headless process for one task run.
func execCommand(ctx context.Context, dir, taskText string) (*exec.Cmd, error) {
	bin, err := resolveBin()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, bin, "headless", taskText)
	cmd.Dir = dir
	return cmd, nil
}
