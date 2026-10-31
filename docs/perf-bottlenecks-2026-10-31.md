# taskboard-native 性能瓶颈（2026-10-31）

实测环境：本机 darwin-arm64，App 约 0.1.82–0.1.83 构建；方法含 `open -a Taskboard` + AX 窗口计数、headless `Frame` 帧耗时、详情打开计时与代码路径阅读。

## 最慢三项

### 1. 冷启动到窗口可交互 — ~2.4–2.7s（P1）

| 指标 | 量级 |
|------|------|
| `open -a Taskboard` 至窗口可交互 | ~2.4–2.7s |
| 进程起来 | ~0.2s |

**原因**：进程内 `service.Start` + SQLite + 首屏 HTTP/SSE + Native 窗串行偏重。

**建议**：延迟/并行起服务；缓存首屏 snapshot；减少首帧工作。

### 2. 多卡列表帧耗时（滚动同路径）— 200 卡 ~24ms/帧（P0）

| 规模 | 约帧耗时 |
|------|----------|
| 5 卡 | ~1.5ms/帧 |
| 50 卡 | ~5.4ms/帧 |
| 200 卡 | ~24ms/帧 |

**原因**：列内无虚拟化；每帧 `cardView` / `taskCard` 全量重建。

**建议（P0）**：视口虚拟列表；降卡复杂度（摘要、动画、ContextMenu 惰性创建）。

### 3. 大表详情打开 — 200×10 表 ~176ms（P1）

| 指标 | 量级 |
|------|------|
| 打开含 200×10 Markdown 表的详情 | ~176ms |
| 稳态帧（有大表） | ~6ms/帧 |
| 对照无表看板稳态 | ~1.9ms/帧 |
| `parser.Parse` / `bodyTableSpans` | ~0.5ms（非瓶颈） |

**原因**：慢在 `fitMarkdownTable` 每帧 O(行×列) 建控件；可能重复 Parse。

**建议（P1）**：表块缓存/离屏；大表折叠或分页；避免 `bodyTableSpans` + 段内再 Parse。

## 其它观察

- SSE `applyEvent` 本身轻，但触发整板重绘，卡多时被列表问题放大。
- 表解析不是瓶颈；控件构建才是。

## 证据来源

- 研发二 2026-10-31 本机实测（cold open / Frame / 详情计时）+ `view.go` / `fitMarkdownTable` / `detailMarkdown` 路径阅读。
- 只分析落盘；本文件不代替代码改动。

## 已落地（研发三 · 列表滚动 P0 · 2026-11-01）

- **视口虚拟列表（列内）**：`columnCards`（`column_virtual.go`）按 `TrackScroll.Y` 与视口高（优先 `content−MaxY`，其次 `panel.H−header`，缓存于 `colScroll.lastViewH`）只对视口±`columnCardOverscan`(240DIP) 调用 `cardView`/`taskCard`；视外用同 key 的等高占位。`cardHeights` 持久缓存实测行高，避免估高漂移。首帧视口未知时仍全量构建。
- **未改用 MyGo `ui.List`**：列内拖拽落位幽灵、源卡保活、FocusGroup 与现有 `edgeInsertIndex` 强耦合；手写视口裁剪成本更低且行为可预期。
- **卡复杂度**：静止卡跳过 `AnimateColor`（仅 hover/press/recent flash 补间）。ContextMenu 的 builder 在 MyGo 里本就仅在打开/选中时运行，未再做按需挂载（挂载惰性会丢掉 Chosen 帧）。
- **测试**：`TestColumnVirtualizesOffscreenCards`；`column_spacing_test` 跳过尚未物化的卡（`bounds.W==0`）。
