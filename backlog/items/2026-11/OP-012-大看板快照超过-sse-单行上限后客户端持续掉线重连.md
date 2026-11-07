---
id: "OP-012"
title: "大看板快照超过 SSE 单行上限后客户端持续掉线重连"
slug: "大看板快照超过-sse-单行上限后客户端持续掉线重连"
status: "inbox"
impact: "high"
effort: "M"
category: "reliability"
family: "F1 服务端 SSE 快照与背压"
created: "2026-11-04"
evidence: "internal/service/sse.go:102-123,140-151; internal/client/client.go:732-763; internal/service/server.go:278-286; internal/model/model.go:114-134,328-342; internal/store/mutations.go:126,222-225"
evidence_status: "source-confirmed-cross-layer-limit-mismatch"
evidence_note: "SSE 服务端把整个 snapshot JSON 写入单个 data 行；客户端 bufio.Scanner 将单行最大 token 固定为 1 MiB。Scanner 超限会使 consume 返回错误，Subscribe 随后重连且每次先读取同一完整快照。服务端 JSON 请求体上限也为 1 MiB，任务描述可使单个任务接近此界，完整多任务快照更容易超过客户端行限。尚未运行专门复现。"
verification: ""
resolution: "- 2026-11-04: 证据补充：问题也可由单张大卡触发。model.Event 携带完整 Task（含 Description），创建/更新事件直接把该 Task 编码成单行 SSE；任务请求体允许接近 1 MiB。该事件越过客户端 Scanner 上限后，重连快照仍包含相同正文，因此同样无法完成恢复。验收需同时覆盖超限的 task event 和初始 snapshot。"
---

## 现状与证据

初始 SSE 快照编码为一行 JSON (`data: %s`)，每次连接都发送；客户端使用 Scanner 读取行，最大 token 为 1<<20。达到 Scanner 限制时报错退出，Subscribe 以指数退避重新请求，但服务器重连仍先发送完整快照，因此过大快照无法恢复。

## 建议方向

让 SSE 帧读取支持大 payload 或改为有界分块/分页快照，并确保快照恢复协议可处理大看板；补充超过 1 MiB 且能在重连后成功应用的集成覆盖。

## 验收标准

构造序列化大小超过 1 MiB 的有效看板快照；客户端能完整解析初始快照、保持在线，并在断线后再次恢复；常规事件与 SSE 协议行为不回退。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。

证据补充：问题也可由单张大卡触发。model.Event 携带完整 Task（含 Description），创建/更新事件直接把该 Task 编码成单行 SSE；任务请求体允许接近 1 MiB。该事件越过客户端 Scanner 上限后，重连快照仍包含相同正文，因此同样无法完成恢复。验收需同时覆盖超限的 task event 和初始 snapshot。
