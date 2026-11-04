# taskboard-native 并发安全分析

- **日期**：2026-10-31 Asia/Shanghai
- **范围**：`internal/store`、`internal/service`、`internal/executor`、`cmd/taskboard`（客户端合并逻辑作后果佐证）；不改产品代码
- **方法**：静态阅读 Mutex / version 乐观锁 / SSE hub / auto-handoff（claim·Dispatch·ProjectRun）/ Herdr `SaveExecution` 写入顺序；对照 `UpdateTask`/`PatchTask`、`AddTaskRelation`、`AddComment*`、`MoveTask`、`NoteProjectRun` 等路径

---

## 结论

单进程内 Store 用一把 `sync.Mutex` 串行化所有 SQLite 读写，任务字段变更靠内存侧 `version` 校验，再 `emit` SSE；该层对「同进程多 HTTP/GUI」基本正确。主要风险在 **Store 锁之外的跨步骤 TOCTOU**（评论后移列、派发前 AssertUnblocked、claim 前后 ProjectRun 游标）、**ProjectRun / Execution 无 CAS 的整行覆盖**，以及 **启动恢复未持 `projectRunMu` 与已恢复的 Herdr 跟踪并发写 run 状态**。自动承接与人手操作通过 version 争用列移动时「人手优先 / claim 失败可回滚」设计清晰，但失败路径仍可能把整轮承接置为 `failed` 或短暂卡在错误的 `CurrentTaskID`。

---

## 问题清单

1. **[P0] · executor/recover · 启动恢复与 Herdr 跟踪并发写 ProjectRun**  
   - **问题**：`NewDispatcher` 先对活跃 Herdr 调用 `resumeHerdrTracking`（后台 `trackHerdr` → 结束后 `finishProjectCard`，持 `projectRunMu`），随后同步执行 `recoverProjectRuns()`，该函数**不**获取 `projectRunMu`，却多次 `SaveProjectRun` / `claimProjectCard` / `Dispatch`。  
   - **触发场景**：服务重启时某卡片 Herdr 会话已近结束，恢复跟踪很快 `returned`，与 `recoverProjectRuns` 同时改同一 `project_runs` 行。  
   - **后果**：`SaveProjectRun` 为整行 UPSERT、无版本字段 → 后写覆盖先写；可能丢 `CurrentTaskID`/cursor、重复派发或错误失败态。  
   - **依据**：`internal/executor/dsh.go`（`NewDispatcher` 中 `resumeHerdrTracking` 早于 `recoverProjectRuns`）；`internal/executor/project_run.go` `recoverProjectRuns`；`internal/store/project_run.go` `SaveProjectRun`。

2. **[P0] · store/execution · Execution 状态无 CAS，跟踪协程可覆盖终端态**  
   - **问题**：`SaveExecution` 在持锁下整行 UPSERT，仅跳过「完全相等」的重复写；**不**校验期望旧状态。`trackHerdr` 使用内存中的 `*Execution` 周期性回写。  
   - **触发场景**：跟进评论 `sendHerdrFollowup`、探测循环写 `disconnected`/`running`，与另一路径写入的 `returned`/`failed`/`released`（或人工可释放态）交错。  
   - **后果**：已收敛的执行记录被旧探测结果「复活」为 active，卡片写锁语义与自动承接 `finishProjectCard`/`Ended()` 判断错乱；可能阻止再派发或错误推进队列。  
   - **依据**：`internal/store/execution.go` `SaveExecution` / `executionStatement`；`internal/executor/herdr.go` `trackHerdr`、`sendHerdrFollowup`。

3. **[P1] · service/comments · @提及评论「先写评论再移回待办」版本 TOCTOU**  
   - **问题**：`handleAddComment` 在提及且卡片在「等你」时：先 `Task()` 取 `version`，再 `AddCommentWithParent`（**不 bump version**），再 `MoveTask(id, 旧 version, intake, …)`。移列与读版本之间可插入人手/自动承接的 `PatchTask`/`MoveTask`。  
   - **触发场景**：用户在「等你」@agent 跟进的同时，另一客户端改标题/移列，或自动承接 `advanceFinishedCard`/`claim` 抢 version。  
   - **后果**：评论已落库，但移列返回 `VersionError`；评论留下、卡片仍停在「等你」，与「有地址评论应回到待办再执行」的产品意图不一致；调用方只看到冲突错误。  
   - **依据**：`internal/service/server.go` `handleAddComment`；`internal/store/mutations.go` `AddCommentWithParent`（version 不变）、`PatchTask` version 检查。

