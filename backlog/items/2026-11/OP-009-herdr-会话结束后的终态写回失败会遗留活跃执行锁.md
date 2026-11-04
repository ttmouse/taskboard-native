---
id: "OP-009"
title: "Herdr 会话结束后的终态写回失败会遗留活跃执行锁"
slug: "herdr-会话结束后的终态写回失败会遗留活跃执行锁"
status: "inbox"
impact: "medium"
effort: "M"
category: "reliability"
created: "2026-11-04"
evidence: "internal/executor/herdr.go:356-367,380-384,409-413,427-446; internal/executor/desktop.go:227-239,267-273,325-388; internal/store/execution.go:28-42; internal/model/model.go:221-238"
evidence_status: "source-confirmed-error-path"
evidence_note: "终态路径忽略 SaveExecution error 后立刻 return; if SQLite upsert fails, stored status remains active and Active() keeps the card locked; tracker goroutine then ends without retry"
verification: ""
resolution: "- 2026-11-04: 补充证据：DSH 的 trackDesktop 在 idle 终态写 returned 失败后仍 return（desktop.go:350-354）；跟踪异常分支忽略写入 disconnected 的错误后也退出（desktop.go:375-388）；服务关闭写 detached 失败后照常退出（desktop.go:330-333）。desktopState 只有 SaveExecution 成功才更新内存 run，因此落库失败时数据库仍保留旧 active 状态；当前 goroutine 随后移除 inflight，不再持续跟踪。与 Herdr 属于同一终态/状态收敛写回失败导致活跃锁残留根因，合并记录，不另建重复项。"
---


## 现状与证据

Herdr tracker 在 done、exit、idle grace 或不可达宽限期等终态分支先改内存 run，再调用 _ = SaveExecution，随后立即 return。若 SQLite upsert 本身失败，数据库仍保留 running/waiting 等 active 状态；Execution.Active 对这些状态返回 true。跟踪 goroutine 已结束，不会在当前服务进程内重试写终态，卡片可能持续被写锁挡住。

## 建议方向

终态落库失败时不能静默结束跟踪：记录可操作错误并有限退避重试；若仍失败，保留明确的持久化故障/需人工释放信号，同时保证下一次恢复可以重新探测并收敛，避免报告“已释放”但实际仍 active。

## 验收标准

对每种终态注入一次 SQLite upsert 失败，验证 tracker 或恢复机制最终把持久状态收敛到正确终态，execution 锁不永久滞留；不能在终态持久化确认前推进依赖该状态的自动承接，也不能误报写锁已释放。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。

补充证据：DSH 的 trackDesktop 在 idle 终态写 returned 失败后仍 return（desktop.go:350-354）；跟踪异常分支忽略写入 disconnected 的错误后也退出（desktop.go:375-388）；服务关闭写 detached 失败后照常退出（desktop.go:330-333）。desktopState 只有 SaveExecution 成功才更新内存 run，因此落库失败时数据库仍保留旧 active 状态；当前 goroutine 随后移除 inflight，不再持续跟踪。与 Herdr 属于同一终态/状态收敛写回失败导致活跃锁残留根因，合并记录，不另建重复项。
