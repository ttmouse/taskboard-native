---
id: "OP-036"
title: "任务详情的表格正文和评论每次渲染都会重新解析 Markdown"
slug: "任务详情的表格正文和评论每次渲染都会重新解析-markdown"
status: "inbox"
impact: "medium"
effort: "M"
category: "performance"
created: "2026-11-04"
evidence: "cmd/taskboard/body_table_ui.go:19-36,147-193; cmd/taskboard/body_table.go:41-59; /Users/douba/go/pkg/mod/github.com/egoist/mygo@v0.6.3/ui/markdown/view.go:136-163"
evidence_status: "code-path-confirmed"
evidence_note: "detailBodyView 每次 UI 构建调用 bodyTableSpans(d.body)；只要文本包含竖线，该函数就完整 parser.Parse 正文。commentBodyView 对每条评论也调用 bodyTableSpans；有表格的评论在处理每个表格块时还会再次 parser.Parse(src) 寻找 table block。MyGo markdown.View 自身按元素状态缓存已解析文档，但不会覆盖调用方每次 bodyTableSpans 和表格块 Parse 的额外解析。非表格文本无竖线时有快速跳过；影响主要在含表格的详情与评论，具体帧成本未测。"
verification: ""
resolution: ""
---

## 现状与证据

编辑器/评论详情窗口在正常帧重建 UI 时，静态 Markdown 正文和评论内容保持不变，但表格范围识别仍重新解析源文本；带表格的评论列表可能重复解析多段内容。

## 建议方向

在 detail 内容或评论变化时解析并缓存表格跨度及文档块索引，渲染期间复用；缓存键需包含源文本版本/哈希，并在评论编辑、正文变更及 SSE 更新后失效。避免为现有 Markdown View 重复维护整份 AST，优先复用本轮已算出的 parser.Document。

## 验收标准

表格正文、跨表格纯文本、代码围栏中的管道符、评论编辑与 SSE 更新结果和当前一致；基准对稳定打开的含表格任务详情测量每帧 Parse 调用数/CPU，解析次数只随内容变化增加，不随渲染帧数增加。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
