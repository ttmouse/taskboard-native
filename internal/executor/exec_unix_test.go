//go:build unix

package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecCommandFinderEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("TASKBOARD_EXEC_PROBE", "preserved")
	// Pi is installed outside Finder's PATH and uses env to find Node.
	for path, script := range map[string]string{
		".npm-global/bin/pi": "#!/usr/bin/env node\n",
		".local/bin/node":    "#!/bin/sh\nprintf '%s\\n' \"$TASKBOARD_EXEC_PROBE\" \"$PWD\" \"$@\"\n",
	} {
		full := filepath.Join(home, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cmd, err := execCommand(context.Background(), home, "任务 with spaces; $(literal)", RuntimePi)
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Finder launch failed: %v: %s", err, out)
	}
	for _, want := range []string{"preserved\n", home + "\n", "--print\n--\n任务 with spaces; $(literal)\n"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output %q does not contain %q", out, want)
		}
	}
	if os.Getenv("PATH") != "/usr/bin:/bin" {
		t.Fatal("changed the parent process PATH")
	}
}
