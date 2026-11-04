---
id: "OP-065"
title: "JSON API 接受首个值后的多余请求体内容"
slug: "json-api-接受首个值后的多余请求体内容"
status: "inbox"
impact: "low"
effort: "S"
category: "reliability"
created: "2026-11-04"
evidence: "internal/service/server.go:278-288; docs/contracts.md API 请求体章节"
evidence_status: "current-source-confirmed"
evidence_note: "共享 decodeJSON 只执行一次 json.Decoder.Decode 并在成功后立即返回；因此一个完整 JSON 值后附加第二个值或非空垃圾时不会被检查。该 helper 被多个写接口复用。未运行请求级复现。"
verification: ""
resolution: ""
---

## 现状与证据

共享请求体解析器在首个 JSON 值成功解码后立即返回，没有确认 EOF。写接口因此会接受例如一个合法对象后附加第二个 JSON 值或垃圾字符的请求体，并按首个对象执行操作；调用方的序列化/拼接错误不会被拒绝，客户端也可能误以为整个请求体都被处理。

## 建议方向

完成首个值解码后再尝试解码一个额外值：允许仅含空白的 EOF，若仍有 JSON 值或其他内容则返回 400。保留当前对未知字段的兼容策略，因为客户端可能提交完整 Task。

## 验收标准

覆盖合法单对象、对象后空白、对象后第二个对象、对象后非空垃圾；仅前两类接受，后两类返回 400，合法调用行为不变。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
