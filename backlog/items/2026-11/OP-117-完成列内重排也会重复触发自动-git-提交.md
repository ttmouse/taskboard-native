---
id: "OP-117"
title: "完成列内重排也会重复触发自动 Git 提交"
slug: "完成列内重排也会重复触发自动-git-提交"
status: "inbox"
impact: "medium"
effort: "S"
category: "reliability"
created: "2026-11-04"
evidence: "cmd/taskboard/board.go:1542-1580; cmd/taskboard/motion.go:254-268; cmd/taskboard/git_commit.go:11-18"
evidence_status: "source-confirmed-same-column-trigger"
evidence_note: "moveTaskWithSuccess 只在同列且 atTop=false、afterID 为空时跳过；同列拖拽重排会带 atTop=true 或 afterID。无论原列是什么，PATCH 成功后只检查目标列是否 role=done 并调用 maybeGitCommitOnDone(cur)，没有检查 cur.ColumnID 原本是否已是 done。dropOn 将预览槽位置参数原样传入 moveTask。故已完成卡片重排也会启动 git add -u/commit；工作区若有新跟踪修改会产生额外自动提交。未做 UI/运行时复现。与 OP-016 的 index 边界问题独立：即使 index 隔离正确，同列重排仍不应再次触发验收提交。"
verification: ""
resolution: ""
---

## 现状与证据

项目设置“验收完成时自动提交 Git”后，移动任意任务成功进入完成列会自动提交 tracked changes。当前逻辑把“目标位于完成列”当作“任务刚刚完成”，而拖动完成列内卡片改变顺序也是一条成功移动。

## 建议方向

只有任务从非完成角色列转换到完成角色列时触发自动提交；同列重排和已完成卡片再次移动到完成列不应提交。判定应基于 source 与 destination 的 column role，兼容全局视图映射到各项目真实列 ID 的场景。

## 验收标准

启用自动提交并准备 tracked dirty file：将待办/进行中/评审任务首次移入完成列时按策略最多提交一次；随后拖动该完成卡到顶部、其他完成卡之后、或从详情重复触发位置变更时，不创建新提交也不写第二条自动提交评论；从完成列移出再重新移入时按明确策略触发一次；全局视图跨项目列映射仍判断正确。用临时 Git 仓验证提交数、内容与评论次数。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
