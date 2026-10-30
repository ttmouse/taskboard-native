package executor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveHerdrAppFindsUserLocalBinOutsideFinderPath(t *testing.T) {
	home := t.TempDir()
	command := filepath.Join(home, ".local", "bin", "herdr-app")
	if err := os.MkdirAll(filepath.Dir(command), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(command, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")

	cmd, err := herdrCommand(context.Background(), "--test-arg")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Path != command {
		t.Fatalf("herdrCommand() path = %q, want %q", cmd.Path, command)
	}
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
}

func TestHerdrCoreCommandsFindUserLocalBinOutsideFinderPath(t *testing.T) {
	home := t.TempDir()
	command := filepath.Join(home, ".local", "bin", "hc")
	if err := os.MkdirAll(filepath.Dir(command), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(command, []byte("#!/bin/sh\nif [ \"$1\" = get ]; then printf '%s' '{\"status\":{\"state\":\"working\"}}'; fi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")

	status, err := herdrGet(context.Background(), "terminal-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := herdrState(status); got != "working" {
		t.Fatalf("herdrState() = %q, want working", got)
	}
	if err := herdrSend(context.Background(), "terminal-1", "follow up"); err != nil {
		t.Fatal(err)
	}
}
