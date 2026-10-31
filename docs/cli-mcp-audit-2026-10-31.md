# CLI / MCP 审计（2026-10-31）

只读审计：对照 `cmd/tb`、`cmd/taskboard-mcp`、`docs/contracts.md`，不改业务代码。

## 1. CLI 命令一览（`cmd/tb`）

全局：`tb [--agent WHO] [--addr ADDR] <command>`；环境变量 `TASKBOARD_ADDR`、`TASKBOARD_DB`、`TB_AGENT`。

| 命令 | 位置参数 | 主要 flags | 参数校验 / 用法提示 | 备注 |
| --- | --- | --- | --- | --- |
| `projects` | — | `--json` `--addr` `--agent` | flag 解析失败 → usage exit | 文本或 JSON |
| `project add` | `NAME` | `--path` `--json` `--addr` `--agent` | 缺 name → usage；有 usage 文案 | |
| `list` | — | `--project` `--column` `--assignee` `--limit` `--offset` `--summary` `--json` | limit 1–200、offset≥0 显式校验 | |
| `show` | `<taskID>` | `--json` | NArg≠1 → usage | |
| `add` | `<title>` | `--project` `--column` `--body` `--assignee` `--dispatch` | title 必填；dispatch 值由服务校验 | |
| `move` | `<taskID> <column>` | `--top` `--after` | NArg≠2 → usage | 列名/ID 解析 |
| `column` | `<columnID> <role>` | `--json` | role 枚举/清空；未知列/角色冲突有错 | 有单测 |
| `comment` | `<taskID> <body…>` | `--mention` | NArg<2 → usage | body 可多词拼接 |
| `followup` | `<taskID> <commentID>` | `--addr` `--agent` | NArg≠2 → usage（`followup.go`） | → `/continue` |
| `execution release` | `<taskID>` | — | 子命令固定 `release` | 写锁收敛 |
| `archive` | `<taskID>` | — | NArg≠1 → usage | |
| `relation add\|remove` | `<taskID> <type> <relatedID>` | — | type 枚举；NArg≠4 → usage | |
| `serve` | — | `--addr` `--db` `--detach` | 本地起服务 | unix/windows 分文件 |

退出码：用法错误通常 2；业务/网络错误 1；成功 0（见 `main_test` / `column_test`）。

**帮助文档**：顶层 `usage()` 覆盖全部子命令，与实现基本一致。单命令失败时多数打印短 usage；flag 解析走 `ContinueOnError` + 自定义文案，完整度良好。

## 2. MCP 工具一览（`cmd/taskboard-mcp`）

协议：stdio、换行分隔 JSON-RPC 2.0；`protocolVersion: "2025-06-18"`；身份 `TASKBOARD_AGENT`（默认 `mcp`），地址 `TASKBOARD_ADDR`。

| 工具 | 必填参数 | 可选参数 | 运行时校验 | 对应 HTTP |
| --- | --- | --- | --- | --- |
| `list_projects` | — | — | — | `GET /api/projects` |
| `create_project` | `name` | `workspacePath` | `requiredString` / `optionalString` | `POST /api/projects` |
| `list_tasks` | `projectId` | `columnId` `assignee` `limit` `offset` `summary` `includeArchived` | 类型检查；limit/offset 用 `requiredInt`（出现时） | `GET /api/tasks?…` |
| `get_task` | `id` | — | 必填；响应可附带正文/评论内图片 MCP image blocks | `GET /api/tasks/{id}` |
| `create_task` | `projectId` `title` | `columnId` `description` `assignee` `dispatchRuntime` | 列名解析；dispatch 透传 | `POST /api/tasks` |
| `update_task` | `id` `version` | `title` `description` `assignee` | 乐观并发；冲突带当前 task | `PATCH /api/tasks/{id}` |
| `move_task` | `id` `version` `columnId` | `afterId` `atTop` | 列解析 | `PATCH`（含 columnId） |
| `add_comment` | `taskId` `text` | `mention` | 必填字符串 | `POST …/comments` |
| `list_comments` | `taskId` | — | 必填 | `GET …/comments` |

Schema：每工具 `inputSchema` 设 `additionalProperties: false`（客户端侧约束）；描述为中文。未知工具名 → `unknown tool`。

