# Search

Task search opens an overlay that filters by title and description across columns without changing the background board's task lists. Empty query, no-results, and close/clear are distinct observable states.

## Sub-features

- `search-open` — open via `搜索任务` or ⌘F (waits while task detail is open).
- `search-match` — title and description hits listed in `任务搜索面板`; board columns underneath stay unfiltered.
- `search-empty` — blank query shows `输入标题或描述中的关键词`; miss shows `没有匹配的任务`.
- `search-open-result` — keyboard Down/Up + Enter opens `打开任务 …` / detail and clears query.
- `search-close` — `关闭搜索` or Escape dismisses and clears.

## How to get to it (user POV)

- Click `搜索任务` (sidebar row when expanded, or top-bar icon when collapsed).
- Press ⌘F while Taskboard is frontmost and no detail dialog is up.
- Type into the field labeled `搜索任务` (placeholder `搜索任务…`).

## Driving it with osascript+screencapture+tb CLI

Preconditions:

- Doctor exit 0; board has at least one known card title (seed or prior `tb add`).
- Unit references: `search_test.go`, `search_overlay_test.go`.
- Dismiss detail dialogs before ⌘F.

- **Open.** Activate Taskboard; click `搜索任务` or:

```bash
osascript -e 'tell application "Taskboard" to activate' \
  -e 'delay 0.2' \
  -e 'tell application "System Events" to keystroke "f" using command down'
```

- **EXPECTED open.** Accessibility finds `任务搜索面板` and input `搜索任务`.
- **Capture BEFORE query.** `$EVIDENCE_DIR/search-open.png`
- **OPERATION — match.** Type a known substring from an existing card title (e.g. part of a seed title). Prefer Accessibility-focused typing into `搜索任务`.
- **EXPECTED match.** Result rows appear (e.g. text / `打开任务 …` style hits from tests); background board task set unchanged (do not assert column filtering).
- **Capture AFTER.** `$EVIDENCE_DIR/search-hits.png`
- **Empty / miss.** Clear field → message `输入标题或描述中的关键词`; type nonsense → `没有匹配的任务`.
- **Close.** Click `关闭搜索` or press Escape; panel gone and query cleared.
- **Proof.** Screenshots of open / hits / empty-or-miss / closed; note query string in `status.txt`.

## Gotchas

- ⌘F is ignored while a task detail dialog is open (`view.go`); close detail first.
- Search is an overlay: proving "filter" by looking only at column cards is wrong — use the panel hits (`searchHits` behavior in tests).
- Long titles ellipsize inside the panel; assert presence, not full string geometry.
- Do not invent a `清除搜索` name; close is `关闭搜索`, and Escape clears.
