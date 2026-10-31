# Herdr / 自动承接 P0 修补方案

- 日期：2026-10-31 Asia/Shanghai
- 执行：研发三（只分析落盘；**本次未改码**）
- 源文档：`docs/auto-handoff-herdr-flow-2026-10-31.md` §4.4–4.5、§5 优先级表 **P0-1 / P0-2 / P0-3**
- 核对范围：`internal/executor/{project_run,herdr,dsh}.go`，`internal/model/model.go`，`docs/skills/manage-taskboard-native/SKILL.md`，`cmd/taskboard/view.go`（徽章文案）

---

## 背景与源文档

研发一对自动承接 + Herdr 主路径做只读分析后，标出三条 **P0**：

| ID | 摘要 |
|----|------|
| P0-1 | 技能声称「派发不因会话结束移评审」，自动承接却会 `advanceFinishedCard` →「等你」 |
| P0-2 | `returned` 只表示 turn 结束，**不等于** Agent 已写结果评论/移列；空结果卡仍可能进「等你」 |
| P0-3 | `disconnected` 仍 `Active`（占写锁），UI 若未强调锁仍在，易误判可重派 |

下文逐条对照 **当前源码** 确认是否仍成立，并给出推荐修补（不实施）。

---

## P0-1：技能「不移评审」vs `advanceFinishedCard`

### 现象

- 技能要求：执行 Agent 自己回写评论并移入评审；**派发程序不根据会话结束把卡移入评审**。
- 自动承接：Herdr/会话收敛（`Ended`）后，服务代为把进行中卡推进「等你」，再拉下一张。

### 源码依据

| 位置 | 符号 / 文案 |
|------|-------------|
| `docs/skills/manage-taskboard-native/SKILL.md` L46 | 「派发程序不替你认领、不代写工作结果、**不根据会话结束或退出码把卡片移入评审**」 |
| 同文件 L61 | 「先回写结果，再移入评审」；「会话正常结束…都不能替代这两次写入」 |
| `internal/executor/project_run.go` `finishProjectCard` | 要求 `execution.Ended()` 且卡仍在进行中 → 调 `advanceFinishedCard` |
| 同文件 `advanceFinishedCard` | `MoveTask` → 评审列（「等你」）+ `task.moved_to_review` + 服务评论 |
| 同文件 `finishedCardReason` | 「会话已返回，卡片**自动移入「等你」**…」 |
| `dsh.go` / `desktop.go` | `defer d.finishProjectCard(task.ID)`（项目跑路径） |

### 是否仍成立

**仍成立。** 技能全文未区分「单卡手动派发」与「项目自动承接」；`advanceFinishedCard` 路径仍在，无门控、无技能同步改写。

### 根因

产品上自动承接把「清空进行中队列」做成服务职责，技能仍按「手动派发语义」写绝对句。Agent/人类会误判「谁负责移列」。

### 推荐修补

1. **文案对齐（优先、低风险）**  
   - 技能增加一小节：自动承接开启时，会话 `Ended` 后服务**可能**把卡移入「等你」以便队列继续；**结果评论与验收证据仍必须由执行 Agent 写入**；「完成」列仍只由人类确认。  
   - 产品/帮助文案同步区分「单卡派发」vs「项目自动承接」。
2. **可选行为开关（中期）**  
   - `ProjectRun` 或设置项：`advanceOnSessionEnd`（默认 true 保持现状）；false 时只记 `task.execution_converged`、留进行中并评论催收（与 P0-2 联动）。
3. **测试点**  
   - 技能/契约测试：文档含「自动承接会移入等你」字样（防回退）。  
   - 已有 `project_run_restart_test` 覆盖 `finishProjectCard` →「等你」；保留并注明与技能例外条款一致。

### 风险

只改技能不改代码：行为不变，误操作面仍在，但至少消除「技能撒谎」。加开关则要兼容旧库默认开启。

### 兼容性

纯文档补丁向前兼容。开关默认 true → 无行为突变。

---

## P0-2：`returned` ≠ 已回写

### 现象

Herdr `done` / idle 稳定 / 正常退出码 → Execution=`returned`，Detail 写明「任务状态由执行 Agent 回写」。自动承接仍可能把**尚无结果评论**的卡推进「等你」。

### 源码依据

| 位置 | 行为 |
|------|------|
| `herdr.go` `trackHerdr` | `done` / idle≥`herdrIdleGrace`(6s) / ExitCode==0 → `Status="returned"`，**不读任务评论** |
| `model.Execution.Ended` | `returned` ∈ `endedStatuses` → 写锁释放 |
| `finishProjectCard` → `advanceFinishedCard` | 仅看列角色 + `Ended()`，**无**「最近 Agent 结果评论」检查 |
| 技能 L61 | 明确：会话结束不能替代评论+移列两次写入 |

### 是否仍成立

**仍成立。** 收敛与业务回写仍解耦；空结果进「等你」无防护。

### 根因

Execution 状态机只跟踪 Herdr terminal lifecycle；交付证据在 Task 评论侧，两边无汇合门闩。

### 推荐修补

1. **收敛前门闩（推荐）**  
   - 在 `advanceFinishedCard`（或 `finishProjectCard` 内）增加：若不存在「执行渠道作者」的结果向评论（启发式：作者 ∈ 已知 runtime Label/ID，且非「自动承接」服务号；可选关键词/最小长度），则：  
     - **不移列**（或仅记 handoff `task.result_missing`）；  
     - 服务评论催收：`@原渠道` 请补结果评论；  
     - 可选：自动 `Continue` 一次催收（需防抖，避免刷屏）。  
   - 超时/人工「强制移入等你」入口保留，避免永久堵队列。
