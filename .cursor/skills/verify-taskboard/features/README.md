# Taskboard verification map

This directory is the maintained source for verifying user-facing behavior of **Taskboard** (MyGo native desktop kanban at `/Applications/Taskboard.app`). Read the index before driving the app, then use the matching feature file as the recipe.

## Baseline preconditions

- Prefer installed App: version in `mygo.json` matches `defaults read /Applications/Taskboard.app/Contents/Info CFBundleShortVersionString`, then `open -a Taskboard`.
- Visible UI changes require: bump `mygo.json` → `python3 scripts/generate_version.py` → `mygo build -skip-dmg` → ditto install → reopen App (see AGENTS.md and skill Launch).
- Alternate isolated review launch: build binary + `TASKBOARD_ADDR=127.0.0.1:47827 TASKBOARD_DB=/tmp/tb-verify-$RUN_ID/taskboard.db` (docs/review-brief.md).
- Unit gate before UI: `go test ./cmd/taskboard/ -count=1` (sidebar tests live there).
- Evidence under `/tmp/tb-verify-$RUN_ID/`; keep after cleanup.
- Never drive the user's already-open Taskboard if you started a different instance; kill only PIDs you started.
- Run `.cursor/skills/verify-taskboard/scripts/doctor.sh` and require exit 0 for the chosen path.

## Driving conventions

- Start from baseline unless a feature lists other preconditions.
- Prefer Accessibility names from tests (`收起侧栏`, `展开侧栏`, `新建任务`, `搜索任务`, `打开设置`, `项目列表`, `任务搜索面板`, `关闭搜索`, `打开任务：…`) over coordinates.
- Keystrokes via `osascript` / System Events; screenshots via `scripts/capture-window.sh`; SSE mutations via `tb` CLI with `TASKBOARD_ADDR` pointed at the instance under test.
- Treat every command as literal. Do not invent selectors absent from `sidebar_test.go` / `search_*` / `new_task_*` / `view.go` / review-brief / AGENTS.md.
- Restore mutated board state when a journey requires a clean seed; never delete proof artifacts in cleanup.

## Proof and skip reporting

- Capture the user action and the resulting state (before + after), not only the final screen.
- Motion claims need optional short recording or frame sequence, not a single still.
- SSE claims need a `tb` write plus a visible in-place board update (no full reload).
- Record feature file used, VERSION, EVIDENCE_DIR paths in `status.txt`.
- Report an unreachable path with the attempted command and unmet precondition.
- Do not report a skipped entry point as verified through a different path.

## Feature entry contract

Each feature file starts with an H1 and one paragraph, then exactly four H2s:

1. `Sub-features`
2. `How to get to it (user POV)`
3. `Driving it with osascript+screencapture+tb CLI`
4. `Gotchas`

## Features

- [Sidebar toggle](./sidebar-toggle.md) — ⌘B / panel chip collapse·expand, crisp animation, prefs async, Cmd+B stutter case.
- [Board SSE](./board-sse.md) — CLI/MCP writes appear in-place on the open board without full reload.
- [Search](./search.md) — ⌘F / `搜索任务` overlay, title+description hits, empty/clear/close.
- [New task](./new-task.md) — ⌘N / `新建任务` compose sheet, project targeting, Enter vs ⌘Enter.
