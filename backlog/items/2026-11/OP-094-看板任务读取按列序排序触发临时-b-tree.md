---
id: "OP-094"
title: "看板任务读取按列序排序触发临时 B-tree"
slug: "看板任务读取按列序排序触发临时-b-tree"
status: "inbox"
impact: "medium"
effort: "M"
category: "performance"
family: "F5 读放大、N+1 与缺索引"
created: "2026-11-04"
evidence: "internal/store/queries.go:213-232; internal/store/store.go:56-80; Python sqlite3 EXPLAIN QUERY PLAN with equivalent schema"
evidence_status: "query-plan-confirmed-system-sqlite-3.54.0"
evidence_note: "tasksLocked 通过 correlated scalar subquery 查询每张卡所属列的 position，并按该值、tasks.position、tasks.created_at 排序。SQLite 3.54.0 对应当前 schema/query 的计划为 SEARCH tasks USING INDEX tasks_project、对每张 task 使用主键查询 columns，且 USE TEMP B-TREE FOR ORDER BY。此路径供 Board、Snapshot 和任务列表复用；尚未在 MyGo 嵌入 SQLite 3.53.4 上验证计划或测量实际耗时。"
verification: ""
resolution: ""
---

## 现状与证据

每次完整读取项目任务时，SQL 都要按列 position、卡片 position 和创建时间排序。列顺序通过相关子查询取得，任务过滤索引不能直接提供最终次序。当前查询计划因此需要额外临时排序结构；项目卡片越多，排序的 CPU 与临时内存成本越高。该数据库读取路径不同于 OP-070 的移动/归档时列内位置重写，也不同于 UI 每帧筛选排序。

## 建议方向

评估将列元数据与任务排序改为可被查询计划直接利用的形式，或在一致性读取中批量读取列序和任务后排序；确保 board、snapshot、list 的归档过滤和稳定顺序一致，并比较查询计划、延迟、分配与多列规模。

## 验收标准

在项目实际嵌入 SQLite 版本上复核 Board、Snapshot、TasksByProject 的查询计划和代表性规模基准；读取路径不再为相关列序表达式构建临时排序，或实测替代方案在高卡片量下降低延迟/分配且不增加不可接受的 SQL 往返；跨列/列内顺序与归档行为保持一致。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
