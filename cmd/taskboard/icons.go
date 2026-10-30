package main

import "github.com/egoist/mygo/ui"

// lucide wraps Lucide-style 24×24 strokes so Icon picks up the text color.
func lucide(shapes string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + shapes + `</svg>`))
}

// autoRun is the project handoff switch (Lucide refresh-cw). One icon for
// both states: the label says whether it is on.
var autoRun = lucide(`<path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/><path d="M8 16H3v5"/>`)

// settingsSliders is the top-bar settings control (icon-only); sliders-horizontal
// stays legible at 16px where the dense gear outline muddied.
var settingsSliders = lucide(`<path d="M21 4h-7M3 4h7M21 12h-9M3 12h5M21 20h-5M3 20h9M14 2v4M8 10v4M16 18v4"/>`)

// Display controls depict the saved card density and board direction.
var cardFull = lucide(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M7 8h10M7 12h10M7 16h6"/>`)
var cardTitle = lucide(`<rect x="3" y="7" width="18" height="10" rx="2"/><path d="M7 12h10"/>`)
var columnsHorizontal = lucide(`<rect x="3" y="3" width="7" height="18" rx="2"/><rect x="14" y="3" width="7" height="18" rx="2"/>`)
var columnsVertical = lucide(`<rect x="3" y="3" width="18" height="7" rx="2"/><rect x="3" y="14" width="18" height="7" rx="2"/>`)

// approveCheck is the 评审-card hover control (same stroke family as settingsSliders).
var approveCheck = lucide(`<path d="M20 6 9 17l-5-5"/>`)

// taskPlus is the per-project hover「+」on a sidebar row.
var taskPlus = lucide(`<path d="M5 12h14"/><path d="M12 5v14"/>`)

// projectTrash is the per-project hover「删除项目」on a sidebar row.
var projectTrash = lucide(`<path d="M3 6h18"/><path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6"/><path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2"/><line x1="10" x2="10" y1="11" y2="17"/><line x1="14" x2="14" y1="11" y2="17"/>`)

var commentMore = lucide(`<circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/>`)

// herdrConversation is the direct link from a task card to its Codex terminal.
var herdrConversation = lucide(`<path d="M21 11.5a8.38 8.38 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8 8.5 8.5 0 0 1 4.7-7.6 8.38 8.38 0 0 1 3.8-.9h.5a8.48 8.48 0 0 1 8 8z"/>`)

// Herdr execution states use compact, colored status icons on task cards.
var herdrSubmitting = lucide(`<path d="M20 7v5h-5"/><path d="M4 17v-5h5"/><path d="M5.64 9A8 8 0 0 1 19 7l1 5M4 12l1 5a8 8 0 0 0 13.36-2"/>`)
var herdrRunning = lucide(`<polygon points="6 3 20 12 6 21 6 3"/>`)
var herdrWaiting = lucide(`<circle cx="12" cy="12" r="10"/><path d="M12 6v6l4 2"/>`)
var herdrReturned = lucide(`<circle cx="12" cy="12" r="10"/><path d="m8 12 2.5 2.5L16 9"/>`)
var herdrFailed = lucide(`<circle cx="12" cy="12" r="10"/><path d="m15 9-6 6M9 9l6 6"/>`)
var herdrDisconnected = lucide(`<path d="M2 8.82a15 15 0 0 1 20 0M5 12.86a10 10 0 0 1 5.17-2.71M8.5 16.43a5 5 0 0 1 2.23-1.32M12 20h.01"/><path d="m2 2 20 20"/>`)
var herdrDetached = lucide(`<path d="m18 13 3.5-3.5a5 5 0 0 0-7.07-7.07L11 5.8"/><path d="m6 11-3.5 3.5a5 5 0 0 0 7.07 7.07L13 18.2"/><path d="m8 8 8 8"/>`)
var herdrReleased = lucide(`<rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 9.9-1M12 16v2"/>`)

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
