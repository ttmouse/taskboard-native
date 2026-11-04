# Sidebar toggle

The project rail collapses and expands completely (no leftover narrow strip). ⌘B and the panel chip share one preference; the rail should slide crisply, prefs persist asynchronously, and rapid toggles must not hitch for multi-hundred milliseconds.

## Sub-features

- `sidebar-collapse` hides the whole rail; board reclaims width; `项目列表` gone; top bar keeps `展开侧栏` / `新建任务` / `搜索任务` / `打开设置`.
- `sidebar-expand` restores the rail and project rows (`切换到项目 <name>`).
- `sidebar-shortcut` ⌘B toggles the same `sidebarCollapsed` preference as the chip.
- `sidebar-anim-crisp` collapse finishes in a quick slide (unit: ~3–20 frames @16ms ≈ 80–260ms); pane+rail fill the window each frame.
- `sidebar-prefs-async` rapid toggles settle to the latest preference in `ui-prefs.json` without blocking the UI.
- `sidebar-stutter-case` rapid ⌘B 5–8 times stays smooth (acceptance OPERATION below).

## How to get to it (user POV)

- Click `收起侧栏` / `展开侧栏` in the top bar (panel chip; tip includes ⌘B).
- Press ⌘B while Taskboard is frontmost and no modal is eating the shortcut.
- Drag `调整侧栏宽度` when expanded (resize; not required for toggle stutter).

## Driving it with osascript+screencapture+tb CLI

Preconditions:

- Doctor exit 0; installed App version equals `mygo.json` (or isolated binary documented in status).
- Unit gate: `go test ./cmd/taskboard/ -run 'SidebarToggle|SidebarCollapse' -count=1` green.
- `EVIDENCE_DIR=/tmp/tb-verify-$RUN_ID/`; Taskboard window visible; you own the PID in `pids.txt`.

- **Capture BEFORE (expanded).** Ensure rail visible (`项目列表` present). Run `.cursor/skills/verify-taskboard/scripts/capture-window.sh "$EVIDENCE_DIR/before-expanded.png"`.
- **Collapse via chip.** Activate App and click Accessibility name `收起侧栏` (or ⌘B once). Rail gone; chip reads `展开侧栏`; board wider.
- **Capture AFTER (collapsed).** Run capture → `$EVIDENCE_DIR/after-collapsed.png`.
- **Expand.** Click `展开侧栏` or ⌘B; project list returns.
- **OPERATION — Cmd+B stutter case.** With App frontmost, send ⌘B rapidly 5–8 times:

```bash
osascript <<'OSA'
tell application "Taskboard" to activate
delay 0.3
tell application "System Events"
  repeat 6 times
    keystroke "b" using command down
    delay 0.05
  end repeat
end tell
OSA
```

- **EXPECTED.** Rail animates crisply between expanded/collapsed; no multi-hundred-ms hitch or frozen frames; final state matches last toggle; prefs write stays async (UI never blocks on disk); `defaults read … CFBundleShortVersionString` still equals `mygo.json` VERSION. Optional: short screen recording of the burst into `$EVIDENCE_DIR/sidebar-stutter.mov` (`screencapture -v` or QuickTime if Accessibility allows).
- **Proof stills.** Before/after pair of expanded vs collapsed is mandatory; stutter recording optional but preferred for motion claims.
- **Unit cross-check.** Keep `TestSidebarToggleAnimatesCrisply` / `TestSidebarTogglePersistsLatestPreferenceAsynchronously` in the pre-UI gate when touching animation or prefs.

## Gotchas

- Process name: installed App is `Taskboard`; review-brief binary is often `taskboard`. `capture-window.sh` defaults to `Taskboard`; set `PROCESS=taskboard` for the binary.
- Do not drive the user's pre-existing Taskboard session if you launched a separate isolated instance.
- Window shortcuts wait while detail/search/compose dialogs are up — dismiss overlays before ⌘B proofs.
- Prefs live beside `TASKBOARD_DB` / user data; isolated runs must set env so toggles do not rewrite the developer's real `ui-prefs.json`.
- Collapse must remove `项目列表` entirely (no `侧栏已收起` strip). If a strip remains, FAIL — do not redefine EXPECTED.
- macOS may show a folder-access sheet on first isolated binary launch (`docs/review-brief.md`); screenshot it, kill your PID, retry — do not click through as the user.
