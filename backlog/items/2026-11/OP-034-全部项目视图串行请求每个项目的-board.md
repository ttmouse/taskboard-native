---
id: "OP-034"
title: "全部项目视图串行请求每个项目的 Board"
slug: "全部项目视图串行请求每个项目的-board"
status: "inbox"
impact: "medium"
effort: "M"
category: "performance"
created: "2026-11-04"
evidence: "cmd/taskboard/board.go:585-630; internal/client/client.go:354-376; internal/service/server.go:479-487"
evidence_status: "code-path-confirmed"
evidence_note: "openGlobal 按 b.projects 顺序循环调用 b.api.Board(p.ID)，每次对应一个独立 GET /api/projects/{id}/board；收到响应后才请求下一项目。项目数量 P 时该加载阶段需要 P 次串行 HTTP 往返，等待延迟线性累加。Client.Board 在当前接口成功时每项目一次请求；若 endpoint 不可用还会回退到项目和任务两个请求。实际项目数与网络往返延迟未采样。"
verification: ""
resolution: "- 2026-11-04: 补充启动路径证据（2026-11-04）：newBoard 先后启动 load 和 startStream（cmd/taskboard/board.go:473-495）。startStream 订阅全局 SSE，服务端首帧返回全局 BoardSnapshot（cmd/taskboard/board.go:677-690; internal/service/sse.go:99-123）。与此同时，load 获取项目列表后对全局视图调用 openGlobal，后者仍逐项目 GET Board 并覆盖状态（cmd/taskboard/board.go:530-586,591-643）。因此初始启动在按项目串行 REST 读取全部项目任务的同时，还读取完整全局 SSE 快照，重复构建/传输/反序列化任务数据；应评估以 SSE 首帧作为全局视图初始数据源，避免双重全量加载，并保留连接先订阅后快照的无丢事件语义。"
---


## 现状与证据

全部项目视图加载前已持有项目列表，但每个项目看板分别从服务读取，服务端每次独立获取项目元数据、任务和关系。相较单次聚合读取，串行网络延迟会增加切换视图等待时间。

## 建议方向

优先评估复用一次全局 BoardSnapshot 或增加聚合读取路径，确保项目列合并规则、失败处理与一致性语义明确；如改 HTTP 契约，先更新 docs/contracts.md。并发请求只能作为短期选项，需考虑单 Store 锁串行和取消/错误聚合。

## 验收标准

用多个项目的服务端延迟注入对照，全部项目加载请求往返数不再随项目数线性增长；卡片合并列名、项目属性、任务排序和加载错误行为保持一致，并记录切换视图的端到端耗时。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。

补充启动路径证据（2026-11-04）：newBoard 先后启动 load 和 startStream（cmd/taskboard/board.go:473-495）。startStream 订阅全局 SSE，服务端首帧返回全局 BoardSnapshot（cmd/taskboard/board.go:677-690; internal/service/sse.go:99-123）。与此同时，load 获取项目列表后对全局视图调用 openGlobal，后者仍逐项目 GET Board 并覆盖状态（cmd/taskboard/board.go:530-586,591-643）。因此初始启动在按项目串行 REST 读取全部项目任务的同时，还读取完整全局 SSE 快照，重复构建/传输/反序列化任务数据；应评估以 SSE 首帧作为全局视图初始数据源，避免双重全量加载，并保留连接先订阅后快照的无丢事件语义。
