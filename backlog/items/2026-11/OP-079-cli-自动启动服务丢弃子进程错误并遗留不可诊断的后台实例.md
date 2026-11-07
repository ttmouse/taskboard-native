---
id: "OP-079"
title: "CLI 自动启动服务丢弃子进程错误并遗留不可诊断的后台实例"
slug: "cli-自动启动服务丢弃子进程错误并遗留不可诊断的后台实例"
status: "inbox"
impact: "medium"
effort: "S"
category: "developer-experience"
family: "F8 CLI/MCP 边界"
created: "2026-11-04"
evidence: "cmd/tb/main.go:704-735,833-865,868-881; cmd/tb/main_test.go:274-308"
evidence_status: "source-confirmed-startup-diagnostics-gap"
evidence_note: "serveDetached sets child stdout and stderr to nil (os/exec sends nil output to the null device), starts and releases the process handle before waitHealthy, then returns only a generic health failure. ensureClient's startDetached similarly releases the child; callers cannot read the actual service.Start/migration error or observe child exit. The existing startup test covers success only."
verification: ""
resolution: ""
---

## 现状与证据

When tb auto-starts the service and the child fails during database open/migration or listener startup, service.Start reports the concrete cause in the child's stderr. Detached startup discards stderr/stdout and releases the process handle before health polling, so the parent cannot surface that cause or check whether the child exited. The command eventually reports only a generic health error, making invalid database paths, migration failures and address conflicts hard to distinguish; depending on where failure occurs, startup may also leave an unobserved process until it exits.

## 建议方向

Add a bounded startup handshake that captures the child's startup result and diagnostics before detaching ownership; report the actual failure and ensure failed/timed-out startups do not leave an untracked service process. Keep normal detached service logs out of CLI stdout after successful readiness.

## 验收标准

Exercise detached startup with a valid DB, invalid/unopenable DB, occupied address and a delayed readiness case. Success must report the resolved address; failures must expose an actionable cause, terminate or account for the child, and avoid printing success before readiness. Subsequent ensureClient calls must not mistake a failed startup for a healthy service.

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