错误处理：HTTP 非期望状态经 `apiError`；409 冲突尽量带回当前 `task` JSON，便于重试。服务不可达有明确文案。

## 3. 与 `docs/contracts.md` 对照

| 项 | contracts.md | 实现 | 结论 |
| --- | --- | --- | --- |
| CLI 列表 | 缺 `relation`；`add` 未写 `--dispatch`；`move` 未写 `--top/--after` | `usage()` 与代码已有 | **文档落后于 CLI** |
| MCP 工具列表 | 与 9 个工具一致（含 `dispatchRuntime`、`mention`） | 一致 | OK |
| MCP 能力边界 | 未写 archive / relation / execution / followup / 独立 dispatch / column role | CLI 有、MCP 无 | **刻意缺口或待补齐**（见下） |

## 4. 参数校验 / 错误 / 帮助：完整度

### CLI
- **完整**：顶层 usage、多数子命令位置参数个数检查、`list` 数值范围、`column` role 枚举、列名解析失败列出可用列。
- **弱项**：
  - `--dispatch` / `--mention` 渠道枚举主要依赖服务端 400，CLI 侧不预校验（可接受，但错误信息依赖 HTTP body）。
  - `comment` body 用剩余位置参数拼接，含 leading `-` 的正文可能被当成 flag（需 `--` 分隔；usage 未提示）。
  - `update` 类「改标题/正文」无独立 CLI（只能靠 MCP/`show`+服务）；与「Agent 写回」路径分工有关，但 contracts 未写明「无 tb update」。

### MCP
- **完整**：必填/类型（string/int/bool）有统一 helper；schema 关闭 additionalProperties；冲突错误带版本；`create_task`+`dispatchRuntime` 有单测。
- **弱项**：
  - **运行时不拒绝未知参数**：schema 写了 `additionalProperties:false`，但 `callTool` 只读已知键，多余键静默忽略——依赖宿主做 schema 校验。
  - `update_task`：空字符串被当成「未提供」，**无法清空** assignee/description。
  - `add_comment` **无 `parentCommentId`**（UI/API 支持回复），MCP 只能发顶层评论。
  - 无 `archive_task`、`set_relation`/`remove_relation`、`release_execution`、`followup_comment`、独立 `dispatch_task`、`set_column_role`——Agent 经 MCP 无法完成 CLI 已覆盖的运维动作。
  - 无 `update_comment` / `delete_comment`（API/UI 作者可编辑删除）。
  - `list_tasks` 的 `includeArchived` 在 schema/实现中有，contracts MCP 段未提及。

## 5. 缺失与改进建议（按优先级）

1. **对齐 contracts CLI 段**：补上 `relation`、`add --dispatch`、`move --top/--after`，避免文档与 `usage()` 分叉。
2. **MCP 补齐写路径高频缺口**：至少 `archive_task`、`followup_comment`（→ continue）、`release_execution`、`add/remove_relation`；可选独立 `dispatch_task` 与 `set_column_role`。
3. **MCP 运行时校验未知参数**（或文档写明「仅靠 schema」），并允许 `update_task` 显式清空字段（如 `null` 哨兵或 `clearAssignee`）。
4. **`add_comment` 支持 `parentCommentId`**，与 HTTP/UI 回复树一致。
5. **CLI**：usage 注明 `comment` 正文若以 `-` 开头需 `--`；可选对 `--dispatch`/`--mention` 做本地枚举提示。

## 6. 测试覆盖速览

- CLI：`main_test.go`、`column_test.go`、`comment_test.go`、`followup_test.go` 覆盖用法、角色冲突、JSON 形状等。
- MCP：`main_test.go`、`create_dispatch_test.go` 覆盖工具调用与 dispatch 建卡；未见「未知参数被拒」或 relation/archive 类用例（因工具不存在）。

---

审计结论：CLI 命令面与帮助较完整，文档略滞后；MCP 覆盖「读板 + 建改移评」主干且校验/冲突处理扎实，但相对 CLI/HTTP **缺少归档、关系、执行释放、评论跟进、回复父评论** 等 Agent 常用写操作，且清空字段/未知参数行为需收紧或写清。
