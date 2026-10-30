//go:build unix

package executor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// execCommand builds the selected headless process for one task run. On unix the
// child gets its own process group so cancel terminates the whole tree
// (Node launchers can spawn helpers); Cancel sends SIGTERM to the
// group and WaitDelay escalates to the context's kill.
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
	// Finder supplies a minimal PATH. Resolving the CLI itself is insufficient:
	// its /usr/bin/env node launcher and the tools it runs need one too.
	paths := filepath.SplitList(os.Getenv("PATH"))
	paths = append(paths, filepath.Dir(bin))
	if home, err := os.UserHomeDir(); err == nil {
		for _, sub := range []string{".local/bin", ".npm-global/bin", "go/bin"} {
			paths = append(paths, filepath.Join(home, sub))
		}
	}
	paths = append(paths, "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin")
	cmd.Env = append(cmd.Environ(), "PATH="+strings.Join(paths, string(os.PathListSeparator)))
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
