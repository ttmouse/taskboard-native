---
id: "OP-061"
title: "旧版 DSH 回执导入每次启动都全量扫描项目任务与评论"
slug: "旧版-dsh-回执导入每次启动都全量扫描项目任务与评论"
status: "inbox"
impact: "medium"
effort: "M"
category: "performance"
family: "F5 读放大、N+1 与缺索引"
created: "2026-11-04"
evidence: "internal/executor/dsh.go:58-64; internal/executor/desktop_legacy.go:14-65; internal/store/queries.go:204-232,353-380; internal/store/relations.go:356-384"
evidence_status: "current-source-confirmed"
evidence_note: "NewDispatcher 每次启动都调用 importDesktopReceipts。该函数先读所有项目，再逐项目调用 TasksByProject(false)，一次加载该项目全部活动卡的完整字段与关系；对每张 doing 卡再逐项查 Execution，缺执行记录时调用 Comments，而 Comments 自身先读取并补全完整任务关系再查全量评论。没有旧 DSH 回执的 doing 卡不会写入标记，因此重启后会再次检查。已成功导入的卡会由新 execution 记录跳过。"
verification: ""
resolution: ""
---

## 现状与证据

为了迁移旧版 DSH 桌面会话回执，服务启动时检查进行中卡片的评论历史并导入显式会话 ID；当前实现扫描范围覆盖所有项目和活动卡。

## 建议方向

将兼容导入改为窄化的批量查询：只取 doing 卡 ID、已有 execution 标志和匹配旧回执所需的 DSH 评论字段，避免装载所有正文、关系和无关评论；保留按评论顺序应用“旧派发回执/后续执行记录清除”的语义，并在确认迁移已完成后评估是否需要持久化导入游标。

## 验收标准

在含大量历史卡片和评论的数据库上验证启动导入查询不再随全部任务正文/关系增长，不存在每卡 Execution/Comments 往返；旧回执 ID 的导入、后续派发/完成评论清除候选、已存在 execution 跳过行为保持一致；对仍无回执的 doing 卡检查多次重启无全量评论重复读取，并测量启动耗时。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
