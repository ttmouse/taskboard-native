---
id: "OP-053"
title: "Herdr 跟踪轮询会重复执行未变化的审批检查与执行记录读取"
slug: "herdr-跟踪轮询会重复执行未变化的审批检查与执行记录读取"
status: "inbox"
impact: "medium"
effort: "S"
category: "performance"
family: "F3 自动承接与执行状态机"
created: "2026-11-04"
evidence: "internal/executor/herdr.go:347-357,401-408,446-450; internal/executor/project_run.go:602-617; internal/store/execution.go:28-42"
evidence_status: "current-source-confirmed"
evidence_note: "Herdr tracker 默认每 2 秒探测。每次状态为 blocked 都调用 parkProjectCardForApproval；该函数在 projectRunMu 下重复读取 Task、ProjectRun，并在需要时查询 Project。每轮随后无条件调用 SaveExecution；即使内存状态未变，该方法仍会在全局 Store.mu 下查询 execution 后才能判等返回。blocked 持续期间，这些重复读取会周期性占用共享锁；此项针对 tracker 周期内的空操作，不涉及网络探测本身。"
verification: ""
resolution: ""
---

## 现状与证据

当 Herdr 会话处于等待审批状态时，tracker 每 2 秒继续探测。每个 blocked 响应都重复执行项目审批卡处理；轮询末尾也尝试保存执行快照。

## 建议方向

相同执行快照已成功持久化后，不再重复调用 `SaveExecution`；首次进入 blocked 时处理审批卡，成功处理后不在每轮重复执行。若移列或写回失败，保留有退避的重试，不能把失败误当成已处理。后续相同状态轮询仍保留必要的外部探测，并确保失联计数、idle grace 与服务关闭恢复行为不变。

## 验收标准

用连续 blocked 响应维持 tracker 多个周期，验证审批移列成功后后续相同响应不再重复处理该卡，执行快照相同则不再获取 Store 锁查询数据库；另注入一次审批处理失败，确认会退避重试且最终仍可收敛。状态变化时仍及时更新 execution、SSE 和卡片状态，恢复/释放语义保持一致。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
