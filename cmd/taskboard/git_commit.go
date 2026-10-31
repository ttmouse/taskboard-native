package main

import (
	"fmt"
	"os/exec"
	"strings"

	"taskboard-native/internal/model"
)

// maybeGitCommitOnDone runs after a card successfully lands in the done
// column when the project's 「验收完成时自动 Git 提交」toggle is on.
func (b *board) maybeGitCommitOnDone(task model.Task) {
	if b.prefs.ProjectGitCommitOnDone == nil || !b.prefs.ProjectGitCommitOnDone[task.ProjectID] {
		return
	}
	workspace := ""
	for _, p := range b.projects {
		if p.ID == task.ProjectID {
			workspace = strings.TrimSpace(p.WorkspacePath)
			break
		}
	}
	if workspace == "" {
		b.toast("Git 提交跳过", "项目未设置工作目录")
		return
	}
	msg := fmt.Sprintf("#%d %s", task.Number, strings.TrimSpace(task.Title))
	if msg == fmt.Sprintf("#%d", task.Number) {
		msg = fmt.Sprintf("#%d 验收完成", task.Number)
	}
	taskID := task.ID
	go func() {
		sha, err := gitCommitTracked(workspace, msg)
		b.post(func() {
			if err != nil {
				b.toast("Git 提交失败", err.Error())
				return
			}
			if sha == "" {
				b.toast("已完成", "工作区无已跟踪改动，未创建提交")
				return
			}
			short := sha
			if len(short) > 7 {
				short = short[:7]
			}
			b.toast("已提交 Git", short)
			go func() {
				_, _ = b.api.AddComment(taskID, "验收完成自动提交 "+sha)
			}()
		})
	}()
}

// gitCommitTracked stages tracked modifications (`git add -u`) and commits.
// Untracked files are left alone. Returns empty sha when the tree was clean.
func gitCommitTracked(workspace, message string) (string, error) {
	if out, err := exec.Command("git", "-C", workspace, "rev-parse", "--is-inside-work-tree").CombinedOutput(); err != nil {
		return "", fmt.Errorf("不是 Git 仓库：%s", strings.TrimSpace(string(out)))
	}
	status, err := exec.Command("git", "-C", workspace, "status", "--porcelain").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git status 失败：%s", strings.TrimSpace(string(status)))
	}
	lines := strings.Split(string(status), "\n")
	hasTracked := false
	for _, line := range lines {
		if len(line) < 2 {
			continue
		}
		// porcelain: XY PATH — untracked is "??"; skip those for add -u intent check
		xy := line[:2]
		if xy == "??" {
			continue
		}
		hasTracked = true
		break
	}
	if !hasTracked {
		return "", nil
	}
	if out, err := exec.Command("git", "-C", workspace, "add", "-u").CombinedOutput(); err != nil {
		return "", fmt.Errorf("git add -u 失败：%s", strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("git", "-C", workspace, "commit", "-m", message).CombinedOutput(); err != nil {
		return "", fmt.Errorf("git commit 失败：%s", strings.TrimSpace(string(out)))
	}
	sha, err := exec.Command("git", "-C", workspace, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("读 HEAD 失败：%s", strings.TrimSpace(string(sha)))
	}
	return strings.TrimSpace(string(sha)), nil
}