4. **[P1] · executor/handoff · `dispatchProjectCard` 先推进 cursor/CurrentTaskID 再 claim**  
   - **问题**：先 `CurrentTaskID=id; Cursor++; SaveProjectRun`，再 `claimProjectCard` / `Dispatch`。claim 失败（人手已移出待办、version 冲突、归档）时已把游标持久化。  
   - **触发场景**：自动承接扫描到待办卡，人手几乎同时拖走或编辑导致 `MoveTask` version 冲突。  
   - **后果**：本批 `TaskIDs` 上该卡被视为已消费；整轮常被标 `failed`；`CurrentTaskID` 可能指向从未成功 claim 的卡。下一轮 `dispatchNextReady` 会重扫看板，但中间 UI/日志呈现「已承接却未派发」。  
   - **依据**：`internal/executor/project_run.go` `dispatchProjectCard`、`claimProjectCard`、`releaseProjectCard`（仅在已 claim 且 version 仍匹配时回滚列）。

5. **[P1] · executor/dispatch · AssertUnblocked 与真正开跑之间无原子性**  
   - **问题**：`Dispatch` 在持 `d.mu` 时调用 `AssertUnblocked`（内部短持 `store.mu` 后释放），随后才登记 `inflight` 并异步 `run`；`handleDispatch` 还在进 Dispatcher 前单独 Assert 一次。依赖边可在两次检查与 `SaveExecution`/`herdrCreate` 之间被写入。  
   - **触发场景**：A 开始派发，B 同时 `AddTaskRelation(..., blocked_by/blocks)` 挂上未完成前置。  
   - **后果**：被依赖阻塞的卡仍启动 Herdr/CLI；之后 `syncRunningTaskColumn`→`MoveTask` 进「进行中」可能因 `assertUnblockedForColumnLocked` 失败，出现「会话已开、卡片未进进行中」并写说明评论（workflow 已部分兜底，但会话已外泄）。  
   - **依据**：`internal/executor/dsh.go` `Dispatch`；`internal/executor/workflow.go` `syncRunningTaskColumn`；`internal/store/relations.go` / `mutations.go` 移入 doing/review 时的阻断检查；`internal/service/server.go` `handleDispatch`。

6. **[P1] · service/sse · 慢订阅者缓冲满被丢弃，无服务端重放**  
   - **问题**：`hub.publish` 在 Store 锁内调用；订阅 channel 容量 64，`default` 分支直接 `delete`+`close` 该订阅。恢复依赖客户端重连拿新 `board.snapshot`，不支持 `Last-Event-ID` 重放。  
   - **触发场景**：高频 `SaveExecution` 探测（约 2s）叠加移列/评论，或 GUI 卡顿未及时读 SSE。  
   - **后果**：连接被服务端静默踢掉；在重连快照前本地看板与评论滞后；若客户端未立即重订，长时间呈现过期 version/列位置。  
   - **依据**：`internal/service/sse.go` `publish` / `handleEvents`；`docs/realtime-sync.md`（明确溢出后靠重连快照）。

7. **[P1] · store/project_run · ProjectRun 无乐观锁且不发 SSE**  
   - **问题**：`project_runs` 单行 UPSERT，无 `version`/条件更新；`SaveProjectRun` 不走 `emit`。运行时主要靠 `projectRunMu` 串行，但见问题 1；GUI 仅在打开项目/启停时 `loadProjectRun`。  
   - **触发场景**：自动承接推进队列期间用户盯着「自动承接」状态；或恢复路径与正常路径交错写。  
   - **后果**：UI 上的 CurrentTaskID/Detail 可长时间过期；并发写时无冲突可观测，只能靠后写覆盖。  
   - **依据**：`internal/store/project_run.go`；`cmd/taskboard/view.go` `loadProjectRun`；对比任务路径的 `emit`。

