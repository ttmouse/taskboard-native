---
id: "OP-110"
title: "客户端保留无人调用且无写入方的 server.json 发现协议"
slug: "客户端保留无人调用且无写入方的-server-json-发现协议"
status: "inbox"
impact: "low"
effort: "S"
category: "developer-experience"
family: "F11 仓库与 DX"
created: "2026-11-04"
evidence: "internal/client/client.go:34-37,73-88,166-173; 全仓非测试代码 rg LoadDiscover/Dial/server.json 无生产调用或写入；cmd/taskboard/main.go:112 直接构造 Discover 并调用 New"
evidence_status: "observed"
evidence_note: "当前 GUI 使用已绑定的进程内服务地址直接创建客户端，CLI/MCP 也各自使用当前服务地址；LoadDiscover/Dial 没有生产调用。仓库中没有 server.json 写入实现，但注释称其由服务写入。该协议和 TASKBOARD_ADDR 覆盖顺序只存在于无人调用的代码。"
verification: ""
resolution: ""
---

## 现状与证据

internal/client 同时提供构造式 New 与自动发现式 Dial。当前入口都采用 New 或独立 HTTP 客户端；自动发现代码假设服务会维护 server.json，而当前服务源码没有此写入方。

## 建议方向

确认不再支持旧自动发现后，删除未使用的 ConfigDir/LoadDiscover/Dial/ErrNotRunning 和相关陈旧注释/测试，保留当前入口所需的明确地址构造；若仍需自动发现，则改为实现一套有实际调用者和权威写入方的协议，避免维护两种互相脱节的连接方式。

## 验收标准

静态引用检查确认删除的发现 API 无调用者；GUI、tb、MCP 仍能按各自明确配置连接服务；文档不再声称不存在的 server.json 写入行为；`TASKBOARD_ADDR` 的生效规则在各入口说明一致。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
