---
id: "OP-063"
title: "批量补全任务关系会因缺少两端 ID 索引扫描整张关系图"
slug: "批量补全任务关系会因缺少两端-id-索引扫描整张关系图"
status: "inbox"
impact: "medium"
effort: "S"
category: "performance"
family: "F5 读放大、N+1 与缺索引"
created: "2026-11-04"
evidence: "internal/store/relations.go:356-384; internal/store/store.go:112-125; system SQLite 3.54.0 EXPLAIN QUERY PLAN with current schema"
evidence_status: "query-plan-confirmed-system-sqlite-3.54.0"
evidence_note: "enrichTasksLocked 按 source_task_id IN (...) OR target_task_id IN (...) 找关联边。当前主键以 relation_type 开头，普通 target 索引也以 relation_type 开头；部分 parent target 索引只覆盖 parent，因此该谓词缺少可直接按两端 ID 查找的索引。系统 SQLite 3.54.0 对当前 schema 的计划为 SCAN r USING COVERING INDEX sqlite_autoindex_task_relations_1；模拟添加 source_task_id 与 target_task_id 单列索引后改为 MULTI-INDEX OR，两支分别 SEARCH 对应索引。该 helper 同时被单任务 enrichTaskLocked 调用，单卡详情查询也可能扫描整张跨项目关系表。实际影响取决于关系表规模和候选 ID 数；尚未在嵌入 SQLite 运行库测量。"
verification: ""
resolution: "- 2026-11-04: 补充项目删除清理路径（2026-11-04）：DeleteProject 在持有全局 Store.mu 时，把删除评论/执行/跟进/任务等语句放进同一事务，并执行 DELETE FROM task_relations WHERE source_task_id IN (SELECT id FROM tasks WHERE project_id=?) OR target_task_id IN (...)（internal/store/mutations.go:575-605）。当前关系主键和 target 索引都以 relation_type 开头，项目删除谓词没有 relation_type；源/目标端点索引的收益可能同时覆盖此路径，并缩短持锁事务时间。此具体 DELETE 的 EXPLAIN 尚未验证，验收需单独检查其计划及跨项目关系规模下的锁持有/删除耗时，避免把 OP-063 对 enrichTasksLocked 的 SELECT 计划直接当作 DELETE 计划证据。"
---

## 现状与证据

任务列表和单任务读取都会补全关系摘要；当前筛选条件按关系边的源/目标任务 ID，但现有索引不能直接支持不限定 relation_type 的两端匹配。

## 建议方向

评估为 source_task_id 与 target_task_id 提供可用于当前 OR 谓词的索引，或拆为两条按端点索引查询后去重；检查与既有主键/target 索引的重复存储成本，并在旧库初始化中可靠创建。

## 验收标准

在实际嵌入 SQLite 上对单卡和小项目执行 EXPLAIN QUERY PLAN，关系行数跨多个项目且目标只涉及少量 ID 时不再扫描整张关系索引；关系完整性、父子方向、阻塞方向和 related 去重不变；比较不同任务数/关系密度下查询耗时、写入开销与索引大小。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。

补充项目删除清理路径（2026-11-04）：DeleteProject 在持有全局 Store.mu 时，把删除评论/执行/跟进/任务等语句放进同一事务，并执行 DELETE FROM task_relations WHERE source_task_id IN (SELECT id FROM tasks WHERE project_id=?) OR target_task_id IN (...)（internal/store/mutations.go:575-605）。当前关系主键和 target 索引都以 relation_type 开头，项目删除谓词没有 relation_type；源/目标端点索引的收益可能同时覆盖此路径，并缩短持锁事务时间。此具体 DELETE 的 EXPLAIN 尚未验证，验收需单独检查其计划及跨项目关系规模下的锁持有/删除耗时，避免把 OP-063 对 enrichTasksLocked 的 SELECT 计划直接当作 DELETE 计划证据。
