---
id: "OP-011"
title: "项目列表活跃时间聚合缺少 updated_at 覆盖索引"
slug: "项目列表活跃时间聚合缺少-updated-at-覆盖索引"
status: "inbox"
impact: "low"
effort: "S"
category: "performance"
created: "2026-11-04"
evidence: "internal/store/queries.go:115-118,159-162; internal/store/store.go:79"
evidence_status: "source-confirmed-performance-hypothesis"
evidence_note: "每次列出项目均按项目聚合 MAX(tasks.updated_at)，谓词为 project_id 与 archived=0；现有 tasks_project 索引仅覆盖 (project_id, archived)。候选收益取决于任务量/项目数，尚无真实库 EXPLAIN 或基准，不能认定当前为瓶颈。"
resolution: ""
---

## 现状与证据

projectsLocked 在项目列表中对每个项目计算活跃任务的 MAX(updated_at)，projectLocked 读取单项目详情时重复同类聚合。现有复合索引 tasks_project(project_id, archived) 过滤任务，但聚合列 updated_at 不在索引中。

## 建议方向

以真实规模数据库运行 EXPLAIN QUERY PLAN 并测量项目列表读取；若聚合反复访问表且成本可见，再评估 (project_id, archived, updated_at) 覆盖索引，并测量写入/存储代价。

## 验收标准

提供代表性任务规模下的前后查询计划与延迟数据；若优化，迁移确保旧库创建索引，并证明项目/活跃时间结果一致且写入代价可接受。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
