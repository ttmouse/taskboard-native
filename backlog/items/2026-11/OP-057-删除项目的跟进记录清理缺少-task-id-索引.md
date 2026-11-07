---
id: "OP-057"
title: "删除项目的跟进记录清理缺少 task_id 索引"
slug: "删除项目的跟进记录清理缺少-task-id-索引"
status: "inbox"
impact: "low"
effort: "S"
category: "performance"
family: "F5 读放大、N+1 与缺索引"
created: "2026-11-04"
evidence: "internal/store/store.go:97-101; internal/store/mutations.go:601; sqlite3 EXPLAIN QUERY PLAN: DELETE FROM followups WHERE task_id IN (SELECT id FROM tasks WHERE project_id=?)"
evidence_status: "query-plan-confirmed-system-sqlite-3.54.0"
evidence_note: "followups 只有 comment_id PRIMARY KEY，没有 task_id 索引。Store.DeleteProject 按 task_id IN (SELECT id FROM tasks WHERE project_id=?) 删除。对照当前 schema 和语句的 SQLite 3.54.0 EXPLAIN QUERY PLAN 显示外层为 SCAN followups；子查询使用 tasks_project 索引。单条状态查询 WHERE task_id=? AND comment_id=? 则由 comment_id 主键检索，因此 task_id 索引的明确收益点是项目删除的集合清理。尚未在 MyGo 嵌入运行库中实测同一计划或耗时。"
verification: ""
resolution: ""
---

## 现状与证据

项目删除事务需删除属于该项目所有任务的 followup 行；关联字段 task_id 当前没有索引，因此查询计划扫描整张 followups 表，即使只删除一个项目的数据。

## 建议方向

考虑为 followups(task_id) 增加索引，使项目删除可按项目任务集合定位跟进记录；确认数据库迁移/旧库初始化会创建该索引，并衡量额外写入与磁盘成本。

## 验收标准

在实际嵌入 SQLite 版本中用跨多个项目的 followups 数据执行 EXPLAIN QUERY PLAN，外层删除不再全表扫描且目标项目行全部删除、其他项目保留；以大量跨项目记录比较删除耗时和新增索引维护成本。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
