---
id: "OP-109"
title: "删除项目后仍保留自动承接 ProjectRun 记录"
slug: "删除项目后仍保留自动承接-projectrun-记录"
status: "inbox"
impact: "medium"
effort: "S"
category: "reliability"
family: "F3 自动承接与执行状态机"
created: "2026-11-04"
evidence: "internal/store/store.go:102-109; internal/store/mutations.go:587-607; internal/store/project_run.go:14-28; internal/service/server.go:954-960"
evidence_status: "observed"
evidence_note: "project_runs.project_id 是主键但未声明外键；DeleteProject 的事务未删除 project_runs；Store.ProjectRun 直接按 ID 查表，GET /api/projects/{id}/run 不验证项目仍存在。因此删除后可按旧项目 ID 读到遗留状态、taskIds 与 detail。启动恢复仅遍历现存项目，不会清除该行。"
verification: ""
resolution: ""
---

## 现状与证据

项目删除注释与事务承诺删除项目及其从属数据，但事务只清理 followups、executions、comments、task_relations、tasks、columns 和 projects，遗漏 project_runs。

## 建议方向

在删除项目事务中同步删除 project_runs，或建立可靠的清理/级联策略；先确认接口契约对已删除项目的 run 查询应返回 404，再更新 docs/contracts.md 与实现，避免保留旧 taskIds 和运行详情。

## 验收标准

删除含自动承接记录的项目后，project_runs 行不再存在，GET /api/projects/{id}/run 返回契约规定的 not found；删除事务回滚时项目与运行记录同时保留；旧数据库无需全表重建，现存项目的运行恢复正常。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
