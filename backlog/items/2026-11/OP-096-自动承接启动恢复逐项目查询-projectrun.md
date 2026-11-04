---
id: "OP-096"
title: "自动承接启动恢复逐项目查询 ProjectRun"
slug: "自动承接启动恢复逐项目查询-projectrun"
status: "inbox"
impact: "low"
effort: "S"
category: "performance"
created: "2026-11-04"
evidence: "internal/executor/dsh.go:46-98; internal/executor/project_run.go:383-401; internal/store/queries.go:115-149; internal/store/project_run.go:13-29"
evidence_status: "code-inspection"
evidence_note: "每次 NewDispatcher 都调用 recoverProjectRuns。该函数先 Store.Projects 读取全部项目和全部列并计算每项目活跃时间，再逐项目调用 Store.ProjectRun；project_runs 以 project_id 为主键，运行态由 status=running 判定。项目数为 P 时，即使没有活动 ProjectRun，也会执行 P 次逐项目查询以及完整 Projects/Columns 读取。"
verification: ""
resolution: ""
---

## 现状与证据

服务每次启动都会创建 Dispatcher 并执行项目运行恢复。当前恢复逻辑为找活动项目运行记录而先构建全部项目模型，再逐项目做 SELECT；没有运行中的项目时，所有这些 SELECT 都只得到 ErrNotFound。项目数量和总任务活跃时间聚合规模增大时，启动恢复产生不必要的串行数据库查询与模型构建。

## 建议方向

提供只返回 status=running 的 ProjectRun 读取路径，并仅为这些项目加载恢复所需的项目列元数据；避免为没有活动队列的项目读取其全部元数据。保留对不存在/被删除项目记录的处理以及恢复过程中的状态校验。

## 验收标准

以 0、1、多个活动运行项目及大量无运行项目验证：启动恢复查询数量不随全部项目数线性增加；只恢复 status=running 的记录，当前卡、列角色、错误处理和恢复顺序不变；测量大项目数量场景下启动恢复的查询数与耗时。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
