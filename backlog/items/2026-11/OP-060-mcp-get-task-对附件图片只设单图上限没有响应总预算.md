---
id: "OP-060"
title: "MCP get_task 对附件图片只设单图上限没有响应总预算"
slug: "mcp-get-task-对附件图片只设单图上限没有响应总预算"
status: "inbox"
impact: "medium"
effort: "M"
category: "performance"
created: "2026-11-04"
evidence: "cmd/taskboard-mcp/main.go:153-179,644-694; docs/contracts.md:137-141"
evidence_status: "current-source-confirmed"
evidence_note: "get_task 会扫描任务正文与全部评论中的唯一图片 URL，逐张同步 fetchImage；每张最多读取 25 MiB，但唯一图片数量及累计原始字节均无上限。每张读完后会把 base64 字符串保存在 blocks 中直到整个工具结果编码，因此累计输出体积至少按图片数线性增长且有约 4/3 编码膨胀；调用处理期间也同时持有完整任务 JSON、解析后的评论文本和所有已生成图像块。单图限制来自上传约定，不能限制历史/多评论中的总量。"
verification: ""
resolution: ""
---

## 现状与证据

MCP get_task 为了让模型直接看到附件，会把正文与评论中的所有不同图片下载后作为图像内容块追加到同一条响应；请求按图片串行进行。

## 建议方向

为 get_task 设计单次调用的图片数量与累计字节预算，超预算时保留任务 JSON/原始图片引用并明确标出省略数量；可减少额外副本并评估有界并发提速。若更改可见 MCP 返回内容，先更新 docs/contracts.md，再改实现。

## 验收标准

用包含多条评论、多个唯一图片且单图接近上传上限的卡片验证响应内存和大小受到总预算约束、下载数量/字节不超过上限；超出时文本、图片 URL 与省略提示明确，普通小图卡片仍返回完整图像块；对串行与有界并发的延迟及峰值内存做测量。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
