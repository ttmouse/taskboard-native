---
id: "OP-071"
title: "MCP 响应超过 4 MiB 后会作为截断文本返回成功"
slug: "mcp-响应超过-4-mib-后会作为截断文本返回成功"
status: "inbox"
impact: "medium"
effort: "S"
category: "reliability"
family: "F8 CLI/MCP 边界"
created: "2026-11-04"
evidence: "cmd/taskboard-mcp/main.go:610-635,370-408; internal/service/server.go:488-612"
evidence_status: "current-source-confirmed"
evidence_note: "MCP do 用 io.ReadAll(io.LimitReader(resp.Body, 4<<20)) 后不检测剩余响应字节，HTTP 状态符合预期时直接把截断字节转为成功文本。list_tasks 的 limit/summary 都是可选参数，服务端可返回项目全部任务正文；单任务写入请求本身允许近 1 MiB 正文，多卡响应可超过 4 MiB。未运行端到端超限复现。"
verification: ""
resolution: ""
---

## 现状与证据

MCP 的共享 HTTP do 将响应体限制为 4 MiB，但 `io.LimitReader` 到达上限会正常返回 EOF，代码没有检查是否还有未读数据。只要 HTTP 状态码符合预期，就将这段被截断的字节作为工具成功文本返回。`list_tasks` 的 limit 与 summary 都可省略，服务端会返回全项目的任务对象和正文；服务端单次 JSON 写入允许接近 1 MiB，请求历史任务累积后容易得到大于 4 MiB 的列表。调用方拿到的是不完整 JSON 文本却收到 `isError=false`。

## 建议方向

读取上限加一字节以检测超限，或采用明确可配置的 MCP 响应预算；超限时返回工具错误并提示使用 summary、分页或 get_task 单卡读取。不能把截断 JSON 作为成功数据返回。

## 验收标准

构造小于、恰好达到和超过响应上限的 JSON 工具结果；小响应完整返回，超限必须明确标记 isError/工具错误且不得返回截断成功文本；list_tasks 分页和 summary 能读取完整数据。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
