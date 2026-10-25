# 工程契约（v0）

服务、CLI、MCP 与原生 UI 共享的接口。改这里 = 改契约，先改文档再改代码。

## 进程与地址

- 单一二进制 `cmd/taskboard`（原生窗口）与 `cmd/tb`（CLI）、`cmd/taskboard-mcp`（MCP stdio）。
- 内建 HTTP 服务默认监听 `127.0.0.1:47826`，`TASKBOARD_ADDR` 可覆盖。
- 数据库默认 `mygo.App.Path(mygo.PathUserData)` 目录下 `taskboard.db`（注意：PathUserData 是 `mygo.PathName` 常量，不是函数；返回 `(dir string, err error)`），`TASKBOARD_DB` 可覆盖。
- 测试钩子：`TASKBOARD_SLOW=<duration>` 给每个请求注入延迟（截图验证加载态用，非产品功能）。
- GUI 与 CLI/MCP 都是服务的客户端；写入只经服务，服务是唯一写者。GUI 通过 SSE 就地更新，禁止轮询。

## 数据（SQLite，经 mygo plugins/sqlite）

```
projects(id TEXT PK, name TEXT, workspace_path TEXT DEFAULT '', position INT, created_at TEXT, updated_at TEXT)
columns(id TEXT PK, project_id TEXT, name TEXT, position INT)
tasks(id TEXT PK, project_id TEXT, column_id TEXT, title TEXT, body TEXT,
      position INT, version INT, archived INT, last_agent TEXT,
      created_at TEXT, updated_at TEXT)
comments(id TEXT PK, task_id TEXT, author TEXT, body TEXT, created_at TEXT)
```

首次打开空库时种子：项目 `local` + 列 `待办/进行中/评审/完成` + 3 张演示卡。时间 RFC3339 UTC；ID 为 16 字节随机 hex。

## HTTP API（JSON）

身份头 `X-Agent`（缺省 `local`）；写入方把身份带入事件与 `lastAgent`。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/health` | `{"ok":true}` |
| GET | `/api/projects` | `[Project]`（含可选 workspacePath） |
| POST | `/api/projects` | `{name, workspacePath?}` → 201 Project，含默认四列 |
| GET | `/api/projects/{id}/board` | `Board`（列 + 任务，含 archived=false） |
| GET | `/api/tasks?projectId=` | `[Task]`，projectId 必填；可选 columnId、assignee、limit、offset、summary |
| GET | `/api/tasks/{id}` | `Task`（含正文与全部评论） |
| POST | `/api/tasks` | `NewTask` → 201 `Task`，事件 `task.created` |
| PATCH | `/api/tasks/{id}` | `TaskPatch`（version 必填）→ 200 `Task`；移动列时事件 `task.moved`（带 from/to），否则 `task.updated` |
| POST | `/api/tasks/{id}/archive` | `{"version":N}` → 200 `Task`，事件 `task.archived` |
| GET | `/api/tasks/{id}/comments` | `[Comment]` |
| POST | `/api/tasks/{id}/comments` | `NewComment` → 201 `Comment`，事件 `comment.added` |
| GET | `/api/events?projectId=` | SSE：`event: <type>` + `data: <Event JSON>`；每 15s `: ping` |

错误：`400` 参数缺失；`404` 不存在；`409` `Conflicterror:"version_conflict", task}`。版本冲突时客户端拉一次详情再对齐。

类型定义唯一来源：`internal/model/model.go`（JSON tag 即线格式）。

## CLI（cmd/tb）

```
tb projects [--json]
tb project add <name> [--path ABSOLUTE_DIRECTORY] [--json]
tb list [--project ID] [--column NAME|ID] [--assignee WHO] [--limit N --offset N] [--summary] [--json]
tb show <taskID> [--json]
tb add <title> [--project ID] [--column NAME|ID] [--body TEXT]
tb move <taskID> <column NAME|ID>
tb comment <taskID> <body>
tb archive <taskID>
tb serve [--addr ...]        # 前台跑服务（--detach 后台）
```

- 身份：`--agent` 或 `TB_AGENT`，默认 `local`。
- 除 `serve` 外，先 `GET /api/health`；不通则自动 `tb serve --detach` 并等 3s。仍不通报错退出。

## MCP（cmd/taskboard-mcp，stdio，换行分隔 JSON-RPC 2.0）

- `initialize` → `protocolVersion: "2025-06-18"`、`serverInfo{name:"taskboard-native"}`、tools 能力。
- `tools/list`：`list_projects`、`create_project`（name，workspacePath 可选）、`list_tasks`（projectId 必填，支持 columnId/assignee/limit/offset/summary）、`get_task`（id，正文及评论）、`create_task`、`update_task`、`move_task`、`add_comment`、`list_comments`。
- `tools/call` → 调 HTTP API，`content:[{type:"text", text:<JSON>}]`。
- 身份：`TASKBOARD_AGENT`，默认 `mcp`。

## 原生 UI（cmd/taskboard）

- 单窗口看板：项目切换、列、卡片、建卡/编辑/移动/评论/归档、加载态（`plugins/loading`）、空态、明暗主题跟随系统。
- 数据只经 HTTP 客户端；SSE 事件就地更新，不整表重建。版本冲突按契约拉详情对齐。
- 视觉规格见 `docs/design.md`（乔布斯产出）。

## 分工（写入边界）

- `internal/store/**`、`internal/service/**`、`cmd/tb/**`、`cmd/taskboard-mcp/**`：Linus（后端 + 代理接口）。
- `internal/client/**`、`cmd/taskboard/**`、`docs/assets/render/**`：Ive（原生 UI）。
- `internal/model/**`、`docs/contracts.md`：orchestrator。
- `docs/design.md`：乔布斯。`docs/design-review.md` + 截图：Dieter Rams。
- 禁止 `go get` / `go mod tidy`（依赖已固定）；禁止 git commit/push；禁止跑 `./...` 全量构建/测试（兄弟任务在写）。


## Agent 读取与工作区（2026-10-25）

本轮取舍与验收见 `docs/agent-read/plan.md`。项目路径使用 JSON 字段 `workspacePath`、数据库列 `workspace_path`；旧数据库首次打开仅补这一列，旧项目与旧名称创建均兼容。提供路径时，API 校验它是本机存在的绝对目录并规范化；不创建目录、不初始化 Git、不派发执行，不要求路径唯一。路径仅用于后续定位执行目录，本轮不引入自动调度。

读取推荐路径：`list_projects → list_tasks(summary=true, filters...) → get_task(id)`。摘要仅包含 id/projectId/columnId/title/version/updatedAt/updatedBy，assignee 非空时返回；不含正文与评论。需要完整内容时按 ID 获取正文与全部评论，不必拉取整个项目的正文。

`columnId` 必须属于项目；assignee 精确匹配；limit 为 1..200 的整数；offset 为非负整数且必须配合 limit。先过滤再分页，顺序与看板一致；offset 为当前数据的偏移分页，不提供并发变更下的快照保证。summary 仅接受 true/false。未传新参数时保持原有全量数组返回，空结果为 []。
