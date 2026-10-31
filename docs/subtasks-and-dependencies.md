# 子任务与卡片依赖（taskboard-native）

本文说明当前实现（截至本文件写入时的代码），覆盖数据模型、存储、UI、阻塞门禁，以及与自动承接的关系。

## 数据模型

关系存在 SQLite 表 `task_relations`：

| 列 | 含义 |
|---|---|
| `relation_type` | `parent` / `blocks` / `related` |
| `source_task_id` | 关系起点 |
| `target_task_id` | 关系终点 |
| `created_at` | 创建时间 |

对外 API 还接受 `blocked_by`：写入时规范化为 **`blocks`**，方向为「阻塞方 → 被阻塞方」。

任务 JSON 上的聚合字段 `relations`（只读展开）：

- `parent`：本卡的父任务（至多一个）
- `subIssues`：以本卡为父的子任务列表
- `blockedBy`：仍阻塞本卡的任务（来自 `blocks`，且阻塞方未在完成列）
- （实现里还有 `blocks` / `related` 的查询路径；UI 当前主要暴露父子与「阻塞于」）

主键仍是任务 UUID；界面展示用四位数字 `number`。

## 父子任务怎么存

- **设父**：`parent` 关系，`source=父`，`target=子`。一张卡最多一个父；再设父会替换旧父。
- **设子**：对目标卡写 `parent`，使当前卡成为其父（UI「设为子任务」）。
- **新建子任务**：在当前卡下 `create` 新卡，并立刻挂上 `parent`。
- **防环**：写入前沿父链向上走，若会把子孙挂回祖先则拒绝。

看板卡角标「子 N」来自 `relations.subIssues` 数量。

## 依赖（阻塞）怎么存

- UI「添加阻塞」→ 当前卡 `blocked_by` 所选卡 → 存成 `blocks(所选, 当前)`，即所选卡阻塞当前卡。
- 「阻塞于 #N …」列表读的是 `relations.blockedBy`。
- 移除依赖 = 删对应 `blocks` 边。

标题在选择器里截断到 36 字；选项值是展示字符串。选择器默认是「（未选择）」，**不会**在无关联时预选第一张卡（避免误以为已建立依赖）。

## UI 入口（详情）

详情 **关系**（点小标题折叠/展开，无通栏大按钮）：

1. **副任务**：已有子卡列表（可点开 / × 移除）；右侧 **+** 菜单：新建… 或选已有卡挂为子任务。
2. **阻塞与**：当前卡阻塞的卡（UI 文案）；**+** 同上（`blocks`）。
3. **阻塞于**：阻塞当前卡的卡；**+** 同上（`blocked_by`）。

「新建…」在对应行下出标题输入，创建入口列新卡并立刻建关系。不再提供整宽「新建子任务」独立入口。


## 阻塞门禁（Blocking）

`Store.AssertUnblocked(taskID)`：

- 查出所有 `blocks` 指向本卡的源卡；
- 若源卡**不在**项目的 `done` 角色列，则仍视为未解除阻塞；
- 有未解除阻塞时返回中文 `InvalidError`。

调用点：

- **移入进行中 / 等你（review）**：服务在 `Move` 时按目标列角色检查（`assertUnblockedForColumnLocked`）；移入待办/完成不因依赖拦截。
- **派发 / 自动承接认领**：`executor` 在派发前 `AssertUnblocked`，被阻塞则不会开跑。

因此：依赖**不会**禁止「验收完成移入完成」本身；它主要挡住进入执行路径（doing / review / dispatch）。完成列上的阻塞方一旦进入完成，被阻塞卡即可放行。

## 与自动承接（auto-handoff）的关系

- 项目 `project_runs` 开启时，调度器从待办取卡派发；若卡被依赖挡住，会记 handoff 日志类事件（如等待 / 跳过），**不会**强行派发。
- 执行中的卡结束会话后通常进「等你」等人验收；依赖门禁与「人点验收」是两套机制。
- 自动承接日志在详情「自动承接」时间线展示；与 `task_relations` 无共享表，只是调度时会读阻塞状态。

## 相关代码

- `internal/store/relations.go` — 读写、防环、`AssertUnblocked`
- `internal/service/server.go` — Move / 关系 API 挂钩
- `internal/executor/dsh.go`（及 project run）— 派发前检查
- `cmd/taskboard/view.go` / `board.go` — 详情 UI 与 `changeTaskRelation`

## 刻意不做的事

- 不会在打开详情时静默建链。
- 阻塞不做成跨 Git/看板的事务；只约束看板状态机与派发。
- 「相关 related」类型在存储层存在，当前详情主路径未当一等公民暴露。
