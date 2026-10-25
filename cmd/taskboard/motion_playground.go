//go:build motionplayground

package main

import (
	"github.com/egoist/mygo/ui"
	"strings"
	"taskboard-native/internal/model"
	"time"
)

func (b *board) singleDemoMode() bool { return b.singleDemo }
func (b *board) motionPhase(text string) {
	if b.singleDemo {
		b.phase = text
	}
}
func (b *board) motionAnimation(t *ui.Theme, task *model.Task) ui.Animation {
	animation := ui.Animation{Spring: ui.SpringOptions{Duration: 420 * time.Millisecond, Bounce: 0.04}}
	switch b.variant {
	case variants[0]:
		animation = ui.Animation{Duration: 260 * time.Millisecond, Ease: ui.EaseOut}
	case variants[2]:
		animation = ui.Animation{Spring: ui.SpringOptions{Duration: 480 * time.Millisecond, Bounce: 0.12}}
		for i, suffix := range []string{"A", "B", "C"} {
			if strings.HasSuffix(task.Title, suffix) {
				animation.Delay = time.Duration(i) * 55 * time.Millisecond
				break
			}
		}
	}
	return animation
}
