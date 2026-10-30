//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"strings"

	"taskboard-native/internal/executor"
)

// openCodexSession focuses the card's existing session in Herdr or Ghostty.
// Arguments are passed directly to subprocesses; no shell parses user data.
func openCodexSession(workspace, sessionID string) error {
	workspace = strings.TrimSpace(workspace)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("项目工作目录或 Codex 会话 ID 缺失")
	}
	if strings.HasPrefix(sessionID, "herdr:") {
		terminal := strings.TrimPrefix(sessionID, "herdr:")
		if terminal == "" {
			return fmt.Errorf("Herdr terminal ID 缺失")
		}
		binary, err := executor.ResolveHerdrApp()
		if err != nil {
			return fmt.Errorf("无法在 Herdr 中定位 Codex 对话：%w", err)
		}
		cmd := exec.Command(binary, "do", "agent.focus", "--terminal", terminal)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("无法在 Herdr 中定位 Codex 对话：%w", err)
		}
		// agent.focus selects the terminal inside Herdr, but does not activate
		// Herdr.app on macOS. Reopen by bundle ID so the selected conversation
		// is visible to the person who invoked this action from Taskboard.
		if err := exec.Command("open", "-b", "dev.herdr.app").Run(); err != nil {
			return fmt.Errorf("无法将 Herdr 窗口置于前台：%w", err)
		}
		return nil
	}
	if workspace == "" {
		return fmt.Errorf("项目工作目录或 Codex 会话 ID 缺失")
	}
	cmd := exec.Command("open", "-a", "Ghostty", "--args", "--working-directory="+workspace, "-e", "codex", "resume", sessionID)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("无法启动 Ghostty：%w", err)
	}
	return nil
}
