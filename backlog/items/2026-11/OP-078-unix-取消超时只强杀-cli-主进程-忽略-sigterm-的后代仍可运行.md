---
id: "OP-078"
title: "Unix 取消超时只强杀 CLI 主进程，忽略 SIGTERM 的后代仍可运行"
slug: "unix-取消超时只强杀-cli-主进程-忽略-sigterm-的后代仍可运行"
status: "inbox"
impact: "high"
effort: "M"
category: "reliability"
family: "F4 派发校验与进程生命周期"
created: "2026-11-04"
evidence: "internal/executor/exec_unix.go:15-48; Go 1.27.1 GOROOT/src/os/exec/exec.go:837-848"
evidence_status: "source-confirmed-process-tree-escalation-gap"
evidence_note: "exec_unix sends SIGTERM to the process group, but after WaitDelay expires Go 1.27.1 watchCtx escalates with c.Process.Kill(), which targets only the command leader. A descendant that ignores SIGTERM can outlive cmd.Wait and continue modifying the workspace after the dispatcher removes the inflight lock."
verification: ""
resolution: "- 2026-11-04: Evidence clarification: Go 1.27.1 watchCtx calls Process.Kill only if the direct child is still alive when WaitDelay expires; if the leader exited earlier but a descendant retains inherited pipes, Wait closes those pipes after the delay without killing that descendant. Both cases allow a SIGTERM-ignoring descendant to survive group cancellation."
---

## 现状与证据

The Unix executor creates a process group and sends SIGTERM to the whole group when its context is canceled. However, if the command leader exits while a descendant ignores SIGTERM, WaitDelay's standard-library escalation kills only cmd.Process (the leader), then closes inherited pipes so Wait can return. Dispatcher cleanup can then release the task's in-memory inflight entry while the surviving descendant continues filesystem or external side effects. This contradicts the exec_unix.go comment that cancellation terminates the whole tree. Evidence was checked against the current local Go 1.27.1 toolchain.

## 建议方向

Own the grace-period escalation at the process-group level: after SIGTERM, ensure the original process group receives SIGKILL if descendants remain, with lifecycle synchronization that avoids signaling a reused process-group ID. Keep bounded Wait behavior and platform-specific semantics explicit.

## 验收标准

Launch a test command that spawns a descendant which ignores SIGTERM and records its PID, then cancel the task. After the grace period, cmd.Wait and dispatcher cleanup may complete only when the descendant is confirmed terminated; the descendant must not be able to perform a delayed workspace write, and ordinary commands that exit on SIGTERM must still stop promptly.

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。

Evidence clarification: Go 1.27.1 watchCtx calls Process.Kill only if the direct child is still alive when WaitDelay expires; if the leader exited earlier but a descendant retains inherited pipes, Wait closes those pipes after the delay without killing that descendant. Both cases allow a SIGTERM-ignoring descendant to survive group cancellation.
