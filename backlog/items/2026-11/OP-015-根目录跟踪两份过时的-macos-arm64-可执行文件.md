---
id: "OP-015"
title: "根目录跟踪两份过时的 macOS arm64 可执行文件"
slug: "根目录跟踪两份过时的-macos-arm64-可执行文件"
status: "inbox"
impact: "medium"
effort: "S"
category: "developer-experience"
created: "2026-11-04"
evidence: "git ls-files -s taskboard tb; README.md:8-17; docs/agent-read/usage.md:7,38-40; .gitignore:1"
evidence_status: "repository-state-confirmed"
evidence_note: "HEAD 跟踪根目录 taskboard 和 tb 两个 Mach-O arm64 可执行文件，Git blob 分别为 19,870,706 与 13,330,226 字节。README 推荐 go run，Agent 文档使用 build/bin 并说明无需全局命令；build/ 已忽略，根目录两个二进制未忽略且没有被文档用作入口。"
resolution: ""
---

## 现状与证据

本机架构、编译环境生成的可执行文件与源代码一同跟踪，合计约 33.2 MB；在源码更新时，仓库根目录二进制可能保持旧版本，令读者误把它当作当前实现。

## 建议方向

停止跟踪根目录的本机构建产物，将可再生成的 CLI/GUI 输出放在 build/ 并保持忽略；README 和开发流程只引用源码构建或 build/bin 产物。若需要发布物，用版本化发布渠道保留。

## 验收标准

从新克隆仓库不再下载根目录本地可执行文件；文档中的 go run/构建流程可完整构建所需入口；正式发布产物仍可按版本取得。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
