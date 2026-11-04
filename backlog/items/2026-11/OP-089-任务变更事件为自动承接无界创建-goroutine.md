---
id: "OP-089"
title: "任务变更事件为自动承接无界创建 goroutine"
slug: "任务变更事件为自动承接无界创建-goroutine"
status: "inbox"
impact: "medium"
effort: "M"
category: "reliability"
created: "2026-11-04"
evidence: "internal/service/server.go:136-140; internal/store/store.go:407-412; internal/store/mutations.go:135-138; internal/executor/dsh.go:39-43; internal/executor/project_run.go:166-178"
evidence_status: "code-inspection"
evidence_note: "每个 created/moved/archived 事件都直接 ；事件回调从持 Store.mu 的写操作中发出。所有 goroutine 竞争 Dispatcher 全局 projectRunMu，随后至少查询 ProjectRun。突发速率超过串行处理能力时，没有有界队列或合并机制，goroutine 数可随待处理事件增长。未实测具体积压阈值。"
verification: ""
resolution: "- 2026-11-04: 证据字段补充（2026-11-04）：命令行反引号展开导致 evidence_note 中 `go NoteProjectRun` 被 shell 当作命令执行，正文的“独立启动一个 NoteProjectRun goroutine”与源码一致；准确源码为 internal/service/server.go:138-140。当前 evidence_note 在此命令中无法直接改写，故保留此更正记录供核对。"
---


## 现状与证据

服务的 Store.OnEvent 对每个任务创建、移动和归档事件独立启动一个 NoteProjectRun goroutine。Store 的 emit 在变更方法持有 Store.mu 时同步调用回调，因此 worker 需等待写锁释放；NoteProjectRun 又由 Dispatcher 的单个 projectRunMu 串行处理所有项目，并在检查 run 状态前先查询 ProjectRun。批量/高频任务变更快于这条单线程处理路径时，会留下数量无上限的等待 goroutine，增加内存和调度开销并与正常写请求争抢 Store 锁。

## 建议方向

改用有界的串行工作队列，并按项目/任务合并尚未处理的最新状态，或采取等效的有界调度策略。保持单项目自动承接的状态转换顺序；队列满时必须能从 Store 当前状态补偿重建，不能静默丢掉让队列停滞的最后一次变更。评估是否需要把全局 projectRunMu 缩小为项目级锁，避免无关项目互相阻塞。

## 验收标准

对高于处理速率的 create/move/archive 事件突发，等待 worker 数和内存占用保持有界；突发后各项目 CurrentTaskID、队列游标及卡片所在列与现有语义一致，活跃执行不重复派发；不同项目的进展不被全局锁无谓串行化，写请求延迟有可复现的基准记录。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。

证据字段补充（2026-11-04）：命令行反引号展开导致 evidence_note 中 `go NoteProjectRun` 被 shell 当作命令执行，正文的“独立启动一个 NoteProjectRun goroutine”与源码一致；准确源码为 internal/service/server.go:138-140。当前 evidence_note 在此命令中无法直接改写，故保留此更正记录供核对。
