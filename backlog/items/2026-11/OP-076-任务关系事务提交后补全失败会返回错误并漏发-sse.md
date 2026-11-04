---
id: "OP-076"
title: "任务关系事务提交后补全失败会返回错误并漏发 SSE"
slug: "任务关系事务提交后补全失败会返回错误并漏发-sse"
status: "inbox"
impact: "medium"
effort: "M"
category: "reliability"
created: "2026-11-04"
evidence: "internal/store/relations.go:120-137,204-215; internal/store/relations.go:347-385; internal/service/server.go:801-830"
evidence_status: "source-confirmed-post-commit-error-path"
evidence_note: "AddTaskRelation and RemoveTaskRelation commit relation and task-version changes before enrichTaskLocked performs additional queries; an enrichment error returns before result/events are emitted. The HTTP handler then returns an error although the mutation is durable, and clients receive no task.updated event. Retrying an add can report duplicate relation."
verification: ""
resolution: ""
---

## 现状与证据

Relation add/remove is committed in a SQLite transaction, but both methods then perform fallible enrichment queries before publishing task.updated events and returning success. If one post-commit read fails, the caller sees an error despite the relation and task version already changing; SSE subscribers get no mutation event and may retain stale relation summaries. Retrying an add then hits the duplicate-relation check. AddTaskRelation also has an extra post-commit read for the previous parent when replacing a parent.

## 建议方向

Make the post-commit response/event path non-failing where possible: derive required relation summaries from the committed mutation or use a transactional result snapshot; otherwise explicitly represent the mutation as committed and ensure an event/snapshot repair is guaranteed. Do not simply emit events before the transaction commits.

## 验收标准

Inject a failure in relation enrichment after the transaction succeeds for add, remove, and parent replacement. The API result must accurately report committed state, subscribers must converge without waiting for unrelated reconnection, and retry behavior must not mislead callers or duplicate the relation; transaction failures must still emit no success event.

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
