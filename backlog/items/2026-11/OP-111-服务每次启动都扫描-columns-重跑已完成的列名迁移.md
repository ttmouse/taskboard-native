---
id: "OP-111"
title: "服务每次启动都扫描 columns 重跑已完成的列名迁移"
slug: "服务每次启动都扫描-columns-重跑已完成的列名迁移"
status: "inbox"
impact: "low"
effort: "S"
category: "performance"
family: "F15 迁移与启动顺序"
created: "2026-11-04"
evidence: "internal/store/store.go:264-272; Python sqlite3 EXPLAIN QUERY PLAN using current columns schema and columns_project index"
evidence_status: "source-and-query-plan-confirmed-system-sqlite"
evidence_note: "Store.Open 每次调用 migrate；review 列名迁移无版本门控，每次都执行 UPDATE columns SET name=... WHERE role=? AND name=?。当前仅有 columns_project(project_id,position) 索引；在等价 schema 上，SQLite 查询计划为 SCAN columns。成功升级后的常见数据库中该更新将扫描整张列表并匹配 0 行。列数通常远少于任务数，尚无真实启动耗时测量，因此仅列为低影响候选；系统 Python SQLite 计划不等同于 MyGo 嵌入版本。"
verification: ""
resolution: ""
---

## 现状与证据

列名迁移位于 Store.Open 的通用 migrate 流程中，每次服务启动都会经过该语句。迁移成功一次后旧默认名称已改为“等你”，但后续启动仍扫描 columns 表寻找匹配项；项目数量增长时扫描随所有项目列数线性增加。

## 建议方向

评估将该变更纳入明确的一次性 schema migration/version marker，使已处理数据库启动时跳过 UPDATE；若项目列数量与启动数据证明扫描可忽略，则保留现状，避免仅为此增加索引。

## 验收标准

构造含大量项目列且已完成旧标签迁移的数据库，记录 Store.Open 的 SQL 执行/访问计划与耗时；已迁移数据库后续启动不再扫描 columns 执行该 UPDATE，旧库首次升级仍正确重命名旧默认 review 列且不覆盖自定义名称；比较启动时间并证明迁移状态在失败/重试时安全。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
