---
id: "OP-068"
title: "Board 请求遇到任意错误都会降级成多次请求"
slug: "board-请求遇到任意错误都会降级成多次请求"
status: "inbox"
impact: "low"
effort: "S"
category: "reliability"
family: "F2 客户端连接与过期状态"
created: "2026-11-04"
evidence: "internal/client/client.go:352-374; cmd/taskboard/board.go:562-569,647-652"
evidence_status: "current-source-confirmed"
evidence_note: "Board 注释将 fallback 限定为 endpoint absent，但实现丢弃首个 get 错误，对 500、超时、连接错误和 JSON 解码错误都继续 GET projects 与 tasks。GUI 的单项目初次加载及项目切换直接调用该方法。客户端默认 HTTP timeout 10 秒，单次故障可触发额外串行请求。"
verification: ""
resolution: ""
---

## 现状与证据

Client.Board 的兼容回退原本用于旧服务没有 `/api/projects/{id}/board` 路由，但代码对第一请求的任何错误都执行 fallback。HTTP 500、超时/断连、响应 JSON 损坏都会被吞掉，然后串行请求项目列表和任务列表。GUI 的初次项目加载和切换都调用 Board；客户端每个请求默认超时 10 秒，因此超时情况下会额外延长加载等待，且若 fallback 成功还会把 Board 路由故障隐藏成成功结果。

## 建议方向

仅对可证明路由不存在的状态（例如 404/405）走旧接口兼容；其他状态保留并返回原始错误，不再发起 fallback。若历史服务可能使用其他状态码表示缺少路由，应先定义兼容判别条件。

## 验收标准

服务返回 Board 200 时只发一次请求；Board 404/405 时依次调用兼容接口；Board 500、超时、连接失败或 JSON 错误时直接返回该错误，不请求 Projects/Tasks，也不静默返回 fallback 结果。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
