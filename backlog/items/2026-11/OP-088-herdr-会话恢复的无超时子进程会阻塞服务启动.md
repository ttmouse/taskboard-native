---
id: "OP-088"
title: "Herdr 会话恢复的无超时子进程会阻塞服务启动"
slug: "herdr-会话恢复的无超时子进程会阻塞服务启动"
status: "inbox"
impact: "medium"
effort: "M"
category: "reliability"
created: "2026-11-04"
evidence: "internal/service/server.go:125-153; internal/executor/dsh.go:47-99,101-147; internal/executor/herdr.go:304-317,332"
evidence_status: "code-inspection"
evidence_note: "service.StartOptions 在启动 HTTP Serve goroutine 前同步调用 NewDispatcher；恢复 returned Herdr 执行时串行调用 herdrGet(context.Background())，最终执行 hc get 的 Command.Output，无超时或取消。若 hc/Herdr 响应挂起，Start 不返回，GUI runWindow 在 service.Start 返回前也无法创建窗口。仅在存在匹配的 returned Herdr 记录且恢复查询卡住时触发；未测当前冷启动时延。"
verification: ""
resolution: "- 2026-11-04: 补充证据澄清（2026-11-04）：GUI 阻塞点来自 cmd/taskboard/main.go:86-114 的 runWindow 在 service.Start 返回前同步执行；service.Start 的 HTTP Serve goroutine 在 internal/service/server.go:148-152 才启动。新增条目正文中的对应一句意为“cmd/taskboard.runWindow 也同步等待 service.Start 后才进入创建窗口流程”。shell 对反引号命令替换的误处理导致函数名未保留在正文；以上记录是可核对的准确引用。\n- 2026-11-04: 补充同一无时限调用的跟踪路径（2026-11-04）：herdrGet(ctx, address) 也由 trackHerdr 每轮调用（internal/executor/herdr.go:358-369）；传入的是该执行生命周期的 ctx，没有单次探测 deadline。若某次 hc get 挂起，轮询 goroutine 无法进入后续失败计数、断连状态写回或两分钟不可达收敛，直到整个执行 context 被取消。这个后果与启动恢复阻塞不同，但根因相同：herdrGet 的外部子进程调用没有有界时限。建议把验收扩展到恢复查询和运行中轮询，单次探测超时应允许后续轮次继续并保持状态语义。"
---



## 现状与证据

StartOptions 打开 Store 后先同步构造 Dispatcher，HTTP listener 虽已绑定，但 Serve goroutine 直到 NewDispatcher 返回后才启动。Dispatcher 的 recoverReturnedHerdrExecutions 对 returned Herdr 会话逐条调用 herdrGet(context.Background())；herdrGet 调用 hc get 并等待 Command.Output，调用链没有 deadline。 也同步等待 service.Start 后才进入创建窗口流程。因此恢复命令或本地 Herdr 服务无响应时，服务不可用且 GUI 窗口启动被无限期拖延。该问题区别于 OP-058 执行记录 SQL 全表扫描与 OP-061 旧回执导入扫描：瓶颈是启动关键路径上的外部子进程无期限等待。

## 建议方向

为启动恢复的外部命令设置明确 deadline 和取消策略；恢复单条会话超时应保留可重试/可诊断状态并继续处理其余记录。若将恢复移出 HTTP listener 启动关键路径，需提供明确 readiness 状态并确保恢复期间写请求不会启动重复执行会话。

## 验收标准

模拟 hc get 长时间不返回，service.Start 与 GUI 可在有界时间内进入可用/明确恢复中状态；对应执行记录不被误标终态或重新派发，超时会被记录且其余会话恢复继续；恢复完成后状态与原有语义一致。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。

补充证据澄清（2026-11-04）：GUI 阻塞点来自 cmd/taskboard/main.go:86-114 的 runWindow 在 service.Start 返回前同步执行；service.Start 的 HTTP Serve goroutine 在 internal/service/server.go:148-152 才启动。新增条目正文中的对应一句意为“cmd/taskboard.runWindow 也同步等待 service.Start 后才进入创建窗口流程”。shell 对反引号命令替换的误处理导致函数名未保留在正文；以上记录是可核对的准确引用。

补充同一无时限调用的跟踪路径（2026-11-04）：herdrGet(ctx, address) 也由 trackHerdr 每轮调用（internal/executor/herdr.go:358-369）；传入的是该执行生命周期的 ctx，没有单次探测 deadline。若某次 hc get 挂起，轮询 goroutine 无法进入后续失败计数、断连状态写回或两分钟不可达收敛，直到整个执行 context 被取消。这个后果与启动恢复阻塞不同，但根因相同：herdrGet 的外部子进程调用没有有界时限。建议把验收扩展到恢复查询和运行中轮询，单次探测超时应允许后续轮次继续并保持状态语义。