2. **兼容性**  
   - 旧卡无评论：默认「催收+留进行中」可能卡住自动承接 → 建议：催收 N 次或宽限 T 后仍允许 `advanceFinishedCard`，并在评论标明「无结果评论，仅会话收敛」。  
   - 或 ProjectRun 字段 `requireResultComment` 默认 false（先观测），再默认 true。
3. **测试点**  
   - 单测：`returned` + 无 Agent 评论 → 不移列 + 有催收评论事件。  
   - `returned` + 有 Codex/Claude 评论 → 移「等你」（与现行为一致）。  
   - 强制推进 API/旁路路径仍可测。

### 风险

启发式误判「过程评论」为结果评论 → 空交付漏网；过严 → 队列停滞。需明确「结果评论」判定或让 Agent 写结构化标记（如评论前缀 `结果：` / handoff 事件）。

---

## P0-3：`disconnected` 占写锁

### 现象

探测连续失败 ≥`herdrFlapProbes`(3) → `disconnected`；满 `herdrUnreachableGrace`(2min) 才 `released`。其间 `Active()==true`，Dispatch 返回 `ErrInProgress`。UI 徽章「连接异常」vs「已释放」有区分，但未明示「锁仍在 / 不可重派」。

### 源码依据

| 位置 | 行为 |
|------|------|
| `herdr.go` `trackHerdr` | misses≥3 → `disconnected`；≥2min → `released`（Detail 已写「释放写锁」） |
| `model.endedStatuses` | **不含** `disconnected`；含 `released` |
| `Execution.Active` | 非 ended → true（disconnected 占锁） |
| `model/execution_test.go` | 显式：`disconnected` Active=true；`released` Active=false |
| `dsh.go` `Dispatch` | Active && !Stalled → `ErrInProgress`（Herdr `disconnected` 非 Stalled） |
| `view.go` `herdrExecutionLabel` | disconnected→「连接异常」；released→「已释放」 |
| 徽章 Tooltip | 仅 `"Herdr 执行状态：" + label`，**无写锁说明** |

附注：`finishedCardReason` 有 `case "disconnected"` 文案，但 `finishProjectCard` 要求 `Ended()`，**disconnected 不会走该分支**（死文案/历史残留）；disconnected 时更可能 `execution_not_converged` / 承接失败路径。

### 是否仍成立

**仍成立（文案有部分软化）。**  
- 写锁语义未变。  
- UI 已有「连接异常」/「已释放」分词，且 `execution_badge_test` 覆盖两态可见性。  
- **缺口仍在**：Tooltip/详情未写「连接异常期间不可重新派发；约 2 分钟不可达后释放写锁」。

### 根因

临时故障态必须占锁防双写；产品侧把「连接异常」当成终态文案，未绑定锁生命周期说明。

### 推荐修补

1. **UI 文案（优先）**  
   - `disconnected` Tooltip/详情：`连接异常（写锁仍占用，暂不可重派；持续不可达约 2 分钟后释放）`。  
   - `released`：`已释放写锁，可重新派发或评论跟进`。  
   - 可选：详情页「打开 Herdr / 标记释放」按钮（与分析文档 P1-3 联动）。
2. **Dispatch 错误回传**  
   - `ErrInProgress` 在 status==disconnected 时附中文原因，GUI toast 复用。
3. **测试点**  
   - 扩展 `execution_badge_test`：断言 Tooltip/无障碍 Label 含「写锁」或「不可重派」。  
   - 模型单测已覆盖 Active；保持。  
   - 可选：Dispatch 对 disconnected 返回可识别错误类型的测试。

### 风险

文案过长挤胶囊；宜 Tooltip/详情长文、胶囊保持短标签。缩短 `herdrUnreachableGrace` 会更快释放但增加「误释放后双会话」风险——本次不建议改默认时长。

### 兼容性

仅 UI/错误串：无协议破坏。

---

## 建议实施顺序

1. **P0-1 技能/产品文案对齐**（最快消除认知冲突）  
2. **P0-3 UI/Toast 写锁说明**（低风险、可单测）  
3. **P0-2 结果评论门闩**（需启发式或结构化标记；建议先 flag 默认关，再收紧）  
4. （顺带）删掉或改写 `finishedCardReason` 中不可达的 `disconnected` 分支，避免误导后续维护者  

与源文档 P1（不可达 CLI 分支、space 歧义、unknown 一键释放、结构化 ReasonCode）不阻塞上述 P0。

---

## 明确「本次未改码」

本文件为核对与方案落盘。未修改 `internal/`、`cmd/`、技能正文或测试；未 commit/push；未派 Herdr/Codex。

---

## 落地状态（2026-11-01 Asia/Shanghai，研发三）

- **P0-1**：已改 `docs/skills/manage-taskboard-native/SKILL.md`（单卡手动派发 vs 项目自动承接）。
- **P0-3**：已改 `cmd/taskboard/view.go` 徽章 Tooltip/无障碍长文（写锁仍占用）；短标签仍为「连接异常」。
- **P0-2**：**只补本备注**——`returned`≠已回写；空结果进「等你」的门闩为后续项；**本次不改** `advanceOnSessionEnd` / `advanceFinishedCard` / `finishProjectCard`，不加开关。
