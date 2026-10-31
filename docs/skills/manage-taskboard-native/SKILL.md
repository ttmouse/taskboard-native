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

## 执行 Agent 负责的任务闭环

收到派发的执行 Agent 是原卡的责任人：自己认领、实现、验证、回写结果和状态。点击派发只递交任务上下文；派发程序不替你认领、不代写工作结果。

### 单卡手动派发 vs 项目自动承接

| 模式 | 谁移入「等你 / 评审」 | 结果评论与验收证据 | 「完成」列 |
|------|----------------------|--------------------|-----------|
| **单卡手动派发**（顶栏/卡片派发、建卡并派发、`tb`/`MCP` 单次 dispatch） | **执行 Agent** 在回写结果后自行移入评审；派发程序**不**仅因会话结束或退出码移列 | **必须由执行 Agent 写入** | 只由人类确认 |
| **项目自动承接**（项目顶栏「自动承接」开启） | 会话 `Ended` 后，**服务可能**把仍在进行中的卡移入「等你」，以便队列继续下一张 | **仍必须由执行 Agent 写入**；服务代移列**不能**替代结果评论 | 只由人类确认 |

自动承接开启时：服务把「清空进行中/待办队列」做成项目职责，可能在会话收敛后代为移入「等你」并写一条说明评论；这与单卡手动派发的「程序不因会话结束移列」不同。无论哪种模式，**结果评论与验收证据仍必须由执行 Agent 写入**；「完成」列仍只由人类确认。不要把「卡已在等你」或「执行已返回」当成交付完成。

派发只需提供任务 ID 和技能名，例如：

```text
使用 manage-taskboard-native 执行任务 <taskId>。
```

任务标题、正文、项目和状态列以 API 返回的最新数据为准，不要求派发提示词重复携带。通过宿主技能工具或 `~/.agents/skills/manage-taskboard-native/SKILL.md` 加载本技能；无法加载时明确报告，不得声称已使用。

被评论跟进唤醒时，只处理那一条评论：读原卡和该评论，按评论要求执行、验证、回写结果和状态。评论可能带 `mention` 字段（人类用 `@渠道` 指定由哪个渠道跟进）；`mention` 与你自己渠道不同说明这条评论被明确交给了另一个渠道，不要抢着处理，遇到与自己渠道不一致的 `mention` 先核对后再决定。`tb followup` 是发起入口，跟进提示本身就是它投递的；在被它唤醒的会话里再调用它只会拿到「已接收评论跟进请求」，卡片不会有任何变化——那不是完成，不要把它当成结果汇报。

1. **读取原卡和项目目录** — 先运行 `tb show <taskId> --json`，读取正文、全部评论、projectId 和当前 version；再运行 `tb projects --json`，按 projectId 找到项目的 workspacePath 和状态列。工作目录以这个 workspacePath 为准，在该目录执行任务，不从提示词或临时路径猜测。非默认看板由 CLI 的 `TASKBOARD_ADDR` 或 `--addr` 指定；不要连接 DSH 插件的 :47825，也不要另挑任务或创建替代卡。
2. **认领后执行** — 先核对卡片 execution，再决定是否改状态。若存在活动 execution 且环境变量 `TASKBOARD_DISPATCH_TASK_ID` 与原卡 ID 相同，这是本次派发会话自身的执行锁：卡片在待办时移到进行中并继续原卡，已经在进行中时直接继续；不要重复派发，也不要把它当成其他 Agent。若活动 execution 的标记缺失或不匹配，停止并说明，不能抢占。没有活动 execution 时，待办卡正常移到进行中。所有写请求使用自身执行身份（如 DSH、Codex、Pi Agent、Claude Code），通过 `TB_AGENT` 或 `--agent` 指定，不能用默认 local。
3. **实现并验证** — 只完成本卡范围。恢复任务先检查已有产物，补齐缺失工作，不重复实现。重要进展、阻塞和验证失败由你写入原卡，不必逐工具刷评论。
4. **先回写结果，再移入评审** — 验证通过后，在原卡评论中写清变更、验证证据和遗留风险；随后读取最新 version，将原卡移入评审（单卡手动派发时由你移列）。最后再次读取原卡，确认结果评论存在、状态确已变更，才能报告交付。会话正常结束、有一段文字，都不能替代结果评论；自动承接若已代为移入「等你」，你仍须补齐结果评论与验收证据，不能把服务移列当成已交付。
5. **受阻或失败** — 写明原因、已完成部分和下一步；暂时无法继续则退回待办，等待人工确认时可保留进行中并写明在等什么。验证未通过不得移到评审。完成只留给用户明确验收确认。
6. **处理冲突和断连** — 409 时重新读取原卡，核对变化再决定是否继续，禁止盲目覆盖。回写请求断连时先 GET 核对是否已经成功，避免重复评论或重复移动；恢复会话优先补回写，不重新派发自身。若无法访问看板或模型已不能继续调用工具，明确说明尚未回写；不能声称程序会代为完成。

