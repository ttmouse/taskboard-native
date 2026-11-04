---
id: "OP-062"
title: "DSH 会话快照每次分页下载并累积完整历史但只消费最新 turn"
slug: "dsh-会话快照每次分页下载并累积完整历史但只消费最新-turn"
status: "inbox"
impact: "medium"
effort: "M"
category: "performance"
created: "2026-11-04"
evidence: "internal/executor/desktop.go:154-177,189-218,330-355; internal/executor/continuation.go:121-132"
evidence_status: "current-source-confirmed"
evidence_note: "desktopBridge.snapshot 循环请求 session.getSnapshot 直到所有分页结束，把所有 desktopItem（含完整历史 message 文本）追加到一个 slice。resultAfter 只使用最后一个 turn.end 和该 turn 完结前的助手 message；评论续聊 preflight 调用 snapshot 后只用 lastDesktopTurn 找最后一个 turn.end。trackDesktop 每秒读取一次状态，idle 时立即调用 resultAfter；评论已提交而新 turn 尚未开始时，旧快照可能被重复完整分页与累积。实际会话历史规模、协议是否支持增量/反向分页尚未确认。"
verification: ""
resolution: ""
---

## 现状与证据

DSH 持久会话变长后，检查最新回合结果仍会拉取整段历史；在会话短暂 idle 的轮询周期中可能重复发生。

## 建议方向

优先确认桌面桥协议是否可按 turn/cursor 增量读取；若支持，只取上次已知 turn 之后的项。若协议只能返回完整快照，则逐页消费并仅保留判定最新 turn 所需的 ID/终态与该 turn 助手文本，且对 same-id idle 状态采用合理退避，避免每秒重拉长历史。保留 SnapshotComplete 校验和恢复时不误判旧 turn 的语义。

## 验收标准

构造含大量旧消息及新 turn 尚未开始的 DSH 会话，验证一次轮询的传输/解析工作不再随全部旧消息增长，或 idle 等待期间不会每秒重复获取完整历史；最终仍仅将 PreviousTurnID 之后的新 done turn 判为结束，SnapshotComplete 不完整时仍拒绝确认，并测量长短会话的字节数、峰值内存与 idle 等待请求数。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
