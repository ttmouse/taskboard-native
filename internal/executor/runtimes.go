package executor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	RuntimeDSH    = "dsh"
	RuntimeCodex  = "codex"
	RuntimePi     = "pi"
	RuntimeClaude = "claude"
)

// Runtime is the public channel metadata shared by the task menu and runner.
type Runtime struct{ ID, Label string }

type runtimeDefinition struct {
	Runtime
	args func(dir, text string) []string
}

// Add a channel here to reuse the dispatch lifecycle and expose it in the UI.
var runtimeDefinitions = []runtimeDefinition{
	{Runtime{RuntimeDSH, "DSH"}, nil}, // Uses the running desktop bridge, never a CLI.
	{Runtime{RuntimeCodex, "Codex"}, func(dir, text string) []string {
		return []string{"exec", "--cd", dir, "--sandbox", "workspace-write", "--skip-git-repo-check", "--color", "never", "--", text}
	}},
	{Runtime{RuntimePi, "Pi Agent"}, func(_, text string) []string { return []string{"--print", "--", text} }},
	// Claude Code is non-interactive: --print returns after one turn. The agent
	// needs Bash to drive the board CLI (tb) the skill requires; file edits are
	// accepted by acceptEdits. Session flags are added per run in prepareCLI.
	{Runtime{RuntimeClaude, "Claude Code"}, func(_, text string) []string {
		return []string{"--print", "--permission-mode", "acceptEdits", "--allowedTools", "Bash", "--", text}
	}},
}

func Runtimes() []Runtime {
	result := make([]Runtime, len(runtimeDefinitions))
	for i, definition := range runtimeDefinitions {
		result[i] = definition.Runtime
	}
	return result
}

// KnownRuntime reports whether id names a dispatch channel. Create-with-
// dispatch callers use it to reject a request before a card exists.
func KnownRuntime(id string) bool {
	_, ok := runtimeDefinitionFor(id)
	return ok
}

func runtimeDefinitionFor(id string) (runtimeDefinition, bool) {
	for _, definition := range runtimeDefinitions {
		if definition.ID == id {
			return definition, true
		}
	}
	return runtimeDefinition{}, false
}

// Finder's PATH omits user installations; resolve all channels consistently.
func resolveBin(runtime string) (string, error) {
	if _, ok := runtimeDefinitionFor(runtime); !ok {
		return "", fmt.Errorf("unknown runtime %q", runtime)
	}
	if path, err := exec.LookPath(runtime); err == nil {
		return path, nil
	}
	var paths []string
	if home, err := os.UserHomeDir(); err == nil {
		for _, dir := range []string{".local/bin", ".npm-global/bin", ".cargo/bin", "go/bin"} {
			paths = append(paths, filepath.Join(home, dir, runtime))
		}
	}
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin"} {
		paths = append(paths, filepath.Join(dir, runtime))
	}
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path, nil
		}
	}
	return "", fmt.Errorf("找不到 %s 命令，请在本机安装并登录对应 CLI", runtime)
}