### 创建任务的两种模式

- 仅建卡：`tb add <标题>`（或 `POST /api/tasks` 不带 `dispatchRuntime`）——只建卡，不派发。
- 建卡并派发：`tb add <标题> --dispatch dsh|codex|pi|claude`（或 `POST /api/tasks` 带 `"dispatchRuntime"`）——同一请求建卡并交给所选渠道；渠道未知或项目无有效 workspacePath 时 400，不建卡。
- GUI：撰写条 Enter 仅建卡，⌘Enter 建卡并派发到所选渠道。
- MCP：`create_task` 不带 `dispatchRuntime` 只建卡，带上则同一调用建卡并派发。

### HTTP 回写接口

CLI 不在执行环境中时，使用本技能的默认看板地址调用 HTTP；环境明确指定 `TASKBOARD_ADDR` 或提供原卡链接时，以该地址为准。不需要再启动一个看板。 本机回环地址不能用只支持公网的 web_fetch/网页搜索抓取；使用本机 CLI、bash 中的 curl 或宿主的本机 HTTP 能力。状态列 ID 从 `GET /api/projects/{projectId}` 的 columns 获取，禁止猜测。

- `GET /api/tasks/{taskId}`：正文、评论、version。
- `POST /api/tasks/{taskId}/comments`，JSON `{"text":"变更、验证、风险"}`。
- `PATCH /api/tasks/{taskId}`，JSON `{"version":当前版本,"columnId":"目标列ID"}`。
- 写请求带 `Content-Type: application/json`、`X-Agent: 执行身份`。需要确认 HTTP 2xx，并最终 GET 核对原卡。

派发器保存的 execution 是连接/会话技术记录，不是任务验收结论。任务事实以执行 Agent 回写的评论和卡片状态为准。

执行记录会收敛：Herdr terminal 连续不可达超过宽限期（约 2 分钟）后由服务自动写 `released`（写锁释放），服务重启也会重新探测并收敛。`disconnected`/`unknown` 期间**写锁仍占用，暂不可重派**；徽章「连接异常」不是终态。遇到卡片「明明没人跑，却拒绝派发/跟进」或徽章一直挂「连接异常」时，先 `tb show <taskID> --json` 看 `execution.status`；宽限未满可等待收敛，或原 terminal 确实没了时用 `tb execution release <taskID>` 交由人确认后释放，不要直接改数据库。

技能内旧的界面约定若与本卡明确要求或项目最新文档冲突，以本卡和项目最新文档为准。

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
           [--dispatch dsh|codex|pi|claude]
tb move    <taskID> <column NAME|ID> [--top] [--after CARD_ID]
tb comment <taskID> <body> [--mention dsh|codex|pi|claude]
tb execution release <taskID>
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
5. **Column scrollbar** — Must not overlay cards (gutter / outside card stack) for 待办/进行中/等你/完成. Thumbs stay **hidden while idle** and only appear briefly while the user is scrolling (not merely hovering the column).
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
