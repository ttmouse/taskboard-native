# New task

Users create tasks through the compose sheet (`新建任务` / ⌘N). On a project board it targets that project's 待办; from 全部项目 it either asks to pick a project or reuses the last one. Enter creates; ⌘Enter creates and dispatches when a channel and workspace path allow.

## Sub-features

- `compose-open` — sidebar/top-bar `新建任务` or ⌘N opens the sheet (`任务标题` field; submit `创建任务`).
- `compose-project-gate` — on launch 全部项目 with no remembered project, UI shows `请先在侧栏选一个项目` instead of a form.
- `compose-enter` — Enter creates into 待办; Escape cancels.
- `compose-dispatch` — ⌘Enter uses selected `执行渠道` when workspace path is set; otherwise keeps draft.
- `compose-no-stack` — ⌘N does not open under an open task detail dialog.

## How to get to it (user POV)

- Click `新建任务` (rail or top-bar when collapsed).
- Press ⌘N while frontmost and no blocking dialog.
- Optional: project row action `在此项目新建任务`.
- Sidebar small `+` is **新建项目 only** — not new task (`docs/skills/manage-taskboard-native/SKILL.md`).

## Driving it with osascript+screencapture+tb CLI

Preconditions:

- Doctor exit 0; prefer a concrete project selected (`切换到项目 local`) so ⌘N opens the form.
- Unit references: `new_task_shortcut_test.go`, compose_* tests.
- For SSE-visible create without UI typing, `tb add` from `board-sse.md` is allowed as a complementary proof — still drive UI once for compose acceptance.

- **Select project.** Click `切换到项目 local` (or another real project name from `项目列表`).
- **Capture BEFORE.** `$EVIDENCE_DIR/before-compose.png`
- **OPERATION — open compose.**

```bash
osascript -e 'tell application "Taskboard" to activate' \
  -e 'delay 0.2' \
  -e 'tell application "System Events" to keystroke "n" using command down'
```

Or click `新建任务`.

- **EXPECTED open.** Compose UI with accessible `任务标题`; creating targets 待办 for that project.
- **OPERATION — create.** Type a unique title; click `创建任务` or press Enter (not ⌘Enter unless testing dispatch).
- **EXPECTED create.** Sheet closes; card appears under 待办 (`打开任务：<title>`); optional confirm with `tb list --project local`.
- **Capture AFTER.** `$EVIDENCE_DIR/after-create.png`
- **Cancel path.** Re-open compose, type draft, Escape; no new card.
- **Proof.** before/after + title string + optional `tb show` JSON in `status.txt`.

## Gotchas

- Launch default is 全部项目 (`curGlobal`); ⌘N may show `请先在侧栏选一个项目` — select a project before asserting the form.
- Detail dialog blocks ⌘N; close detail first.
- Do not confuse `新建项目` (+) with `新建任务`.
- Dispatch (⌘Enter) needs workspace path; failure must keep draft — do not call that a successful create.
- Prefer unique titles (`verify-$RUN_ID-…`) so assertions do not collide with seed cards.
