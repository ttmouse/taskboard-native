---
id: "OP-113"
title: "项目切换的过期 Board 响应可覆盖较新的选择"
slug: "项目切换的过期-board-响应可覆盖较新的选择"
status: "inbox"
impact: "medium"
effort: "S"
category: "reliability"
family: "F2 客户端连接与过期状态"
created: "2026-11-04"
evidence: "cmd/taskboard/view.go:934-935; cmd/taskboard/board.go:647-671; cmd/taskboard/board.go:591-640"
evidence_status: "source-confirmed-unsequenced-async-switches"
evidence_note: "项目行点击只排除 addTask 与 active，没有排除 b.switching；切换期间仍可启动下一次选择。每次 openProject 都并发启动 Board 请求，响应回调无请求代次/目标校验，直接将 b.cur 设为该请求 id 并替换 columns/tasks。若后发选择先返回而先前请求后返回，旧结果会覆盖新选择；不同项目 Board payload/查询耗时可不同。源码证实无序回写路径，实际响应反序尚未运行时复现。"
verification: ""
resolution: ""
---

## 现状与证据

项目切换通过异步 API 请求加载完整 Board；切换中旧内容只降低透明度，侧栏仍可点击。openGlobal 也有异步加载并无代次校验，因此项目切换与全部项目视图间同样可能存在竞争。

## 建议方向

为用户的最新目标选择建立单调请求代次或可取消请求；仅当前代次回调可提交项目/列/任务状态和关闭 switching。统一覆盖 openProject、openGlobal 以及启动 load 的初始切换路径，确保旧错误也不能覆盖新目标的状态提示。

## 验收标准

用可控 API 延迟使 A 切换请求晚于随后 B 返回；最终 cur、列、任务、运行状态与侧栏高亮均保持 B，旧错误不覆盖 B 的状态；交替切换到全部项目也遵循最后一次选择；快速重复点击不遗留错误 switching 状态。另用真实 UI 或请求延迟确认项目较大的 Board 读取能够触发该响应顺序。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
