---
name: manage-taskboard-native
description: Operate the taskboard-native board (localhost:47826) via tb CLI or HTTP API — claim, comment, move columns, archive. Use when an agent must create or update cards, record progress, or follow the native kanban workflow. Prefer tb CLI; do not use the DSH board on :47825 for this product.
---

# Manage taskboard-native

Board: **`127.0.0.1:47826`** (native MyGo app + SQLite service).  
Not the DSH plugin board on `:47825`.

Primary interface: **`tb` CLI** (`cmd/tb`). HTTP under `/api/...` is equivalent. Set:

```sh
export TASKBOARD_ADDR=127.0.0.1:47826   # bare host:port — never prefix http://
export TB_AGENT=豆皮                     # or pass --agent on every write
```

Health check: `GET http://127.0.0.1:47826/api/health` → `{"ok":true}`  
(`GET /health` is 404 — that is not a down service.)

## Columns (per project)

| Column | Role |
|--------|------|
| 待办 | Backlog / unclaimed |
| 进行中 | Claimed and actively worked |
| 评审 | Implementation done; waiting for human acceptance |
| 完成 | Human confirmed done |

There is **no delete**. Finished or obsolete cards are **archived** (`tb archive`).

Typical project for this repo: **`taskboard-native`** (discover id with `tb projects --json`).

## Attribution (required on writes)

Every mutation must name the acting agent. Default identity is `local`. Prefer:

```sh
tb --agent 豆皮 <command>
# or
TB_AGENT=豆皮 tb <command>
```

## Workflow (from DSH manage-taskboard, adapted)

1. **Search before create** — `tb list --project <id> --summary`. Reuse a card when it already tracks the same work; comment instead of duplicating.
2. **Read before execute** — `tb show <taskID>` (body + comments are requirements).
3. **Claim** — Move 待办 → 进行中 **before** starting work. That is the claim. A claim comment is optional habit, not required by the board.
4. **Implement** — Prefer one in-flight card per agent in this repo when possible.
5. **Before 评审** — After self-verify, **must** `tb comment` a short summary (key changes, verification, remaining risks); then `tb move <id> 评审`. Never jump straight to 完成.
6. **完成** — Only when the **human explicitly** accepts or asks to mark complete. Self-verification alone is not enough.
7. **Blocked / abandoned** — Comment the reason; move back to 待办, or `tb archive` (no canceled column — archive is the dead-end).
8. **完成但未验证 / 验收失败** — If a card was moved to 完成 (or 评审) but the change **did not take effect**, user rejects it, or verification fails: **`tb move <id> 进行中`**, comment what failed, and keep working on that same card until human-verified. Do **not** leave a broken delivery in 完成 and open a duplicate.


### Version / conflict

Tasks carry a `version`. On conflict: **re-read** (`tb show`), reconcile, **retry**. Do not overwrite blindly.

## CLI cheat sheet

```text
tb [--agent WHO] [--addr ADDR] <command>

tb projects [--json]
tb project add NAME [--path ABSOLUTE_DIRECTORY] [--json]
tb list    [--project ID] [--column NAME|ID] [--assignee WHO] [--limit N --offset N] [--summary] [--json]
tb show    <taskID> [--json]
tb add     <title> [--project ID] [--column NAME|ID] [--body TEXT] [--assignee WHO]
tb move    <taskID> <column NAME|ID> [--top] [--after CARD_ID]
tb comment <taskID> <body>
tb archive <taskID>
tb serve   [--addr ADDR] [--db PATH] [--detach]
```

Standing rule: **every task an agent dispatches must appear as a card** on this board, with honest column + comments (do not stamp「活跃」on unclaimed 待办).

## Product UI conventions (GUI)

Agreed 2026-10-25; keep when changing UI:

1. **New task vs new project** — Sidebar small **「+」** is **新建项目 only**. **新建任务** is the **top-bar plus icon** (defaults into 待办). **Never** put「添加任务」on column headers — it was removed and must stay gone.
2. **New project** — Demoted control; sidebar plus / dedicated flow — not a bright primary button.
3. **Cards** — No decorative border/outline (theme tokens; flat face).
4. **Sidebar** — No vertical divider line; distinguish rail with `Surface` background vs board `Background`. Project names **left-aligned** (`TextAlign`/`Justify` Start).
5. **Column scrollbar** — Must not overlay cards (gutter / outside card stack) for 待办/进行中/评审/完成. Thumbs stay **hidden while idle** and only appear briefly while the user is scrolling (not merely hovering the column).
6. **Sound** — Subtle cues on task created and status change (especially 完成); only on real mutations, not poll/refresh.
7. **Top bar** — Keep compact; reduce padding/height without dropping essential controls. **No window/header title text**: `TitleBarHiddenInset` + empty `Title`; compact height `Space(8)`≈32 — **never** size to `max(bar.Height)` (inflates to ~46–52). Center traffic lights with `TrafficLightPosition` (Y≈10). Top bar: search + 新建任务 plus + settings gear (no refresh, no sync green dot — SSE silent; offline uses banner). **Launch default**: sidebar「全部项目」(curGlobal), not `local`.
8. **Search** — Top-bar「搜索任务…」filters cards by title + description across all columns; clear button when query non-empty.

See also `docs/design.md`.

## Out of scope

- DSH `:47825` / `X-Taskboard-*` headers — different product.
- Deleting tasks (archive only).

## Related

- `docs/agent-read/` — read-path CLI/MCP contracts
- `docs/design.md` — UI anatomy
- `docs/product-direction.md` — product direction
