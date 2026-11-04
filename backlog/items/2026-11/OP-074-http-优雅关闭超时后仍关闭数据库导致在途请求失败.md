---
id: "OP-074"
title: "HTTP 优雅关闭超时后仍关闭数据库导致在途请求失败"
slug: "http-优雅关闭超时后仍关闭数据库导致在途请求失败"
status: "inbox"
impact: "medium"
effort: "M"
category: "reliability"
created: "2026-11-04"
evidence: "internal/service/server.go:158-170,1001-1053; internal/store/store.go:172-175"
evidence_status: "source-confirmed-shutdown-race"
evidence_note: "Close waits up to 3s for HTTP Shutdown, then closes dispatch and Store regardless of shutdownErr; active handlers may continue after deadline and make later Store calls against a closed DB"
verification: ""
resolution: ""
---

## 现状与证据

Server.Close gives HTTP handlers up to three seconds to drain, but on timeout it immediately closes the dispatcher and Store while handlers may still be in progress. Store.Close serializes only with a currently-held Store mutex; it does not join complete handler lifetimes. A handler making sequential Store calls can therefore finish one call before close, then fail a later call after the database closes, leaving an incomplete request during shutdown.

## 建议方向

Define an explicit forced-shutdown path after the graceful timeout: stop or cancel remaining HTTP work, wait for handler/store operations to settle or return a clear shutdown error, and close Store only after no handler can use it. Preserve bounded shutdown and document how active requests are terminated.

## 验收标准

With a deterministic handler held across the three-second Shutdown deadline and performing a second Store operation afterward, Close must not close Store underneath it or leave the request with a misleading partial result; shutdown must remain bounded and subsequent requests must be rejected.

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
