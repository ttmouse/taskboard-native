---
id: "OP-031"
title: "SSE 同一事件会由每个订阅连接重复 JSON 编码"
slug: "sse-同一事件会由每个订阅连接重复-json-编码"
status: "inbox"
impact: "low"
effort: "S"
category: "performance"
family: "F1 服务端 SSE 快照与背压"
created: "2026-11-04"
evidence: "internal/service/sse.go:38-50,126-140"
evidence_status: "code-path-confirmed"
evidence_note: "hub.publish 将同一 model.Event 分发给每个匹配订阅者；每个 handleEvents 请求各自在收到事件后调用 json.Marshal(ev)。因此单次更新的编码工作随匹配连接数线性重复，且卡片事件包含完整任务字段，较大事件会放大编码 CPU。真实订阅数与事件速率未采样，故影响暂列低。"
verification: ""
resolution: ""
---

## 现状与证据

同一事件内容对所有连接相同，订阅者各自发送 SSE 时重复构建 JSON 字节。

## 建议方向

先基准测量多订阅者与大任务事件负载；若编码明显，可在事件分发前编码一次并安全共享不可变字节，或者以有界缓存复用编码结果，同时保留每连接写失败的独立处理。

## 验收标准

覆盖不同项目过滤的订阅连接，确认每个连接收到的 JSON 与当前内容一致、序号和事件名正确；基准显示编码调用数及 CPU 随连接数不再线性重复，且不会引入无界事件缓存。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
