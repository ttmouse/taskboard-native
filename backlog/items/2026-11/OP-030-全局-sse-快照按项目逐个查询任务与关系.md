---
id: "OP-030"
title: "全局 SSE 快照按项目逐个查询任务与关系"
slug: "全局-sse-快照按项目逐个查询任务与关系"
status: "inbox"
impact: "medium"
effort: "M"
category: "performance"
created: "2026-11-04"
evidence: "internal/store/queries.go:83-112,116-156,213-235; internal/service/sse.go:96-110"
evidence_status: "code-path-confirmed"
evidence_note: "Snapshot 对每个项目循环调用 tasksLocked；每个非空项目至少分别执行任务查询和关系补全查询，项目数增长时 SQL 往返次数线性增长。全局快照还会在 projectsLocked 中独立读取项目与所有列。属于代码路径可确认的 query-count 扩张；实际延迟影响取决于项目数量、卡片和关系规模，尚未基准验证。与 OP-014 的项目范围快照先读全局项目再过滤是不同成本：此项针对全局快照的按项目逐次任务读取。"
verification: ""
resolution: ""
---

## 现状与证据

未传 projectId 的 SSE 初始快照会包含全部项目。当前实现依次对每个项目执行 tasksLocked，而 tasksLocked 先查询任务，再为非空任务查询关系；因此含 P 个非空项目时至少增加 2P 次 SQL 查询，并在整个过程持有 Store 互斥锁。

## 建议方向

评估为全局快照增加一次跨项目任务批量读取，再统一做关系补全；复用项目/列映射构造各 ProjectBoard，保持锁内一致性以及现有排序、归档过滤与项目作用域。

## 验收标准

对含多个项目、空项目、有关系任务及归档卡片的样本，对照 BoardSnapshot 的内容、顺序和 Seq 完全一致；SQL 查询次数不再随项目数线性增长，并测量高项目数初始 SSE 快照耗时及锁占用。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
