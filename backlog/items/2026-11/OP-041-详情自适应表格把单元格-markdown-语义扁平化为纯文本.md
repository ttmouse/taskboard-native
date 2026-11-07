---
id: "OP-041"
title: "详情自适应表格把单元格 Markdown 语义扁平化为纯文本"
slug: "详情自适应表格把单元格-markdown-语义扁平化为纯文本"
status: "inbox"
impact: "medium"
effort: "M"
category: "product"
family: "F10 产品体验"
created: "2026-11-04"
evidence: "cmd/taskboard/markdown_table_fit.go:39-76; cmd/taskboard/detail_images.go:284-288; cmd/taskboard/body_table_ui.go:91-104,181-190; /Users/douba/go/pkg/mod/github.com/egoist/mygo@v0.6.3/ui/markdown/parser/html.go:313-328"
evidence_status: "current-source-confirmed"
evidence_note: "任务正文和评论中的表格由 fitMarkdownTable 接管渲染；每个 cell 通过 parser.PlainText 转字符串后统一交给 ui.Text。PlainText 遍历子节点保留链接标签/代码文本，但不会保留链接目标、强调样式或图片渲染，因此不是单元格文字消失，而是 Markdown 的交互/格式语义被降级。图片 alt 文本可能保留，但图像本身不显示。现有自适应表格目标是窄窗填宽；语义损失范围尚无用户数据。"
verification: ""
resolution: ""
---

## 现状与证据

自适应表格便于窄窗口阅读，但与同一正文中的普通 Markdown.View 内容相比，单元格链接不可点、粗斜体和图片呈现变成普通文字。评论表格同样走此渲染器。

## 建议方向

评估在等宽单元格布局中复用 Markdown inline renderer，保留链接点击、强调、代码和图片；若完整复用会破坏单元格高度/换行，先明确支持范围并优雅降级复杂块。

## 验收标准

正文与评论表格中的链接标签可点击到原目标，粗/斜体与代码维持样式，图片按既有图片权限和尺寸规则展示；纯文本及窄窗换行不回退，复制/选择行为与当前一致。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