8. **[P1] · auto-handoff × 人手 · 完成推进/审批停靠使用易过期的 Task.Version**  
   - **问题**：`advanceFinishedCard` / `parkProjectCardForApproval` 使用调用方传入或稍早读到的 `task.Version` 做 `MoveTask`；失败时记录 log 并「等待人手移出进行中」，不重试读最新 version。  
   - **触发场景**：Herdr turn 结束自动移「等你」的同时，人手改描述或微调列内顺序（bump version）。  
   - **后果**：自动移列失败，卡片留在「进行中」阻塞队列（`run.Detail` 提示等待）；执行已 Ended 但看板列与自动承接游标脱节，需人手再拖一次。设计上「人手赢」正确，但一致性窗口偏长。  
   - **依据**：`internal/executor/project_run.go` `advanceFinishedCard`、`finishProjectCard`、`parkProjectCardForApproval`；注释已承认 version race。

9. **[P2] · store/mutations · SQL 更新未带 `WHERE version=?`（CAS 仅在进程内）**  
   - **问题**：`updateTaskStatement` / `ArchiveTask` 的 UPDATE 仅为 `WHERE id = ?`；乐观锁是「持 `s.mu` 读出版本再比较」后写回。包注释假定单写者进程。  
   - **触发场景**：未来多进程打开同一 DB、或外部工具直接写 SQLite。  
   - **后果**：进程外写入可被静默覆盖；version 数字仍单调但语义不再是真正的 CAS。  
   - **依据**：`internal/store/store.go` 包注释；`internal/store/mutations.go` `updateTaskStatement`、`ArchiveTask`。

10. **[P2] · store/relations · 关系变更只 bump 发起侧 version，对侧同 version 双事件**  
    - **问题**：`AddTaskRelation`/`RemoveTaskRelation` 只递增 path 任务 `version`，对 `related`（及旧 parent）`enrich` 后仍以**旧 version** 各 `emit` 一次 `task.updated`。  
    - **触发场景**：两边几乎同时改关系；或客户端按「version 严格递增才应用」实现（当前 GUI 用 `<` 比较，相等会覆盖，较安全）。  
    - **后果**：严格客户端可能丢对侧关系摘要；调试时出现「同 version 两次更新」。关系边本身在锁内事务写入，DB 不裂，主要是推送/合并语义。  
    - **依据**：`internal/store/relations.go`；`cmd/taskboard/board.go` `upsert`（`t.Version < local` 才拒绝）。

11. **[P2] · store/comments · 评论不参与 task.version；同评论并发改删无行级期望版本**  
    - **问题**：`AddComment*`/`UpdateComment`/`DeleteComment` 不 bump task version；事件携带当时的 task.Version 仅作附带字段。更新/删除靠 author 匹配，无 comment 版本。  
    - **触发场景**：两客户端同时改同一条评论；或一边删一边改。  
    - **后果**：Store 锁下串行，最终一致为 last-write-wins；先删后改得 `ErrNotFound`。不会与 PatchTask 破坏字段原子性，但无法向客户端报告「评论冲突」，只能报任务级冲突。  
    - **依据**：`internal/store/mutations.go` 评论段注释「task version is untouched」。

12. **[P2] · store/placement · 列内 renumber 改写兄弟 position 却不 bump、不逐卡 SSE**  
    - **问题**：`placeLocked`/`compactLocked`/`renumber` 在事务内更新同列其他卡的 `position`，但只对移动卡 bump version 并 `emit` 一次。  
    - **触发场景**：快速连续拖拽；或非官方客户端按事件增量维护 position。  
    - **后果**：官方 GUI 用 `reconcilePlacement` 按移动卡 position 重推算邻居，一般可恢复；其他客户端或中途快照前可能短暂错序。  
    - **依据**：`internal/store/mutations.go` `renumber`；`cmd/taskboard/board.go` `reconcilePlacement` 注释。

