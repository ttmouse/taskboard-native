---
id: "OP-069"
title: "tb serve --detach 未核对已有服务的数据库路径"
slug: "tb-serve-detach-未核对已有服务的数据库路径"
status: "inbox"
impact: "low"
effort: "S"
category: "reliability"
family: "F8 CLI/MCP 边界"
created: "2026-11-04"
evidence: "cmd/tb/main.go:677-709; internal/service/server.go:292-294"
evidence_status: "current-source-confirmed"
evidence_note: "serveDetached 在 health 200 后无条件打印 already listening 并返回成功；db 参数只在启动子进程时转发，已有实例的健康端点仅返回 {ok:true}，不核对数据库身份。"
verification: ""
resolution: "- 2026-11-04: 补充 GUI 启动复用路径（2026-11-04）：runWindow 调 service.Start 后只要错误满足 EADDRINUSE，就直接把默认地址交给 newBoard；启动阶段不探测 /api/health，也不校验响应是否属于 Taskboard（cmd/taskboard/main.go:99-113）。端口被非 Taskboard 服务占用时窗口仍打开，后续 API 操作才失败；若占用服务恰好返回兼容形状，还可能误写到错误服务。应将既有服务身份校验覆盖 GUI 复用入口，验证失败时中止启动并明确错误，不能将单纯端口冲突视为“Taskboard 已在运行”。验收包含正确 Taskboard、非 Taskboard HTTP 服务及不响应服务占用默认端口三种情况。"
---

## 现状与证据

调用 `tb serve --addr A --db B --detach` 时，如果 A 已有健康服务（但它可能使用数据库 C），serveDetached 不检查 db 参数是否与已有服务一致，直接输出 `already listening` 并返回成功。调用者会以为 B 对应的服务已启动，后续 CLI 操作却落到另一个数据库。现有 health handler 只返回 `{ok:true}`，没有实例或数据库身份可供核对。

## 建议方向

当指定 db 且目标地址已有服务时，提供安全的实例身份校验，确认路径/实例一致才成功；无法验证或不一致时明确失败。避免在健康响应中直接泄露数据库绝对路径，可用不可逆实例标识或专用诊断协议。

## 验收标准

在 addr 空闲时按给定 db 启动成功；已有服务使用同一数据库时按定义复用；已有服务使用不同数据库或无法确认时必须返回非零并明确说明，不能打印成功式 already listening。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。

补充 GUI 启动复用路径（2026-11-04）：runWindow 调 service.Start 后只要错误满足 EADDRINUSE，就直接把默认地址交给 newBoard；启动阶段不探测 /api/health，也不校验响应是否属于 Taskboard（cmd/taskboard/main.go:99-113）。端口被非 Taskboard 服务占用时窗口仍打开，后续 API 操作才失败；若占用服务恰好返回兼容形状，还可能误写到错误服务。应将既有服务身份校验覆盖 GUI 复用入口，验证失败时中止启动并明确错误，不能将单纯端口冲突视为“Taskboard 已在运行”。验收包含正确 Taskboard、非 Taskboard HTTP 服务及不响应服务占用默认端口三种情况。
