---
id: "OP-102"
title: "API 请求体大小受限但读取时间无上限"
slug: "api-请求体大小受限但读取时间无上限"
status: "inbox"
impact: "medium"
effort: "S"
category: "reliability"
created: "2026-11-04"
evidence: "internal/service/server.go:143-147,278-287,897-911; internal/service/images.go:40-45"
evidence_status: "source-confirmed-unbounded-body-read-duration"
evidence_note: "http.Server 只配置 ReadHeaderTimeout=5s，没有 ReadTimeout 或请求体级 deadline。JSON handler 通过 MaxBytesReader 限制 1 MiB，派发和图片上传也限制字节数，但慢速客户端可在字节上限内持续发送；读取函数同步等待 body 完成。每个未完成请求会持续占用连接和 handler goroutine。"
verification: ""
resolution: ""
---

## 现状与证据

服务端设置了 5 秒请求头读取超时，但没有请求体读取期限。JSON、派发和图片上传路径都在 handler 中同步读取受最大字节数约束的 body；字节上限不能限制传输耗时。慢速或停发 body 的本地客户端可以长期占住连接和 goroutine，并可通过并发请求放大占用。当前只绑定 loopback，影响范围是能访问本机服务的进程或页面。

## 建议方向

为请求体读取设置有界时限或最低传输速率策略，确保过期连接被取消并释放 handler 资源；时限需覆盖正常的大图片上传，同时不限制 SSE 响应的持续连接时长。

## 验收标准

对 JSON 与图片上传分别验证正常速率的大/小 body 可成功，超过时限的停发或慢速 body 被及时终止且不触发写操作；大量超时请求后服务仍可处理健康检查与正常 API，请求读取期限不截断 SSE 响应。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
