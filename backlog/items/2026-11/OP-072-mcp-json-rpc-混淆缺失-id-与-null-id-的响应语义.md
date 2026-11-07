---
id: "OP-072"
title: "MCP JSON-RPC 混淆缺失 ID 与 null ID 的响应语义"
slug: "mcp-json-rpc-混淆缺失-id-与-null-id-的响应语义"
status: "inbox"
impact: "low"
effort: "S"
category: "reliability"
family: "F8 CLI/MCP 边界"
created: "2026-11-04"
evidence: "cmd/taskboard-mcp/main.go:68-73,111-128; JSON-RPC 2.0 Specification sections 4.1 and 5"
evidence_status: "current-source-confirmed"
evidence_note: "response.ID 使用 omitempty 导致 parse/invalid-request 错误在 req.ID nil 时序列化时省略必需 id:null；handleLine 又把缺失 ID 与显式 RawMessage(\"null\") 合并为 no-response notification。JSON-RPC 2.0 将 notification 定义为没有 id 成员，null 是允许但不推荐的 ID 值，并要求未知请求 ID 的错误响应使用 id:null。"
verification: ""
resolution: ""
---

## 现状与证据

MCP JSON-RPC 响应把 `ID` 标记为 `omitempty`，因此解析错误和缺少 ID 的无效请求返回时会省略响应中的 `id`，而不是规范要求的 `id:null`。此外 handleLine 把 RawMessage 为空和字面值 `null` 都视为 notification 并不发响应；规范按是否存在 id 成员定义 notification，null 虽不推荐但仍是可表达的 ID。与 JSON-RPC 2.0 对 parse/invalid-request 示例和响应对象字段要求不一致，可能让严格客户端无法关联或忽略错误响应。

## 建议方向

区分 ID 成员不存在与显式 null；只有前者按 notification 不回包。错误响应若无法解析出请求 ID，应显式序列化 `id:null`（不要用 omitempty 丢掉字段），同时继续原样回显合法请求 ID。

## 验收标准

核对 malformed JSON、缺少 id 的 invalid request、显式 null id 的请求、普通 notification 和普通 string/integer ID 请求：前两类错误响应必须含 id:null，null-ID 请求按请求处理并回 id:null，缺 id notification 不回包，其他请求原样关联 ID。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
