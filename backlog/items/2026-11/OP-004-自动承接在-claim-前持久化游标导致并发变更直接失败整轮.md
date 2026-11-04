---
id: "OP-004"
title: "自动承接在 claim 前持久化游标导致并发变更直接失败整轮"
slug: "自动承接在-claim-前持久化游标导致并发变更直接失败整轮"
status: "inbox"
impact: "medium"
effort: "M"
category: "reliability"
created: "2026-11-04"
evidence: "internal/executor/project_run.go:287-302,333-362; docs/concurrency-safety-2026-10-31.md:4-9"
evidence_status: "source-confirmed-race"
evidence_note: "dispatchProjectCard 先写 CurrentTaskID/Cursor，再读取并条件移动任务；claim 错误直接向上返回，continueProjectRun 将整轮置为 failed"
resolution: ""
---

## 现状与证据

dispatchProjectCard 在 claimProjectCard 前先持久化 CurrentTaskID 和递增后的 Cursor。claim 会重新读取任务并检查归档状态、所在列和版本；若用户在队列扫描后移动或归档该卡，claim 失败，错误被 continueProjectRun 转成整轮 failed。源码明确存在这个竞态窗口。

## 建议方向

将候选 claim 与游标推进设计为可恢复状态转移：只有 claim 成功后才提交已消费游标，或在 claim 失败时恢复/跳过当前候选并继续队列；确保重启恢复路径可以判定该卡是否已派发，避免重复会话。

## 验收标准

并发模拟扫描后用户移列、归档、版本冲突三种情形；自动承接不因单张候选竞争而错误消耗游标或无必要整轮失败；重启后不重复派发同卡，run/currentTask/cursor 状态与实际任务列一致。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
