package executor

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPiUsesPinnedModelWithoutChangingDefaults(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "runtime.json")
	for _, model := range []string{"zhipu/glm-5.3-flash", "codex/gpt-6-luna"} {
		config := `{"pi":{"provider":"magpie","model":"` + model + `"}}`
		if err := os.WriteFile(profile, []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
		args, err := runtimeArgs("dir", "literal task --model other", RuntimePi, profile)
		want := []string{"--print", "--provider", "magpie", "--model", model, "--", "literal task --model other"}
		if err != nil || !reflect.DeepEqual(args, want) {
			t.Fatalf("args=%q err=%v, want %q", args, err, want)
		}
		after, _ := os.ReadFile(profile)
		if string(after) != config {
			t.Fatal("model selection modified configuration")
		}
	}
	if err := os.WriteFile(profile, []byte(`{"pi":{"provider":"magpie"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeArgs("dir", "task", RuntimePi, profile); err == nil {
		t.Fatal("accepted a Pi connection without a model")
	}
}

func TestRuntimeConnectionExecutesWithScopedProfile(t *testing.T) {
	installFakeRuntime(t, RuntimeCodex, `printf '%s\n' "$@"`)
	dir := t.TempDir()
	profile := filepath.Join(dir, "runtime.json")
	const config = `{"codex":{"baseUrl":"http://127.0.0.1:3425/v1","model":"codex/gpt-6-luna","reasoningEffort":"medium"}}`
	if err := os.WriteFile(profile, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	prompt := "任务中文; $(literal)"
	cmd, err := execCommand(context.Background(), dir, prompt, RuntimeCodex, profile)
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`--ignore-user-config`, `model="codex/gpt-6-luna"`, `model_providers.taskboard.base_url="http://127.0.0.1:3425/v1"`, `model_reasoning_effort="medium"`, prompt} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("child missing %q: %s", want, out)
		}
	}
	after, err := os.ReadFile(profile)
	if err != nil || string(after) != config {
		t.Fatal("executing a task modified its profile")
	}
}

func TestInvalidRuntimeConnectionDoesNotLaunchCLI(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "launched")
	t.Setenv("RUNTIME_PROFILE_LAUNCH_MARKER", marker)
	installFakeRuntime(t, RuntimeCodex, `printf 'unexpected' > "$RUNTIME_PROFILE_LAUNCH_MARKER"`)
	profile := filepath.Join(t.TempDir(), "runtime.json")
	for _, body := range []string{`{`, `{"codex":{}}`, `{"codex":{"baseUrl":"file:///tmp/config","model":"model"}}`, `{"codex":{"baseUrl":"http://127.0.0.1:3425/v1?token=invalid","model":"model"}}`} {
		if err := os.WriteFile(profile, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := execCommand(context.Background(), t.TempDir(), "task", RuntimeCodex, profile); err == nil {
			t.Fatalf("accepted invalid connection: %s", body)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("invalid connection started a CLI")
	}
	// A broken Codex profile must not block a different runtime.
	if _, err := runtimeArgs("dir", "task", RuntimePi, profile); err != nil {
		t.Fatal(err)
	}
}
