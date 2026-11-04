---
id: "OP-066"
title: "任务 ID 前缀解析使用 LIKE 导致扫描全表"
slug: "任务-id-前缀解析使用-like-导致扫描全表"
status: "inbox"
impact: "medium"
effort: "S"
category: "performance"
created: "2026-11-04"
evidence: "internal/store/queries.go:276-327; internal/store/store.go:55-80; SQLite EXPLAIN QUERY PLAN on equivalent tasks schema"
evidence_status: "current-source-confirmed"
evidence_note: "resolveTaskIDLocked 对非完整十六进制 ID 执行 id LIKE prefix% 两次查询；tasks.id 只有 BINARY 主键索引。系统 SQLite 对等表结构的计划显示 LIKE 查询为 SCAN tasks，而二进制范围谓词可 SEARCH 主键索引。仓库嵌入 SQLite 的具体执行计划尚未直接读取，需实现前复核。"
verification: ""
resolution: ""
---

## 现状与证据

人类任务号/精确 ID 优先走索引，但短十六进制前缀解析走 `id LIKE ? ESCAPE ...` 并过滤 archived。当前 tasks 表只有 id 主键索引，没有 LIKE 兼容索引。用系统 SQLite 对等 schema 查询 `EXPLAIN QUERY PLAN` 得到 `SCAN tasks`；将谓词换为二进制前缀范围后可使用 `sqlite_autoindex_tasks_1`。任务数量增长时，短前缀读取会逐行扫描任务表，零匹配时还会再做一次包含归档任务的扫描。

## 建议方向

使用保持十六进制前缀语义的二进制范围查询（prefix <= id < prefixSuccessor），或建立并验证适配当前 SQLite/LIKE 语义的索引；保留转义与唯一匹配规则，明确处理前缀字符集。

## 验收标准

在项目使用的实际 SQLite 引擎上对有代表性的 tasks 表执行 EXPLAIN QUERY PLAN，证明前缀查询走 id 索引；覆盖活跃/归档任务、唯一匹配、歧义、百分号/下划线等输入，结果与现契约一致。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
