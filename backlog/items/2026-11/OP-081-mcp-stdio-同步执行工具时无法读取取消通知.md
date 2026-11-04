---
id: "OP-081"
title: "MCP stdio 同步执行工具时无法读取取消通知"
slug: "mcp-stdio-同步执行工具时无法读取取消通知"
status: "inbox"
impact: "high"
effort: "M"
category: "reliability"
created: "2026-11-04"
evidence: "cmd/taskboard-mcp/main.go:88-107,110-128,153-175; https://modelcontextprotocol.io/specification/2025-06-18/basic/utilities/cancellation"
evidence_status: "source-confirmed-cancellation-unreachable"
evidence_note: "serve handles each scanned line inline; handleLine calls dispatch and tools/call synchronously before scanning the next line. The same path returns immediately for notifications without dispatching them. Thus a client notifications/cancelled line sent while a long-running tool is executing cannot be read or applied until after the tool finishes. MCP 2025-06-18 says receivers should stop cancelled in-progress requests and free resources."
verification: ""
resolution: ""
---

## 现状与证据

The stdio read loop is serial: it does not read another JSON-RPC message until handleLine returns, and tools/call synchronously performs the full HTTP request and optional image fetches. A client cancellation notification arriving during that work remains buffered on stdin, so the tool continues and retains resources until it completes or its HTTP timeout. Even when later read, notifications are currently discarded before any dispatch. This prevents the declared MCP 2025-06-18 cancellation flow from stopping an in-progress tool.

## 建议方向

Separate stdin message reception from request execution. Track cancellable in-flight request contexts by JSON-RPC ID, handle notifications/cancelled promptly, pass cancellation through tool HTTP requests and image fetches, and serialize response writes safely. Ignore unknown/finished IDs and suppress the response for a cancelled request as specified.

## 验收标准

Start a controllably blocked tools/call, then send notifications/cancelled with its requestId on stdin. The server must observe cancellation before the tool's normal completion, cancel downstream HTTP/image reads, free request resources, and send no response for that request; unknown IDs and normal concurrent requests must remain safe.

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
