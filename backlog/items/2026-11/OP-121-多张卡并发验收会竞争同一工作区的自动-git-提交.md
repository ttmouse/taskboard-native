---
id: "OP-121"
title: "多张卡并发验收会竞争同一工作区的自动 Git 提交"
slug: "多张卡并发验收会竞争同一工作区的自动-git-提交"
status: "inbox"
impact: "medium"
effort: "M"
category: "reliability"
created: "2026-11-04"
evidence: "cmd/taskboard/board.go:1565-1580; cmd/taskboard/git_commit.go:13-56,61-96"
evidence_status: "source-confirmed-unserialized-worktree-commit"
evidence_note: "每次 MoveTask 成功且目标列 role=done 都调用 maybeGitCommitOnDone；该函数每次启动独立 goroutine，直接运行 gitCommitTracked，同一工作区没有队列或互斥。两张卡并发验收时，两条链会交错执行 status、add -u、commit 和 HEAD 查询：Git index.lock 可使其中一次 add/commit 失败；若第二次 add 在第一次 commit 后成功，第二次 commit 可能因没有待提交变更而返回失败。反之若期间又有新工作区修改，两个提交范围和回写 sha 也可能难以对应。OP-016 处理预暂存索引污染，OP-117 处理同一完成卡重排误触发，均未处理不同卡片同时触发。尚未做确定性并发复现。"
verification: ""
resolution: ""
---

## 现状与证据

同一工作树可能在短时间内接受多张卡片。当前每次验收都独立异步运行完整 Git 工作流，没有按仓库串行化；Git 的 index.lock 能防止索引文件同时写坏，但无法保证应用层 status/add/commit 序列作为整体互斥，因此可能出现偶发 Git 错误、重复空提交失败或 sha 与触发卡片难以对应。

## 建议方向

按规范化工作树路径建立有界串行提交队列或等效互斥，保证 status、暂存、commit、HEAD 读取为一个不可交错的操作单元；界面仍异步，不能阻塞 UI。定义队列中后续验收遇到无新变更时的成功/无操作结果与评论语义，并保留 OP-016 的预存 index 边界保护及 OP-117 的触发条件。

## 验收标准

用临时 Git 仓库同时触发多张卡进入完成列，确定性复现并验证 Git 子命令不交错、不因 index.lock 或空索引产生误报；有新 tracked 修改时提交顺序和每个 SHA/评论对应关系明确，无新修改时按定义报告无操作而不创建空提交；GUI 保持响应。重复项目路径/符号链接应映射到同一 worktree 锁；预先 staged 的无关变更仍按 OP-016 的安全语义完整保留。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
