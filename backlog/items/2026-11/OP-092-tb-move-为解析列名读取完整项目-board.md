---
id: "OP-092"
title: "tb move 为解析列名读取完整项目 Board"
slug: "tb-move-为解析列名读取完整项目-board"
status: "inbox"
impact: "medium"
effort: "S"
category: "performance"
created: "2026-11-04"
evidence: "cmd/tb/main.go:451-483,913-920; internal/service/server.go:178-200; internal/store/queries.go:186-200,213-232"
evidence_status: "code-inspection"
evidence_note: "cmdMove 顺序 GET /api/tasks/{id}、GET /api/projects/{id}/board、PATCH /api/tasks/{id}。Board handler 返回 Store.Board；该查询在列元数据之外还加载项目所有活动任务并批量补全关系。CLI 对 Board 结果只读取 Columns 来解析用户传入的列名。大项目下该中间往返与移动无关。"
verification: ""
resolution: "- 2026-11-04: 补充同根因：cmdAdd 在显式传入 --column 时也调用 c.board(projectID)，只使用 Board.Columns 解析列名；因此 tb add --column 同样把全部项目任务及关系随 Board 读取。源码：cmd/tb/main.go:422-429；读取实现仍见 internal/store/queries.go:186-200,213-232。\n- 2026-11-04: 补充 MCP 调用路径（2026-11-04）：cmd/taskboard-mcp 的 create_task 在显式传 columnId 时调用 resolveColumn，move_task 也调用该函数；resolveColumn 对 /api/projects/{id}/board 做完整 GET 并只读取 Columns（cmd/taskboard-mcp/main.go:429,492,751-780）。因此 MCP 显式列名建卡和移动同样把项目全部活动任务、关系与正文读入并丢弃，除本项已记的 tb move/tb add 外也受影响。验收覆盖 MCP 两个工具，记录请求数/字节并确认列解析读取不包含 Tasks。"
---



## 现状与证据

README 示例的 tb move <taskID> <column name> 在客户端先 GET 完整任务以取得 projectId/version，再 GET 项目 Board 以将列名解析为 columnId，最后 PATCH 移动。GET Board 调用 Store.Board，会在 Project/Columns 后加载项目全部活动任务并补全关系；cmdMove 只使用返回的 Board.Columns。卡片数量和正文增长时，这个中间 GET 带来不必要的 SQLite 查询、关系扫描、序列化、网络传输和解码，且所有三次 HTTP 往返串行执行。

## 建议方向

移动前只读取列元数据，不获取 Board.Tasks；可复用仅含项目/列信息的现有接口或新增窄项目元数据接口。若新增 HTTP 路由或字段，先同步更新 docs/contracts.md。保留任务版本校验、项目列归属校验和列名/ID两种输入。

## 验收标准

对同一移动请求，目标列名仍按项目唯一解析、跨项目列仍被拒绝、过期 version 仍返回当前冲突；中间读取响应不含任何 Task，读取与响应成本不再随项目卡片数、正文长度和关系数增长；记录请求数、响应字节及移动端到端延迟。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。

补充同根因：cmdAdd 在显式传入 --column 时也调用 c.board(projectID)，只使用 Board.Columns 解析列名；因此 tb add --column 同样把全部项目任务及关系随 Board 读取。源码：cmd/tb/main.go:422-429；读取实现仍见 internal/store/queries.go:186-200,213-232。

补充 MCP 调用路径（2026-11-04）：cmd/taskboard-mcp 的 create_task 在显式传 columnId 时调用 resolveColumn，move_task 也调用该函数；resolveColumn 对 /api/projects/{id}/board 做完整 GET 并只读取 Columns（cmd/taskboard-mcp/main.go:429,492,751-780）。因此 MCP 显式列名建卡和移动同样把项目全部活动任务、关系与正文读入并丢弃，除本项已记的 tb move/tb add 外也受影响。验收覆盖 MCP 两个工具，记录请求数/字节并确认列解析读取不包含 Tasks。
