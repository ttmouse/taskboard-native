---
id: "OP-064"
title: "按任务号或短前缀读取评论时未使用解析后的任务 ID"
slug: "按任务号或短前缀读取评论时未使用解析后的任务-id"
status: "inbox"
impact: "medium"
effort: "S"
category: "reliability"
family: "F5 读放大、N+1 与缺索引"
created: "2026-11-04"
evidence: "internal/store/queries.go:353-361; internal/store/queries.go:254-270; internal/service/server.go:972-979; docs/contracts.md:51,66"
evidence_status: "current-source-and-contract-confirmed"
evidence_note: "Store.Comments 调用 taskLocked(taskID) 验证/解析任务标识，但丢弃返回的 model.Task，随后仍以原始 taskID 调 commentsLocked。comments 表按规范 UUID task_id 存储，因此传十进制 number 或唯一 hex 前缀时会验证成功却查不到该任务评论。HTTP GET /api/tasks/{id}/comments 直接使用此方法；契约明确评论与按任务 ID 定位的接口接受任务号和唯一短前缀。"
verification: ""
resolution: ""
---

## 现状与证据

API 支持用全局任务号、完整 ID 或唯一短前缀定位任务；评论列表端点验证标识后却以未规范化的原字符串查 comments 表，合法别名会得到空数组。

## 建议方向

保留 taskLocked 的所有标识解析与 not-found/歧义错误行为，保存其返回的规范 task.ID，并用该 ID 查询评论。检查同一服务内其它接受别名后直接按原始字符串查子表的路径。

## 验收标准

用同一任务的完整 ID、十进制任务号和唯一短前缀调用 GET /api/tasks/{id}/comments，均返回完全相同且按创建时间排序的评论；不存在/歧义标识继续返回既有错误，任务隔离不变。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
