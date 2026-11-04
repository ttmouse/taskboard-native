---
id: "OP-105"
title: "共享 HTTP 客户端先缓冲完整 JSON 响应再解码"
slug: "共享-http-客户端先缓冲完整-json-响应再解码"
status: "inbox"
impact: "medium"
effort: "S"
category: "performance"
created: "2026-11-04"
evidence: "internal/client/client.go:178-215"
evidence_status: "source-confirmed-response-copy-before-decode"
evidence_note: "Client.do 对所有 HTTP 响应先 io.ReadAll；成功响应随后 json.Unmarshal(raw,out)。在 Unmarshal 返回前，raw 字节切片和构造中的完整 model/Board/Comment 对象同时存活。Board、Task、Projects、Comments 等 payload 没有统一响应字节上限，因此峰值额外驻留随响应 JSON 大小增长。SSE 单独流式读取，不属于此项。"
verification: ""
resolution: ""
---

## 现状与证据

共享 HTTP 客户端把成功响应完整读取进 raw []byte 后再反序列化为输出对象。大型项目看板、任务评论历史或项目列表在解码期间同时保留原始 JSON 和已分配模型字段，形成一份与响应大小近似线性增长的额外临时内存；客户端最终仍需保留模型对象，但不需要完整 raw 副本。

## 建议方向

对 2xx JSON 响应直接使用 json.Decoder 从 resp.Body 解码到 out；无响应体/ out=nil 的写入保持兼容。非 2xx 错误路径可保留有界诊断读取，并继续正确解码带当前任务的 409。不要给正常大型看板引入未经契约定义的硬截断。

## 验收标准

用大 Board、长评论历史和大任务响应记录峰值 RSS/分配量；解码期间不再完整持有原始 JSON 副本，返回对象与现有字段值一致，空响应和 204 成功、非 2xx 错误、携带当前 Task 的 409、响应体关闭及连接复用语义均正确。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
