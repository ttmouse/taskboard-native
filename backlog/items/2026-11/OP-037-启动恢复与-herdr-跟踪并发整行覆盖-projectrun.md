---
id: "OP-037"
title: "启动恢复与 Herdr 跟踪并发整行覆盖 ProjectRun"
slug: "启动恢复与-herdr-跟踪并发整行覆盖-projectrun"
status: "inbox"
impact: "high"
effort: "M"
category: "reliability"
family: "F3 自动承接与执行状态机"
created: "2026-11-04"
evidence: "internal/executor/dsh.go:47-98,214-229; internal/executor/project_run.go:383-416,549-590; internal/store/project_run.go:31-44; docs/concurrency-safety-2026-10-31.md:17-21"
evidence_status: "current-source-confirmed"
evidence_note: "当前 NewDispatcher 在 ActiveExecutions 循环中直接启动 resumeHerdrTracking goroutine，然后调用 recoverReturnedHerdrExecutions 与 recoverProjectRuns。recoverProjectRuns 没有持 projectRunMu；异步 trackHerdr 结束后调用 finishProjectCard，该函数持 projectRunMu 后读取并 SaveProjectRun。SaveProjectRun 为无版本条件的整行 UPSERT。启动恢复与快速结束/恢复会话发生交错时，两个路径可能基于旧 run 快照互相覆盖 cursor、CurrentTaskID 或状态。结论由当前源码确认并与历史并发审计一致；尚未构造确定性并发复现。"
verification: ""
resolution: ""
---

## 现状与证据

服务启动时同时执行“恢复活跃 Herdr 会话跟踪”和“恢复项目自动承接状态”。这些路径可能针对同一个 project_runs 行写入完整旧快照，后写者会无冲突覆盖前一条路径已经提交的新进度。

## 建议方向

先让启动恢复阶段对 ProjectRun 的读改写与跟踪结束路径使用同一串行化机制，或在恢复完成前暂缓启动 tracker；若改为版本/CAS 写入，确保所有 SaveProjectRun 写入者共同遵守冲突语义。还需检查锁顺序，不要在 Store 锁内等待 projectRunMu。

## 验收标准

通过可控屏障让同一项目的 Herdr tracker 结束与 recoverProjectRuns 对同一行交错，确认 cursor、CurrentTaskID 和终态不会被旧快照回退或覆盖；启动恢复、人工停止及任务结束的并发顺序均可重复验证且无死锁。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
