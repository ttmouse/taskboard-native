---
id: "OP-038"
title: "Herdr 跟踪器可在人工释放后把执行状态重新写回运行中"
slug: "herdr-跟踪器可在人工释放后把执行状态重新写回运行中"
status: "inbox"
impact: "high"
effort: "M"
category: "reliability"
created: "2026-11-04"
evidence: "internal/executor/herdr.go:356-450; internal/store/execution.go:28-39,42-57; internal/model/model.go:228-246,261-269; internal/service/server.go:865-885"
evidence_status: "current-source-confirmed"
evidence_note: "trackHerdr 在失联宽限期内反复以同一个内存 run 写 disconnected；若后续探测恢复，它会将 run 改为 running 并调用无条件 SaveExecution。handleReleaseExecution 允许对 disconnected/unknown 等 Releasable 状态人工 ReleaseExecution 为 released，但不取消 Dispatcher 中的 Herdr tracker。SaveExecution 只跳过完全相等状态，不做期望旧状态/CAS 校验，因此仍在运行的 tracker 后续探测可把 released 覆盖回 running，重新占用卡片写锁。此结论由当前代码路径确认，尚未运行确定性竞态复现。"
verification: ""
resolution: ""
---

## 现状与证据

人工释放的明确语义是停止该执行记录占用写锁，以便重新派发；但服务端仍运行的 Herdr 监视协程保留旧 Execution 副本，继续写回探测状态。

## 建议方向

人工 Release 与 tracker 需要共享停止/收敛机制：释放前取消并等待该任务 tracker，或将状态更新改为仅当数据库仍处于该 tracker 预期状态时写入；确保失联后真实重连但已人工释放的旧 tracker 不会恢复写锁。处理并发时避免持 Store 锁等待执行 goroutine。

## 验收标准

用可控 Herdr 探测序列先触发 disconnected，再人工 release，之后让同一 session 探测返回 working；Execution 必须保持 released，后续可安全派发，旧 tracker 不再写回；同时验证未人工释放时 tracker 仍可正常恢复 running。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
