//go:build !motionplayground

package main

import (
	"github.com/egoist/mygo/ui"
	"taskboard-native/internal/model"
	"time"
)

// Production uses the accepted B spring and shares the motion engine
// with the Playground, without its temporary service or demo controls.
type playgroundState struct{}

func (b *board) topBar(c *ui.Context, t *ui.Theme) { b.productionTopBar(c, t) }
func (b *board) singleDemoMode() bool              { return false }
func (b *board) motionPhase(string)                {}
func (b *board) motionAnimation(_ *ui.Theme, _ *model.Task) ui.Animation {
	return ui.Animation{Spring: ui.SpringOptions{Duration: 420 * time.Millisecond, Bounce: 0.04}}
}
