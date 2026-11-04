# Board SSE

The open board updates in place when tasks change through the built-in HTTP/SSE service. CLI and MCP are clients of the same service; the GUI must not poll. A write via `tb` should slide/insert cards and update counts without rebuilding the whole board.

## Sub-features

- `sse-create` — `tb add` creates a card that appears in the target column without a manual refresh.
- `sse-move` — `tb move` relocates a card across columns with in-place transition.
- `sse-comment` — `tb comment` reflects on an open detail or badge without full reload.
- `sse-reconnect` — disconnect/reconnect recovers via `board.snapshot` then incremental events (`docs/realtime-sync.md`).

## How to get to it (user POV)

- Leave Taskboard open on a project board (e.g. switch via `切换到项目 local` or 全部项目 as needed).
- From a terminal, run `tb` against the same listen address the App uses (default `127.0.0.1:47826`; isolated review often `127.0.0.1:47827`).
- Watch the window: cards and column counts update silently (no refresh control in top bar; offline uses a reconnect banner).

## Driving it with osascript+screencapture+tb CLI

Preconditions:

- Doctor exit 0 for the instance under test.
- `TASKBOARD_ADDR` points at that instance (App default `127.0.0.1:47826`, or `127.0.0.1:47827` for isolated binary).
- Repo `tb` binary available: `/Users/douba/Projects/taskboard-native/tb` (or `go run ./cmd/tb`).
- Board window visible; capture helper works for process `Taskboard` or `PROCESS=taskboard`.

- **Capture BEFORE.** `.cursor/skills/verify-taskboard/scripts/capture-window.sh "$EVIDENCE_DIR/before-board.png"`
- **OPERATION — create.**  

```bash
cd /Users/douba/Projects/taskboard-native
export TASKBOARD_ADDR=127.0.0.1:47826   # or 47827 for isolated
./tb --agent verify add "验收写入测试" --project local
./tb --agent verify list --project local --json | tee "$EVIDENCE_DIR/list-after-add.json"
```

- **EXPECTED after create.** New card title visible in 待办 (Accessibility `打开任务：验收写入测试` or visible text); column count ticks; no whole-board flash/reload.
- **Capture AFTER create.** → `$EVIDENCE_DIR/after-add.png`
- **OPERATION — move.** Take `id` from JSON; `./tb --agent verify move <taskID> 进行中`
- **EXPECTED after move.** Card leaves 待办 and appears under 进行中 in-place; capture `$EVIDENCE_DIR/after-move.png`.
- **Proof.** Pair screenshots + `list-after-add.json` (and optional `tb show <id> --json`) in `status.txt`. PASS only if UI and CLI agree.

## Gotchas

- Wrong `TASKBOARD_ADDR` mutates another instance (or fails). Always align addr with the window you screenshot.
- Never write SQLite directly; GUI/CLI/MCP are clients only (AGENTS.md).
- Do not treat a manual App relaunch as SSE proof.
- Seed/project id `local` is the usual review target; if missing, create via UI/CLI first and note it in Gotchas/status — do not skip.
- Isolated DB under `/tmp/tb-verify-$RUN_ID/taskboard.db` will not show the user's production cards; say so in the report.
