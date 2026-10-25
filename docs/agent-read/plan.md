# Agent 读取闭环 · 本轮取舍

2026-10-25。目标：Agent 少读无关内容，能从任务摘要直接进入单卡正文和评论。当前正式动效及未提交改动保留，不提交 Git，不操作用户现有任务。

## 取舍

- 本轮交付读取闭环；不加入租约、自动派发、依赖图、优先级模型或数据库迁移。
- 保持现有接口默认行为；新增筛选、分页和摘要为可选参数。项目作用域仍必填。
- 使用既有列 ID 表示筛选，CLI 支持列名解析；不增加第二套 status 字段。
- 单卡直接复用已有 `GET /api/tasks/{id}`，它已经包含正文及 comments。MCP 和 CLI 补齐入口，不新建上下文聚合服务。
- 列表仍是 JSON 数组，不改成分页信封。`limit` 1..200，`offset` 非负整数且仅在提供 limit 时允许；没有 limit 时保持原有全量行为。排序保持看板顺序。分页为当前数据的偏移分页，不承诺并发变更下快照一致。

## 统一接口

`GET /api/tasks?projectId=...&columnId=...&assignee=...&limit=20&offset=0&summary=true`

- `columnId`：该项目中存在的列 ID；未知或跨项目列返回 400。
- `assignee`：精确匹配；不提供则不过滤。
- `limit` / `offset`：过滤之后分页，参数错误返回 400；空结果 `[]`。
- `summary`：严格 true/false；true 仅返回 `id, projectId, columnId, title, version, updatedAt, updatedBy`，`assignee` 非空时带上；不含正文、评论、position、archived。
- 单卡 `GET /api/tasks/{id}` 返回原有 Task（含正文与 comments），404 保持原契约。

CLI：`tb list --project ID [--column NAME|ID] [--assignee WHO] [--limit N --offset N] [--summary] [--json]`；`tb show ID [--json]`。
MCP：扩展 `list_tasks` 同名 HTTP 参数；新增 `get_task {id}`。

## 分工

Lead：产品取舍、统一契约、整合、独立验证、文档和打包。三个实现 Worker 分别仅拥有 internal/service、cmd/tb、cmd/taskboard-mcp。第四位 Luna 检查 Worker 在实现后核对读取闭环和兼容性，默认只读。

## 验收

隔离临时数据库验证：按列/负责人过滤、分页边界、空结果、摘要不泄漏正文/评论、单卡正文及评论完整；CLI/MCP 对齐 HTTP 契约，旧调用与正式 UI 保持可用。不向用户数据库创建测试卡。

## 用户追加：项目与工作路径

本轮同时补齐项目创建入口和 `Project.workspacePath`：`POST /api/projects {name, workspacePath?}`，返回带默认四列的新项目；GET /api/projects 和 project board 返回同一字段。旧数据/仅名称创建继续允许空路径。路径若提供必须为存在的本机绝对目录，服务层校验并 filepath.Clean；不创建目录、不初始化仓库、不触发执行，不强制路径唯一。

Lead 负责 internal/model 的 WorkspacePath 字段与 internal/store 的兼容升级：保留 `CreateProject(name,by)`，新增 `CreateProjectWithPath(name,workspacePath,by)`；API Worker 直接调用新方法（Lead 正实现）。store 负责持久化，不重复做文件系统校验。

CLI Worker 同时补 `tb projects --json` 和 `tb project add NAME [--path ABSOLUTE_DIRECTORY] [--json]`；常规 projects 输出展示非空工作路径，create 返回项目 ID/路径便于后续建卡。MCP Worker 同时增加 `create_project {name,workspacePath?}`，list_projects 原样转发新增路径。新增项目、工作路径与读取闭环仍保持一个简单、可验收的主路径。
