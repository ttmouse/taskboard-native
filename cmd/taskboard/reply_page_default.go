//go:build !motionplayground

package main

import "github.com/egoist/mygo/ui"

// replyDemoPage is the Playground-only static reply sample; production has
// no such page and the board pane always renders normally.
func (b *board) replyDemoPage(c *ui.Context, t *ui.Theme) bool { return false }
