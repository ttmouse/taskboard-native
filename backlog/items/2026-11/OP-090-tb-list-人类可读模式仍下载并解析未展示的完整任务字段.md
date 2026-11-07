---
id: "OP-090"
title: "tb list 人类可读模式仍下载并解析未展示的完整任务字段"
slug: "tb-list-人类可读模式仍下载并解析未展示的完整任务字段"
status: "inbox"
impact: "low"
effort: "S"
category: "performance"
family: "F8 CLI/MCP 边界"
created: "2026-11-04"
evidence: "cmd/tb/main.go:156-165,247-271; internal/service/server.go:598-616"
evidence_status: "code-inspection"
evidence_note: "非 JSON 输出最终只打印 ID、列名、标题、版本和更新者，但没有 --summary 时请求完整 Task 数组并 Unmarshal 到 model.Task。服务端 summary 投影已包含所有打印字段，可减少响应 JSON 字节和客户端反序列化分配。服务端仍会先全量读任务并补全关系，属于 OP-017 的独立网络/客户端成本；未测数据量。"
verification: ""
resolution: "- 2026-11-04: 补充实现细节（2026-11-04）：cmd/tb.client.tasks 将响应解码到 json.RawMessage（cmd/tb/main.go:928-930,1051-1061）；在人类可读路径 cmdList 随后再 json.Unmarshal 到 []model.Task（cmd/tb/main.go:254-260）。因此全量列表期间原始 JSON 字节和完整任务对象会同时驻留。summary 默认化可缩小两者；进一步可让人类可读路径直接解码到仅含展示字段的摘要切片，避免保留整份 raw JSON 与多余字段。此客户端驻留开销与 OP-017 的服务端全量扫描/关系补全属于同一列表读取链路。"
---

## 现状与证据

tb list 在人类可读模式下只打印任务 ID、列名、标题、版本和更新者，却默认请求没有 summary=true 的完整任务结果，并把响应反序列化为完整的 model.Task 切片。较长正文、关系摘要等字段不会展示也不会使用。服务端已有摘要投影，保留上述显示字段。此优化只减少 HTTP 返回字节与 CLI 解码/分配；数据库全量扫描、过滤与关系补全成本仍由 OP-017 覆盖。

## 建议方向

当输出不是 JSON 且用户未要求完整字段时，默认请求 summary=true；JSON 输出继续尊重显式 --summary 选择，避免改变脚本依赖的 JSON 内容。

## 验收标准

同一项目/过滤条件下，人类可读输出的行内容和顺序与当前一致，响应只含显示所需摘要字段且客户端不再反序列化正文/关系等完整数据；默认 JSON 输出仍保留原完整任务字段，显式 --summary 行为不变。记录大描述、多任务时响应字节与 CLI 分配量对比。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。

补充实现细节（2026-11-04）：cmd/tb.client.tasks 将响应解码到 json.RawMessage（cmd/tb/main.go:928-930,1051-1061）；在人类可读路径 cmdList 随后再 json.Unmarshal 到 []model.Task（cmd/tb/main.go:254-260）。因此全量列表期间原始 JSON 字节和完整任务对象会同时驻留。summary 默认化可缩小两者；进一步可让人类可读路径直接解码到仅含展示字段的摘要切片，避免保留整份 raw JSON 与多余字段。此客户端驻留开销与 OP-017 的服务端全量扫描/关系补全属于同一列表读取链路。
