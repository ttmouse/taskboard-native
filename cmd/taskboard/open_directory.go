package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// openProjectDirectory asks macOS Finder to open the project's workspace.
// The path is passed as a single argument, so project names and paths are not
// interpreted by a shell.
func openProjectDirectory(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("项目尚未设置工作目录")
	}
	if err := exec.Command("open", path).Start(); err != nil {
		return fmt.Errorf("启动 Finder 失败：%w", err)
	}
	return nil
}
