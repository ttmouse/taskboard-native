---
id: "OP-103"
title: "tb 写操作读取完整任务及评论只为取得版本号"
slug: "tb-写操作读取完整任务及评论只为取得版本号"
status: "inbox"
impact: "medium"
effort: "M"
category: "performance"
created: "2026-11-04"
evidence: "cmd/tb/main.go:471-483,1021-1041; internal/service/server.go:642-649; internal/store/queries.go:236-270"
evidence_status: "source-confirmed-cli-preflight-overfetch"
evidence_note: "tb archive 与 relation add/remove 先通过 c.task 读取完整 GET /api/tasks/{id}，仅使用 Version；tb move 还使用 ProjectID。handler 调用 Store.Task，除 taskLocked 的 CompletedBy/Execution/关系补全外，还读取并返回该任务全部评论。CLI 丢弃这些字段后再发送带 version 的写请求；tb move 的额外完整 Board 读取另由 OP-092 跟踪。"
verification: ""
resolution: "- 2026-11-04: 补充 MCP move_task 预检（2026-11-04）：该工具接收调用方提供的 version，但仍先调用 GET /api/tasks/{id} 并只读取 ProjectID 来解析列名；MCP task helper 将完整响应反序列化为 model.Task，而 Task handler 还补全 CompletedBy、Execution、关系和整段评论（cmd/taskboard-mcp/main.go:479-493,728-749; internal/service/server.go:642-649; internal/store/queries.go:254-273,364-380）。这与 tb move 的完整任务预检是同根因，另行叠加 OP-092 的完整 Board 读取。建议的窄化元数据读路径应同时覆盖 MCP move_task；验收统计完整任务响应中评论/关系的字节与调用总数。"
---


## 现状与证据

CLI 的 archive 和关系增删为乐观并发校验先 GET 完整任务，只需要 Version；move 还需要 ProjectID。GET handler 走 Store.Task，会查询任务、执行和关系摘要并读取评论历史，然后将整张任务返回。CLI 丢弃评论、关系和执行详情，仅用少数元数据发起下一次写请求。评论历史长时，这增加数据库读取、序列化、网络传输和客户端解码；move 路径另有只为解析列名而获取完整 Board 的成本，已由 OP-092 记录。

## 建议方向

提供或复用只返回必要元数据的读取路径，并让 CLI 使用该窄响应做版本校验和项目/列解析；如改变 HTTP 契约，先更新 docs/contracts.md。不要绕过 Store 或取消现有版本冲突保护。

## 验收标准

对 archive、relation add/remove 和 move 记录请求数、响应字节及长评论历史下的耗时；预检响应不带评论正文、关系图或执行详情，版本冲突仍返回当前版本/任务语义，跨项目列仍拒绝，命令成功输出不变。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。

补充 MCP move_task 预检（2026-11-04）：该工具接收调用方提供的 version，但仍先调用 GET /api/tasks/{id} 并只读取 ProjectID 来解析列名；MCP task helper 将完整响应反序列化为 model.Task，而 Task handler 还补全 CompletedBy、Execution、关系和整段评论（cmd/taskboard-mcp/main.go:479-493,728-749; internal/service/server.go:642-649; internal/store/queries.go:254-273,364-380）。这与 tb move 的完整任务预检是同根因，另行叠加 OP-092 的完整 Board 读取。建议的窄化元数据读路径应同时覆盖 MCP move_task；验收统计完整任务响应中评论/关系的字节与调用总数。
