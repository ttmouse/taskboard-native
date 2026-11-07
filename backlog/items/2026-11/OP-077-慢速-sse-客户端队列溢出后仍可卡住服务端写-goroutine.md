---
id: "OP-077"
title: "慢速 SSE 客户端队列溢出后仍可卡住服务端写 goroutine"
slug: "慢速-sse-客户端队列溢出后仍可卡住服务端写-goroutine"
status: "inbox"
impact: "medium"
effort: "M"
category: "reliability"
family: "F1 服务端 SSE 快照与背压"
created: "2026-11-04"
evidence: "internal/service/sse.go:16-25,36-51,132-155; internal/service/server.go:143-147; internal/service/server_test.go:493-557"
evidence_status: "source-confirmed-blocking-write-path"
evidence_note: "The hub disconnects an overflowing subscriber by closing sub.done, but the handler only checks done between writes; an in-progress ResponseWriter Write/Flush is not interrupted by that channel. The HTTP server config has no WriteTimeout and there is no per-write deadline. A connected peer that stops reading can fill TCP buffers and retain the handler indefinitely; this stream path is not covered by tests."
verification: ""
resolution: ""
---

## 现状与证据

SSE event fanout is nonblocking at the hub, but the HTTP writer can still block on a slow or stalled client. Once an event write blocks, further store events fill the 64-event queue and hub.publish removes the subscriber and closes done; the handler cannot observe done until the blocked network write returns. With no HTTP WriteTimeout or per-write deadline, a peer that keeps the TCP connection open without reading can pin a handler and connection, consuming resources and potentially preventing graceful server shutdown from draining.

## 建议方向

Add a bounded write strategy compatible with long-lived SSE streams, such as per-frame deadlines via ResponseController and prompt connection termination when a subscriber is dropped. Preserve normal idle streams with periodic ping and snapshot-based recovery after reconnect; do not impose a global fixed WriteTimeout that silently expires healthy SSE sessions.

## 验收标准

Use a real TCP client that completes the SSE handshake and then stops reading; send enough sizable events to block the socket and overflow the hub queue. The server must release the handler/connection within a bounded interval, normal readers must continue receiving events and pings, and reconnect must restore state through the snapshot.

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
