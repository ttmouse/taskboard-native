---
id: "OP-087"
title: "断开的 SSE 请求仍在全局锁内构建完整快照"
slug: "断开的-sse-请求仍在全局锁内构建完整快照"
status: "inbox"
impact: "medium"
effort: "M"
category: "reliability"
created: "2026-11-04"
evidence: "internal/service/sse.go:89-105,116-135; internal/store/queries.go:80-110"
evidence_status: "code-inspection"
evidence_note: "handler 在调用 Snapshot 前订阅并未检查 request context，且先完成 JSON marshal、写首帧之前没有 context select；Snapshot 以 context.Background 查询并在整个流程持有 Store 全局 mutex。客户端提前断连时，请求取消只在快照完成并进入流循环后才检查。尚未实测大型数据集的锁占用时间。"
verification: ""
resolution: "- 2026-11-04: 补充证据（2026-11-04）：同一 request context 未传入 Store 的问题也覆盖普通读取端点，不限于 SSE。GET /api/projects 调用 Projects，GET /api/projects/{id}/board 调用 Board，GET /api/tasks 调用 TasksByProject，均不传 r.Context；相关 Store 方法再以 context.Background 执行查询并持有同一 s.mu。尤其 GET /api/tasks 的 limit/summary 在完整读取和关系补全之后才应用（OP-017），客户端中断后仍可能继续做整项目工作。建议方向应把可取消请求上下文传播到所有较重的 Store 读取路径，并在连接取消时及时释放全局锁；保留 SSE 的先订阅后快照语义。"
---


## 现状与证据

handleEvents 注册订阅后同步调用 Store.Snapshot，并在其返回后才编码完整快照和写 HTTP 响应；请求上下文仅在初始帧处理完成后的循环中检查。Store.Snapshot 用 context.Background 执行项目/列/全部任务和关系查询，并在读取全部内容期间持有 s.mu。若浏览器或代理在慢快照期间断开，handler 无法及时停止这项工作，仍会占用唯一 Store 锁并延迟同服务的其他读写。

## 建议方向

为快照路径传递请求 context，在进入锁、项目循环和数据库查询之间响应取消；确保取消会释放 Store 锁并清理已登记的 SSE subscriber。先验证底层 SQLite Query 对 context 取消的支持，不支持时考虑可控分批/协作式取消，避免破坏订阅先于快照的无丢事件语义。

## 验收标准

通过延迟/大数据快照建立 SSE 请求后主动断开连接，数据库查询及时停止或在有界时间内退出，subscriber 被移除，随后独立读写不需等待整份快照完成；正常连接仍接收 seq 一致的完整首帧及之后事件，快照错误语义不变。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。

补充证据（2026-11-04）：同一 request context 未传入 Store 的问题也覆盖普通读取端点，不限于 SSE。GET /api/projects 调用 Projects，GET /api/projects/{id}/board 调用 Board，GET /api/tasks 调用 TasksByProject，均不传 r.Context；相关 Store 方法再以 context.Background 执行查询并持有同一 s.mu。尤其 GET /api/tasks 的 limit/summary 在完整读取和关系补全之后才应用（OP-017），客户端中断后仍可能继续做整项目工作。建议方向应把可取消请求上下文传播到所有较重的 Store 读取路径，并在连接取消时及时释放全局锁；保留 SSE 的先订阅后快照语义。
