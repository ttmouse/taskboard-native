---
id: "OP-059"
title: "CLI 执行把完整 stdout 与 stderr 无上限保留到进程结束"
slug: "cli-执行把完整-stdout-与-stderr-无上限保留到进程结束"
status: "inbox"
impact: "medium"
effort: "S"
category: "performance"
created: "2026-11-04"
evidence: "internal/executor/dsh.go:437-445,455,491-508; internal/executor/diagnostic.go:5-25"
evidence_status: "current-source-confirmed"
evidence_note: "executeCLI 为每个子进程创建无上限 bytes.Buffer，stdout 和 stderr 从 Start 持续写到 Wait 结束。成功路径只检查 stdout 是否有非空结果，失败路径的 failureDiagnostic 最多展示 800 个 rune，且会从过长文本保留首尾片段；因此当前内存占用随进程累计输出量增长，而下游使用只需有限摘要。多个并发长输出会叠加内存峰值；实际 CLI 输出分布尚未测量。"
verification: ""
resolution: ""
---

## 现状与证据

CLI 和评论续聊过程输出的全部 stdout/stderr 都在内存保留到子进程结束；成功后仅用 stdout 判断是否有文字，失败后只生成有限长度诊断。

## 建议方向

用流式有界采集器替代完整 bytes.Buffer：保留 stdout 非空判断和诊断需要的首尾片段，stderr 保留与当前 failureDiagnostic 等价的有限首尾摘要；stdout 仍完整转发给 sessionObserver，以免破坏 Codex 会话识别。

## 验收标准

用长时间输出大量 stdout/stderr 的成功与失败进程验证采集内存上限不随总输出量增长；失败评论的首尾诊断文本与现行上限一致，成功空输出判断一致，Codex thread.started 事件仍能被完整识别，进程退出码和 followup 状态不变。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
