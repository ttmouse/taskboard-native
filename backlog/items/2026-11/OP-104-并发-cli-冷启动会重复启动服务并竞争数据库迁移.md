---
id: "OP-104"
title: "并发 CLI 冷启动会重复启动服务并竞争数据库迁移"
slug: "并发-cli-冷启动会重复启动服务并竞争数据库迁移"
status: "inbox"
impact: "medium"
effort: "S"
category: "reliability"
family: "F8 CLI/MCP 边界"
created: "2026-11-04"
evidence: "cmd/tb/main.go:835-865,685-709; internal/service/server.go:89-120; internal/store/store.go:151-164,178-208"
evidence_status: "source-confirmed-uncoordinated-startup"
evidence_note: "ensureClient 在 health 不通后等待/重试 300ms，随后每个调用都独立 startDetached；serveDetached 也只做一次先查后启。子进程进入 service.StartOptions 后先 Store.Open 并逐条执行 schema/migrate，再 net.Listen；没有跨进程启动互斥。多个同时冷启动调用可并发打开同一数据库、竞争 check-then-ALTER migration 与监听端口，输家会启动失败；额外进程诊断被吞掉的问题另由 OP-079 跟踪。"
verification: ""
resolution: ""
---

## 现状与证据

多个 CLI/Agent 在服务尚未健康时同时调用 ensureClient，会各自完成短暂 health 轮询并各自启动 tb serve 子进程；显式 tb serve --detach 也在 health 检查后直接启动，没有原子占位。每个服务进程在争用端口前先打开相同数据库并运行非事务迁移。结果是冷启动时重复创建进程、竞争 SQLite schema/迁移和端口，输家无谓失败并增加启动资源；并发旧库迁移存在 check-then-ALTER 时序冲突可能。

## 建议方向

为目标地址/数据库启动增加跨进程协调，使首个进程负责初始化与监听，其余启动者等待该实例健康并确认实例身份；协调失效时需有可回收的锁语义，不能依靠可能永久残留的普通标记文件。诊断输家具体错误仍按 OP-079 处理。

## 验收标准

在空库首次启动与旧版数据库升级场景，同时启动多个 ensureClient/serve --detach 调用；只发生一次成功迁移和一次服务监听，其余调用等待并复用同一实例；所有调用一致返回成功或明确错误，不留下额外服务进程，启动过程无 SQLite schema 竞态。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
