//go:build !unix

package executor

import (
	"context"
	"os/exec"
)

// execCommand builds the selected headless process for one task run.
func execCommand(ctx context.Context, dir, taskText, runtime string, configPaths ...string) (*exec.Cmd, error) {
	bin, err := resolveBin(runtime)
	if err != nil {
		return nil, err
	}
	args, err := runtimeArgs(dir, taskText, runtime, configPaths...)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	return cmd, nil
}
