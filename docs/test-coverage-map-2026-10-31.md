# taskboard-native 测试覆盖地图

日期：2026-10-31（Asia/Shanghai）  
范围：HEAD `441b4b6` + 当时工作区（含研发二表格 WIP 测试文件）  
方法：`go test` / `-cover`；只梳理不改产品代码

## 1. 现有测试一览（约 113 个 `*_test.go`）

### `cmd/taskboard/`（约 63）
- UI / 渲染 / 动效 / compose / 详情 / handoff 头 / 表格探测
- WIP：`body_table_test.go`（spans / splice / 点表编辑）

### `internal/store`
- relations（parent / cycle / blocked_by gate）
- completion、project、execution 等

### `internal/executor`
- `dispatch_policy`（约 3 场景）、`handoff_log_read`
- herdr / dsh / desktop / continuation / runtimes

### `internal/service`
- server（含 relations API、评论回复树）
- handoff_log endpoint、desktop_flow

### 其它
- `internal/client`、`cmd/tb`、`cmd/taskboard-mcp`：HTTP / CLI / MCP 薄层

## 2. 运行结果摘要（当次）

| 包范围 | 结果 | cover（约） |
|--------|------|-------------|
| `./internal/...` + `cmd/tb` + `cmd/taskboard-mcp` | 全过 | client 52% / executor 67.8% / model 62.8% / service 81.4% / store 68% / tb 19.1% / mcp 33.9% |
| `./cmd/taskboard/` | 全过（~39s） | 包级 ≈30%（UI 交互测为主） |

未为修测改业务代码。走查时 UI 测试曾改写 `docs/assets/render/*.png`，已还原未入仓。

## 3. 缺口

| 模块 | 现状 | 缺口 |
|------|------|------|
| 关系 | store/API 有 parent / blocks / blocked_by | **`related` 几乎无测、UI 无入口**；候选排除 / GUI 加减关系缺专门测 |
| 表格 | WIP 有 spans / splice / 打开编辑 | 缺 blur 提交、多表切换偏移、`liveBody` 脏检查与 save 一致性 |
| auto-handoff | 读路径有测 | 写事件 / 汇总边界、大日志性能无压测 |
| classify | 仅约 3 个 BuildDispatch 场景 | **误伤用例不足**（「新增需求」「接口说明」等） |
| git 验收提交 | — | **`gitCommitTracked` / `maybeGitCommitOnDone` 零测试** |
| 评论引用树 | playground / render / server 有 | 深层嵌套、删父后 UI 行为覆盖薄 |

## 4. 优先补测（按风险）

1. **`gitCommitTracked`**：临时 git 仓测 clean / 仅 untracked / `add -u` 与 message（防误提交最高）。
2. **`classifyTask` 误伤表**：标题含「新增需求」「接口说明」等应保持 single-agent。
3. **验收完成 → `maybeGitCommitOnDone`**：prefs 开/关、无 workspace、失败 toast 路径。
4. **关系 `related` + UI 候选**：跨项目 / 自环 / 已关联排除；GUI 加删 parent/blocks。
5. **表格 WIP**：切换两表时偏移；`commitTableEdit` + dirty / `saveDetail` 写回一致性。
6. **handoff 大文件**：超 limit 截尾、坏行跳过、打开详情不超时。

## 5. 相关对话产出

同期代码走查要点（未改代码）：`view.go`/`board.go` 过大；已跟踪二进制与 `.gitignore` 过窄；client 绑 executor 类型；handoff 全扫 JSONL；classify 子串误伤；`gitCommitTracked` 波及面；表格 PlainText 丢 inline；关系候选全扫；WIP 表格重复 Parse / 失焦提交 / 偏移风险。
