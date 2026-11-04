---
id: "OP-107"
title: "单项目视图仍消费全局 SSE 事件"
slug: "单项目视图仍消费全局-sse-事件"
status: "inbox"
impact: "low"
effort: "M"
category: "performance"
created: "2026-11-04"
evidence: "cmd/taskboard/board.go:startStream 以空 projectID 调用 Subscribe，applyEvent 对其他项目事件立即 return；internal/service/sse.go:handleEvents 与 hub.publish 支持按 projectId 筛选。"
evidence_status: "observed"
evidence_note: "无关事件仍会服务端编码/传输、客户端解码并投递到 UI 队列；当前业务处理在项目过滤处结束，实际收益尚未测量。"
verification: ""
resolution: ""
---

## 现状与证据

GUI 的 SSE 流只启动一次，切换到单项目后不重订阅。其他项目产生的事件仍进入该 GUI 的事件流并被排队，然后在 applyEvent 按 ProjectID 丢弃。

## 建议方向

评估在单项目视图按当前项目 ID 订阅，返回全局视图时恢复全局订阅；保证重订阅后的快照和事件顺序正确，并明确项目列表/项目变更数据何时刷新。

## 验收标准

在多项目并发事件负载下测量单项目窗口收到的无关 SSE 事件量、解码/队列成本和 UI 响应；缩流后这些无关事件显著下降，当前项目更新正常，切换全局视图后项目与卡片数据仍新鲜。若无可测收益则不实现。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
