---
id: "OP-023"
title: "任务列表忽略非法 includeArchived 值并静默按 false 查询"
slug: "任务列表忽略非法-includearchived-值并静默按-false-查询"
status: "inbox"
impact: "low"
effort: "S"
category: "reliability"
family: "F8 CLI/MCP 边界"
created: "2026-11-04"
evidence: "internal/service/server.go:556-568; internal/service/server.go:544-553; internal/service/server_test.go:372-380; docs/contracts.md:43"
evidence_status: "source-confirmed-silent-parse-error"
evidence_note: "includeArchived 使用 strconv.ParseBool 并丢弃 error，非法值（如 includeArchived=tru）会变成 false，接口仍返回 200 且排除归档任务。同一 handler 的 summary 参数则明确校验 true/false 并对非法值返回 400。现有测试仅覆盖 includeArchived=true 的成功路径。"
verification: ""
resolution: ""
---

## 现状与证据

CLI/MCP 通过 HTTP 调用 GET /api/tasks，includeArchived 用于决定是否带回归档卡片。调用方拼写或序列化错误时，当前响应看起来成功但结果悄悄少卡，排查原因困难。

## 建议方向

若参数未提供则使用 false；若提供，则显式解析并校验受支持的布尔形式，非法值返回 400 和参数名提示。将 accepted syntax 写入 API 契约并覆盖 true/false/非法值。

## 验收标准

未提供时保持默认不含归档任务；合法 true/false 按预期工作；非法字符串返回 400，不再返回看似成功的不完整列表；CLI/MCP 发送的标准 true 参数兼容。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
