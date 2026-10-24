//go:build !motionplayground

package main

import (
	"time"

	"github.com/egoist/mygo/ui"
	"taskboard-native/internal/model"
)

// Experimental motion is deliberately absent from production builds.
type motionState struct{}

func (b *board) dragSourceOpacity() float32 { return 0.4 }

func (b *board) columnDropStyle(panel ui.Element, t *ui.Theme, over bool) {
	if over {
		panel.Border(2, t.Accent)
	}
}

func (b *board) topBar(c *ui.Context, t *ui.Theme)             { b.productionTopBar(c, t) }
func (b *board) clearMotion()                                  {}
func (b *board) forgetMotion(string)                           {}
func (b *board) noteMove(model.Task, model.Task)               {}
func (b *board) rememberDrop(string, ui.Element)               {}
func (b *board) previewMovePosition(*model.Task, string, bool) {}
func (b *board) dropWindowY(_ ui.Element, y float32) float32   { return y }

func (b *board) cardView(c *ui.Context, t *ui.Theme, task *model.Task) {
	b.taskCard(c, t, task).Transition(ui.ElementTransition{
		Duration: 200 * time.Millisecond, Ease: ui.EaseOut,
		Position: true, Size: true, Colors: true,
		Enter: &ui.Motion{Collapse: true, Scale: 0.98},
		Exit:  &ui.Motion{Collapse: true, Opacity: 0},
		Match: cardMatch{ID: task.ID},
	})
}

func (b *board) dropProjected(string, *model.Column, ui.Element) bool { return false }

func (b *board) columnCards(c *ui.Context, t *ui.Theme, col *model.Column, tasks []model.Task, panel, list ui.Element, dragID string, over bool, dropLine int) {

	if len(tasks) == 0 {
		ui.Box(c).FillWidth().Padding(t.Space(4), t.Space(4)).Gap(t.Space(1)).Children(func() {
			ui.Text(c, "还没有任务").FontSize(fsm(t)).TextColor(t.TextMuted)
			ui.Text(c, "从上方新建，或拖入卡片").FontSize(fsm(t)).TextColor(t.TextMuted)
		})
		return
	}
	for i := range tasks {
		if i == dropLine {
			b.dropLine(c, t)
		}
		b.cardView(c, t, &tasks[i])
	}
	if dropLine == len(tasks) {
		b.dropLine(c, t)
	}
}