13. **[P2] · executor/claim · claim 读卡与 MoveTask 非单次 Store API**  
    - **问题**：`claimProjectCard`：`Task` → 判列 → `MoveTask(version)`，中间无把「断言仍在 intake」编进同一 store 方法；靠 version 与第二次列检查兜底。  
    - **触发场景**：与人手移列、第二次自动承接 recover 重叠。  
    - **后果**：通常表现为 `VersionError` 或「已离开待办」错误（可接受）；与问题 4 组合时放大为 ProjectRun 失败。`releaseProjectCard` 仅在 claimedVersion 仍匹配时退回 intake，避免覆盖人手编辑——此点设计正确。  
    - **依据**：`internal/executor/project_run.go` `claimProjectCard`、`releaseProjectCard`。

---

## 机制速览

### 锁

| 锁 | 位置 | 作用 |
|----|------|------|
| `Store.mu` | `internal/store/store.go` | 所有 DB 读/写与 `emit` 串行；`OnEvent` **同步**调用且禁止阻塞 |
| `hub.mu` | `internal/service/sse.go` | 订阅表；`publish` 在 Store 锁内被调用，只做非阻塞 channel 发送 |
| `Dispatcher.mu` | `internal/executor/dsh.go` | `inflight`、关闭标志；`Dispatch`/`Continue`/`Close` |
| `Dispatcher.projectRunMu` | 同上 / `project_run.go` | 串行 Start/Stop/Note/finish/park/DeleteProject；**recoverProjectRuns 未纳入** |

锁顺序惯例：`projectRunMu` → `Dispatcher.mu` → `Store.mu`（`DeleteProject`）；`OnEvent` 路径为 `Store.mu` → `hub.mu`，并以 `go NoteProjectRun` 避免在 Store 锁内再抢 `projectRunMu`。

### 乐观锁 version

- 任务：`PatchTask` / `MoveTask` / `ArchiveTask` / `AddTaskRelation` / `RemoveTaskRelation` 在锁内比较 `version`，成功则 `version+1`；冲突返回 `VersionError{Current}`（HTTP 409）。
- SQL **不**使用 `WHERE version=?`；正确性依赖单进程 + `Store.mu`。
- 评论、Execution、ProjectRun：**不**使用 task.version / 自身 version。
- Execution 变更仍 `emit` `task.updated`，**携带未变的 version**；GUI `upsert` 允许 version 相等时覆盖（执行徽章靠此更新）。

### SSE 与写入顺序

1. 调用方 → Store 方法加锁 → SQLite Transaction/Execute **成功** → `emit`（`seq++`，调用 `OnEvent`）→ 解锁。  
2. `OnEvent`：`hub.publish`（满缓冲丢订阅）→ 对 created/moved/archived 且带 `Task` 时 `go NoteProjectRun`（自动承接跟看板列变化）。  
3. SSE：先 `subscribe`，再 `Snapshot`（持 Store 锁读 `seq`+全量），推 `board.snapshot`；之后只转发 `ev.Seq > snapshot.Seq`。  
4. Herdr：内存状态机 → `SaveExecution`（锁内写 DB + 同 version 的 `task.updated`）→ 可能 `parkProjectCardForApproval` / 结束后 `finishProjectCard`（再 `MoveTask`+`SaveProjectRun`）。  
5. 自动承接 claim：`SaveProjectRun`（游标）→ `MoveTask`（version）→ `Dispatch`（`inflight`+异步会话）；失败且已 claim 则 `releaseProjectCard` 条件回滚列。

### 自动承接 vs 手动

- 人手与自动移列争用同一 `version`：一方 `VersionError`，自动侧多数字段失败即停靠等待或 `failed`。  
- 人手移出「进行中」/归档：经 SSE → `NoteProjectRun` 清空 `CurrentTaskID` 并 `continueProjectRun`。  
- `releaseProjectCard`：仅当卡仍停在 claim 产生的 version 时退回待办，避免覆盖人手编辑。

---

*本报告仅分析落盘；未修改产品代码、未 commit/push、未派 Herdr/Codex。*
