---
id: "OP-017"
title: "任务列表的 limit 与 summary 在全量读取和关系补全之后才生效"
slug: "任务列表的-limit-与-summary-在全量读取和关系补全之后才生效"
status: "inbox"
impact: "high"
effort: "M"
category: "performance"
family: "F5 读放大、N+1 与缺索引"
created: "2026-11-04"
evidence: "internal/service/server.go:568-599; internal/store/queries.go:204-232; internal/store/queries.go:14-19; internal/store/relations.go:356-384"
evidence_status: "source-confirmed-unbounded-work-before-pagination"
evidence_note: "GET /api/tasks 接受 limit≤200、offset 和 summary，但先调用 TasksByProject 读出项目全部匹配任务。tasksLocked 对全量行读取完整 taskColumns（含每卡相关子查询读取最新执行完成评论），随后 enrichTasksLocked 读取全量任务关系；服务端此后才筛选 column/assignee、裁 limit/offset、投影 summary。小页摘要请求仍承担全量任务加载/补全成本。"
verification: ""
resolution: "- 2026-11-04: 补充每次列表调用的前置元数据查询（2026-11-04）：Store.TasksByProject 在 tasksLocked 前调用 projectLocked 仅验证项目存在，丢弃返回值；projectLocked 会计算该项目活动任务 MAX(updated_at) 并额外查询所有列（internal/store/queries.go:159-183,204-210）。因此即使无 columnId，普通 GET /api/tasks 也先做两次项目/列 SQL 再查询任务；columnId 请求还会叠加 handler 的 Project 读取（该重复校验已由 OP-052 单独记录）。列表路径可用窄存在性检查，或在分页/筛选 SQL 中保持原 404 语义而避免构造完整 Project。验收记录普通及 column-filter 列表的项目元数据/列查询数。"
---

## 现状与证据

CLI/MCP 的 list_tasks 明确支持分页和 summary；README 引导 Agent 先通过摘要列表定位卡片。当前这些选项主要缩小响应体，没有限制数据库读取、扫描及关系补全范围。

## 建议方向

将项目/列/assignee/归档条件和稳定排序的 limit/offset 下推到 SQL；summary 使用独立轻量 SELECT，跳过完整正文、完成评论子查询与关系补全。完整详情列表保留所需字段。测量大项目下 limit=20 与 summary 对比当前实现的查询数、扫描/分配量和延迟。

## 验收标准

代表性大项目下带过滤、分页的结果与旧实现语义一致；summary 不读取正文、完成评论或关系，limit/offset 让扫描/分配量受请求规模约束；CLI/MCP/API 契约和全量默认查询保持兼容。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。

补充每次列表调用的前置元数据查询（2026-11-04）：Store.TasksByProject 在 tasksLocked 前调用 projectLocked 仅验证项目存在，丢弃返回值；projectLocked 会计算该项目活动任务 MAX(updated_at) 并额外查询所有列（internal/store/queries.go:159-183,204-210）。因此即使无 columnId，普通 GET /api/tasks 也先做两次项目/列 SQL 再查询任务；columnId 请求还会叠加 handler 的 Project 读取（该重复校验已由 OP-052 单独记录）。列表路径可用窄存在性检查，或在分页/筛选 SQL 中保持原 404 语义而避免构造完整 Project。验收记录普通及 column-filter 列表的项目元数据/列查询数。
