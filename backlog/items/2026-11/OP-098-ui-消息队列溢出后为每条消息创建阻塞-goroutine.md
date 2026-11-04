---
id: "OP-098"
title: "UI 消息队列溢出后为每条消息创建阻塞 goroutine"
slug: "ui-消息队列溢出后为每条消息创建阻塞-goroutine"
status: "inbox"
impact: "medium"
effort: "M"
category: "reliability"
created: "2026-11-04"
evidence: "cmd/taskboard/board.go:477-478,500-508,516-525; cmd/taskboard/view.go:80-90; cmd/taskboard/board.go:677-690,699-773"
evidence_status: "code-inspection"
evidence_note: "board.msgs 缓冲仅 64。post 遇到满队列为每条后续消息创建 goroutine 阻塞发送，仍继续调用窗口 Invalidate；pump 在每帧构建时循环读取直到队列瞬时为空，没有每帧处理预算。SSE reader 将快照和事件逐个 post 到此队列，其他后台任务也共用。"
verification: ""
resolution: ""
---

## 现状与证据

主线程渲染或处理大量消息时，后台事件会填满 64 项队列，之后每次 post 都新增一个等待 goroutine。队列持续有生产者时，pump 一帧会不断处理新到消息，帧耗时没有上界；多个阻塞发送 goroutine在空位出现后竞争写入，可能让原先顺序消息重排。任务事件按版本检查可拒绝旧版本，但评论增删和快照替换没有统一序号门禁，乱序应用可能导致短暂或持续状态偏差。

## 建议方向

改用单一有界、有序的消息调度器；按消息类型安全合并可覆盖状态的更新，不能丢的评论/快照按序保留；为每帧 pump 设处理预算并在仍有积压时请求下一帧。禁止每条溢出消息派生一个阻塞 goroutine，并为关闭时发送者退出定义清晰语义。

## 验收标准

压入超过缓冲容量的混合 SSE snapshot/task/comment 与 UI 回调，等待发送 goroutine 数保持有界；单帧 pump 工作量/耗时有上限，积压逐帧收敛；同一连接的事件顺序保持一致，snapshot 后事件不被旧快照覆盖，comment add/delete 不因重排丢失；窗口关闭后队列发送者可收敛退出。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
