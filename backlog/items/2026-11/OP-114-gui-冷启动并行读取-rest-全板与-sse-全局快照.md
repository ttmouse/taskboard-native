---
id: "OP-114"
title: "GUI 冷启动并行读取 REST 全板与 SSE 全局快照"
slug: "gui-冷启动并行读取-rest-全板与-sse-全局快照"
status: "inbox"
impact: "medium"
effort: "M"
category: "performance"
family: "F1 服务端 SSE 快照与背压"
created: "2026-11-04"
evidence: "cmd/taskboard/board.go:473-495,530-566,591-642,680-690,829-874; internal/service/sse.go:95-110"
evidence_status: "current-source-confirmed-duplicate-initial-load"
evidence_note: "newBoard 同步启动 load 与 startStream。初始 cur 为 curGlobal；load 先请求 Projects，随后把 cur 置空并调用 openGlobal，逐项目串行请求完整 Board。与此同时 Subscribe 使用空 projectID，服务端 handleEvents 生成并发送包含全局 Boards/Tasks 的完整 Snapshot，客户端 applySnapshot 也替换 projects/columns/tasks。两条并发路径都读取、传输并反序列化全量任务数据；除重复数据库/JSON/网络工作外，响应顺序还可能造成临时空状态或后写覆盖。尚未用运行时统计量化实际字节数和启动延迟。"
verification: ""
resolution: ""
---

## 现状与证据

全局视图是 GUI 默认入口。由于 Board REST 加载和 SSE 全局快照在 newBoard 中同时启动，冷启动最初会经历 Projects + 每项目 Board REST 请求，同时 SSE 已提供同一时点的全局一致性快照；两者均独立构造并应用整板内容。

## 建议方向

协调首次加载与 SSE 初始快照，使全局启动正常情况下只读取/传输一份完整任务集合；保留 SSE 断开、重连、初始快照失败时的 REST 恢复能力，并保证快照 Seq 后事件连续应用。避免先以空选择处理全局快照造成中间清空，也避免迟到 REST 覆盖较新的 SSE 数据。

## 验收标准

以包含多项目/大量任务的数据启动 GUI，记录 REST 与 SSE 请求数、服务端 SQL/序列化量、响应字节和首帧可交互时间；正常 SSE 首次连接下全局任务快照只读取/传输一次，或有明确可量化的权衡；阻断/断开 SSE 时仍能 REST 展示当前数据并在重连后对齐；制造 REST/SSE 反序延迟，项目、列、任务和事件 Seq 最终状态一致且加载期间无空板闪现。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
