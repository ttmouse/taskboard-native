---
id: "OP-067"
title: "tb comment 将以连字符开头的正文解析成 flag"
slug: "tb-comment-将以连字符开头的正文解析成-flag"
status: "inbox"
impact: "low"
effort: "S"
category: "developer-experience"
created: "2026-11-04"
evidence: "cmd/tb/main.go:555-570,738-767; docs/cli-mcp-audit-2026-10-31.md:61-62"
evidence_status: "current-source-confirmed"
evidence_note: "cmdComment 对完整 args 调用 reorderArgs；该函数遇到任何以 - 开头的 token 都移动到 flags 区并尝试消费后续 token，之后 flag.Parse 会因未知 flag 退出 usage。显式 -- 能绕过，但 CLI usage 未说明，且一般评论文本不应要求用户掌握内部 flag 解析规则。"
verification: ""
resolution: ""
---

## 现状与证据

评论正文以连字符开头（例如 `tb comment 1001 -请检查`）时，cmdComment 会先把 taskID、正文与 flag 混在一起交给 reorderArgs。重排器将 `-请检查` 当成 flag，再把后续 token 当作 flag 值或交给 flag.Parse，命令失败而不会创建评论。加 `--` 可避开重排，但现有 usage 和项目文档没有提示。当前实现确认；已有评论 CLI 测试没有覆盖前导连字符正文。

## 建议方向

解析完已知选项后保留正文尾部为原始位置参数，或在 CLI 明确支持 `--` 时同步更新 usage 与示例；保证 flags 可出现在正文前后时不会把正文 token误识别为选项。

## 验收标准

覆盖普通正文、正文以单连字符/双连字符开头、正文中间包含 flag 样式 token、mention/addr/agent flags 位于正文前后；写入正文须逐字保留，未知真正选项仍应返回 usage 错误。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
