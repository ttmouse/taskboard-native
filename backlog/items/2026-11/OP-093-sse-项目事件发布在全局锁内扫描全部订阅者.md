---
id: "OP-093"
title: "SSE 项目事件发布在全局锁内扫描全部订阅者"
slug: "sse-项目事件发布在全局锁内扫描全部订阅者"
status: "inbox"
impact: "medium"
effort: "S"
category: "performance"
family: "F1 服务端 SSE 快照与背压"
created: "2026-11-04"
evidence: "internal/service/sse.go:28-52,54-70; internal/service/server.go:136-139"
evidence_status: "code-inspection"
evidence_note: "每次 store 事件都同步调用 hub.publish；publish 持有同一把全局 mutex 遍历全部连接，仅通过 projectID 判断是否跳过。项目范围事件的处理时间随全局 SSE 连接数增长，且遍历期间 subscribe/unsubscribe 也需等待该锁。"
verification: ""
resolution: ""
---

## 现状与证据

SSE hub 将所有连接保存在同一 map。publish 对每个事件逐个遍历全部订阅者，并在持锁时做项目范围过滤和向缓冲 channel 发送。高订阅并发下，无关项目的连接也增加每次发布的扫描成本，并延长订阅注册、退订和其他项目事件发布的锁占用。

## 建议方向

按项目维护订阅索引，或采用能在锁外完成分发的快照机制；需保持订阅/退订与事件投递的并发安全、慢客户端丢弃行为和关闭语义。

## 验收标准

为指定项目发布事件时只访问该项目及全局订阅者，或证明锁内扫描不再随其他项目订阅者数量增长；并发订阅、退订、关闭和慢客户端测试无竞态、无泄漏，测量多项目及高连接数下的 publish 延迟与锁等待。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
