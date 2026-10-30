package main

import (
	"strings"

	"taskboard-native/internal/executor"
)

// justTypedMention reports that the draft ends with an @ that starts a word:
// that is the user asking to address an agent. An @ inside prose (an email
// address, a quoted handle) never matches, so text can never route work.
func justTypedMention(s string) bool {
	if !strings.HasSuffix(s, "@") {
		return false
	}
	before := strings.TrimSuffix(s, "@")
	return before == "" || strings.HasSuffix(before, " ") || strings.HasSuffix(before, "\n")
}

// runtimeLabel resolves a channel id to the label the menus use.
func runtimeLabel(id string) string {
	for _, channel := range executor.Runtimes() {
		if channel.ID == id {
			return channel.Label
		}
	}
	return id
}

// commentPlaceholder tells the composer whether sending a comment will
// continue the card's original session or an explicitly addressed channel.
func commentPlaceholder(mention string, autoFollowup bool, originalRuntime string) string {
	if mention == "" {
		if autoFollowup {
			return "写评论…发送后由 " + runtimeLabel(originalRuntime) + " 跟进"
		}
		return "写评论… 输入 @ 可指定跟进给谁"
	}
	return "发送后交给 " + runtimeLabel(mention) + " 跟进…"
}
