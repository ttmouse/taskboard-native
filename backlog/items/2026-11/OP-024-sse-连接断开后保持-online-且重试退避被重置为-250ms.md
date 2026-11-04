---
id: "OP-024"
title: "SSE 连接断开后保持 Online 且重试退避被重置为 250ms"
slug: "sse-连接断开后保持-online-且重试退避被重置为-250ms"
status: "inbox"
impact: "medium"
effort: "S"
category: "reliability"
created: "2026-11-04"
evidence: "internal/client/client.go:665-701,729-765; internal/client/client.go:638-647; cmd/taskboard/view.go:144-149; internal/client/client_test.go:309-340"
evidence_status: "source-confirmed-reconnect-state-bug"
evidence_note: "consume 在 HTTP 200 后、读取任何事件前就发出 Online；Subscribe 的 reached 因此置 true。流随后 EOF/扫描错误时不会发 Offline，且 reached=true 会把 backoff 重置为 250ms。客户端状态注释定义 Online 为“events are flowing”，UI 只在 Offline 展示重连横幅。现有 TestStreamReconnects 只检验一开始连接死端口的 Offline，不检验已建立流随后掉线。"
verification: ""
resolution: ""
---

## 现状与证据

运行中的事件流断开时，客户端会继续重试，但状态可能一直显示 Online，用户看不到已有的“本地服务未连接，正在重试”提示；由于每次 HTTP 握手又立刻把 reached 置真，重试周期保持最短 250ms，服务不可用时约每秒四次连接尝试。

## 建议方向

将 Online 绑定到成功解析并交付初始快照/有效事件；已建立连接在 EOF、Scanner 错误或事件解码失败时切换到 Offline/Connecting，并让连续失败应用指数退避，成功收到事件后再重置。

## 验收标准

测试流在发送快照后中断、200 响应但快照格式损坏、持续失败与恢复：断连状态可见、连续失败间隔逐步增加、恢复后完整快照应用且 Online 恢复；初次无法连接仍显示 Offline。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
