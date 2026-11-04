---
id: "OP-010"
title: "通配 CORS 允许获准访问 loopback 的网页读取本地看板数据"
slug: "通配-cors-允许获准访问-loopback-的网页读取本地看板数据"
status: "inbox"
impact: "high"
effort: "M"
category: "security"
created: "2026-11-04"
evidence: "internal/service/server.go:180-209,212-223,296-310; internal/service/server.go:480-510"
evidence_status: "source-confirmed-browser-risk"
evidence_note: "Default listener is loopback; all responses set Access-Control-Allow-Origin: *; project/task/board/comment GET routes have no authentication or Origin allowlist. Public-site access to loopback is browser-policy gated in some browsers, but any origin allowed to reach the service can read API responses."
resolution: ""
---

## 现状与证据

本服务默认只绑定 127.0.0.1，这是网络边界；但 middleware 对所有请求响应设置 Access-Control-Allow-Origin: *，同时项目、看板、任务和评论读取端点不要求认证，也不校验 Origin。若网页来源能访问 loopback 服务（例如用户已授予浏览器本地网络权限，或使用未实施该限制的浏览器），该来源的 JavaScript 可读取本地任务数据。

## 建议方向

先确定是否有受支持的浏览器 API 客户端；若没有，移除通配 CORS。若有，只允许明确的受信 origin，并为浏览器请求增加可验证的本地授权/CSRF 边界；不要把 loopback 绑定当作浏览器跨源防护。

## 验收标准

从未受信网页 origin 发起项目、看板、任务、评论读取时，浏览器不能读取响应；若保留受支持的浏览器端，白名单来源在授权后按预期工作；原生 GUI、CLI、MCP 仍能正常访问；默认监听继续为 loopback。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
