---
id: "OP-014"
title: "项目范围的 SSE 快照仍遍历全部项目并计算活跃时间"
slug: "项目范围的-sse-快照仍遍历全部项目并计算活跃时间"
status: "inbox"
impact: "medium"
effort: "S"
category: "performance"
family: "F1 服务端 SSE 快照与背压"
created: "2026-11-04"
evidence: "internal/store/queries.go:83-99,115-149; internal/service/sse.go:95-103"
evidence_status: "source-confirmed-unnecessary-work"
evidence_note: "Snapshot(projectID) 先调用 projectsLocked 查询并物化全部项目、所有列，并为每个项目计算活跃任务 MAX(updated_at)，之后才按 projectID 过滤；项目范围 SSE 每次连接/重连都会触发这条路径。全局订阅则需要完整集合，此优化仅针对传入 projectID 的调用。"
verification: ""
resolution: ""
---

## 现状与证据

SSE 支持 projectId 参数；Store.Snapshot 仍先读取全部项目列表与列，再过滤项目并为所选项目载入任务。因此项目范围的订阅在生成快照时执行与全局项目读取规模相当的工作。

## 建议方向

在 projectID 非空时直接查询指定项目及其列与活跃时间，只为该项目构造快照；保留空 projectID 的全局路径。

## 验收标准

多项目数据库中 projectID 快照只触达目标项目的项目/列/任务行；快照内容、Seq 一致性和并发订阅恢复语义与现状相同；提供查询计划或基准证明无关项目增长不再增加耗时。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
