package main

import "github.com/egoist/mygo/ui"

// lucide wraps Lucide-style 24×24 strokes so Icon picks up the text color.
func lucide(shapes string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + shapes + `</svg>`))
}

// settingsSliders is the top-bar settings control (icon-only); sliders-horizontal
// stays legible at 16px where the dense gear outline muddied.
var settingsSliders = lucide(`<path d="M21 4h-7M3 4h7M21 12h-9M3 12h5M21 20h-5M3 20h9M14 2v4M8 10v4M16 18v4"/>`)

// approveCheck is the 评审-card hover control (same stroke family as settingsSliders).
var approveCheck = lucide(`<path d="M20 6 9 17l-5-5"/>`)

// taskPlus is the top-bar 新建任务 control (distinct from sidebar project "+").
var taskPlus = lucide(`<path d="M5 12h14"/><path d="M12 5v14"/>`)
