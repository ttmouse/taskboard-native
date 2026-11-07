---
id: "OP-027"
title: "跟进状态写回失败被忽略会让 reserved 评论永久无法重试"
slug: "跟进状态写回失败被忽略会让-reserved-评论永久无法重试"
status: "inbox"
impact: "high"
effort: "M"
category: "reliability"
family: "F7 写入路径：原子性、静默失败、写入放大"
created: "2026-11-04"
evidence: "internal/store/execution.go:120-148,164-168; internal/executor/continuation.go:48-63,159-174; internal/executor/dsh.go:418-431,472-479,516-523; internal/executor/herdr.go:455-466; internal/executor/continuation_desktop.go:30-52"
evidence_status: "source-confirmed-stuck-reservation-path"
evidence_note: "ReserveFollowup 将 followups.status 持久化为 reserved。若 CLI 启动前失败，执行器调用 SetFollowupStatus(failed) 但忽略错误；同一函数也用它写 sent/unknown。若 SQLite 更新失败，状态留在 reserved。后续 Continue 看到 reserved 且 task 不在内存 inflight 时稳定返回 ErrInProgress；没有从已结束 execution 或 handoff receipt 自动修复该状态。"
verification: ""
resolution: ""
---

## 现状与证据

评论跟进设计要求：只有外部写入结果不确定时才保持锁；明确未启动失败的评论应可重试。当前状态写回失败时，执行器和重试入口之间没有错误回传/修复协议，数据库故障可把一次跟进预留卡死为无期限 in-progress。

## 建议方向

让 SetFollowupStatus 的写入错误进入持久诊断和重试/恢复流程；对外部调用已发送与未发送的情况使用不同可恢复状态，启动前失败不能因状态更新失败而永久停在 reserved。服务重启恢复也需核对 execution 终态及发送回执，避免重复发送不确定指令。

## 验收标准

注入启动前与已发送后的 followup 状态写入失败，确认未启动评论可安全重试、发送结果不确定时不会自动重复投递，并可通过恢复或明确诊断结束 reserved 状态；FollowupStatus 查询错误仍显式返回。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
