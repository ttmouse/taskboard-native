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

// taskPlus is the per-project hover「+」on a sidebar row.
var taskPlus = lucide(`<path d="M5 12h14"/><path d="M12 5v14"/>`)

// taskCompose is the sidebar「新建任务」nav row (square + pen, like Grok「新对话»).
var taskCompose = lucide(`<path d="M12 3H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.375 2.625a1 1 0 0 1 3 3l-9.013 9.014a2 2 0 0 1-.853.505l-2.873.84a.5.5 0 0 1-.62-.62l.84-2.873a2 2 0 0 1 .506-.852z"/>`)

// searchGlass opens task search from the sidebar chrome.
var searchGlass = lucide(`<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>`)

// folderClosed sits before each project name in the sidebar.
var folderClosed = lucide(`<path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.64 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/>`)

// layersAll is the「全部项目」row icon (not a single folder).
var layersAll = lucide(`<path d="m12.83 2.18a2 2 0 0 0-1.66 0L2.6 6.08a1 1 0 0 0 0 1.83l8.58 3.91a2 2 0 0 0 1.66 0l8.58-3.9a1 1 0 0 0 0-1.83Z"/><path d="m22 17.65-9.17 4.16a2 2 0 0 1-1.66 0L2 17.65"/><path d="m22 12.65-9.17 4.16a2 2 0 0 1-1.66 0L2 12.65"/>`)

// houseHome is the breadcrumb's root crumb (Lucide house).
var houseHome = lucide(`<path d="M15 21v-8a1 1 0 0 0-1-1h-4a1 1 0 0 0-1 1v8"/><path d="M3 10a2 2 0 0 1 .709-1.528l7-5.999a2 2 0 0 1 2.582 0l7 5.999A2 2 0 0 1 21 10v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>`)

// chevronRight separates breadcrumb crumbs; chevronDown opens the board switcher.
var chevronRight = lucide(`<path d="m9 18 6-6-6-6"/>`)
var chevronDown = lucide(`<path d="m6 9 6 6 6-6"/>`)

// panelLeftClose collapses the project sidebar (Orca / Lucide panel-left-close).
var panelLeftClose = lucide(`<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M9 3v18"/><path d="m16 15-3-3 3-3"/>`)

// panelLeftOpen expands the project sidebar when collapsed.
var panelLeftOpen = lucide(`<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M9 3v18"/><path d="m14 9 3 3-3 3"/>`)
