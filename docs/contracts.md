# 工程契约（v0）

服务、CLI、MCP 与原生 UI 共享的接口。改这里 = 改契约，先改文档再改代码。

## 进程与地址

- 单一二进制 `cmd/taskboard`（原生窗口）与 `cmd/tb`（CLI）、`cmd/taskboard-mcp`（MCP stdio）。
- 内建 HTTP 服务默认监听 `127.0.0.1:47826`，`TASKBOARD_ADDR` 可覆盖。
- 数据库默认 `mygo.App.Path(mygo.PathUserData)` 目录下 `taskboard.db`（注意：PathUserData 是 `mygo.PathName` 常量，不是函数；返回 `(dir string, err error)`），`TASKBOARD_DB` 可覆盖。
- 测试钩子：`TASKBOARD_SLOW=<duration>` 给每个请求注入延迟（截图验证加载态用，非产品功能）。
- GUI 与 CLI/MCP 都是服务的客户端；写入只经服务，服务是唯一写者。GUI 通过 SSE 就地更新，禁止轮询。

## 数据（SQLite，经 mygo plugins/sqlite）

```
projects(id TEXT PK, name TEXT, workspace_path TEXT DEFAULT '', position INT, created_at TEXT, updated_at TEXT)
columns(id TEXT PK, project_id TEXT, name TEXT, position INT, role TEXT NOT NULL DEFAULT '')
tasks(id TEXT PK, project_id TEXT, column_id TEXT, title TEXT, body TEXT,
      position INT, version INT, archived INT, last_agent TEXT,
      created_at TEXT, updated_at TEXT)
comments(id TEXT PK, task_id TEXT, author TEXT, body TEXT, mention TEXT, parent_comment_id TEXT DEFAULT '', created_at TEXT)
project_runs(project_id TEXT PK, runtime TEXT, status TEXT, task_ids TEXT, cursor INT, current_task_id TEXT, detail TEXT, updated_at TEXT)
```

首次打开空库不创建项目或演示卡。用户创建项目时生成默认列 `待办/进行中/等你/完成`（各自带角色 intake/doing/review/done）。升级时，只有角色为 `review` 且仍使用旧默认名 `评审` 的列会自动改名为 `等你`；自定义列名保留。旧库首次角色回填仍以旧名 `评审` 识别 review 列。已有数据库中的项目和卡片会保留。时间 RFC3339 UTC；ID 为 16 字节随机 hex。

列角色：`role` 取值 `intake`（入口）|`doing`（进行中）|`review`（评审）|`done`（完成）|`""`（无语义），同一项目内同一角色最多一个列。旧库首次打开时补 `role` 列并按列名精确回填（`待办`→intake、`进行中`→doing、`评审`→review、`完成`→done，其余留空）；回填只填空值、不覆盖已显式设置的角色，重复打开结果一致。客户端与服务解析某角色时先认 `role`，项目内没有该角色才退回旧的中文列名，因此旧库、改过列名的项目行为不变。

## HTTP API（JSON）

身份头 `X-Agent`（缺省 `local`）；写入方把身份带入事件与 `lastAgent`。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/health` | `{"ok":true}` |
| GET | `/api/projects` | `[Project]`（含可选 workspacePath） |
| POST | `/api/projects` | `{name, workspacePath?}` → 201 Project，含默认四列 |
| PATCH | `/api/projects/{id}` | `{name?, workspacePath?}`（提供即更新）→ 200 Project，事件 `project.changed`；`name` 须非空，`workspacePath` 空字符串清除路径、`~`/`~/` 前缀展开为家目录、展开后须为本机存在的绝对目录；不迁移卡片、不创建目录 |
| DELETE | `/api/projects/{id}` | → 200 `{"ok":true}`，级联移除项目及全部卡片；项目自动承接仍开启或任一卡片执行记录活跃时返回 409 `project_busy`，须先停止承接或等原会话结束 |
| GET | `/api/projects/{id}/board` | `Board`（列 + 任务，含 archived=false；每张任务含可选 `execution` 会话记录） |
| PATCH | `/api/projects/{id}/columns/{columnID}` | `{"role":"intake|doing|review|done|""}` → 200 `Column`，事件 `project.changed`；`role` 必填且须在枚举内，否则 400；项目或列不存在、列不属于该项目 404；同项目内该角色已被另一个列占用 409 `role_conflict`（不静默抢占）；把列重设成自己已有的值或空字符串（清空）允许 |
| GET | `/api/tasks?projectId=` | `[Task]`，projectId 必填；每张任务含可选 `execution` 会话记录；可选 columnId、assignee、limit、offset、summary |
| GET | `/api/tasks/{id}` | `Task`（含正文与全部评论） |
| POST | `/api/tasks` | `NewTask`（可选 `dispatchRuntime`）→ 201 `Task`，事件 `task.created`；带 `dispatchRuntime` 时同一请求建卡并派发该渠道（先校验渠道与项目 workspacePath，无效 400 不建卡；建卡后派发启动失败回写失败评论） |
| PATCH | `/api/tasks/{id}` | `TaskPatch`（version 必填）→ 200 `Task`；移动列时事件 `task.moved`（带 from/to），否则 `task.updated` |
| POST | `/api/tasks/{id}/archive` | `{"version":N}` → 200 `Task`，事件 `task.archived` |
| GET | `/api/tasks/{id}/comments` | `[Comment]` |
| POST | `/api/tasks/{id}/comments` | `NewComment`（`text`，可选 `mention`：`dsh`\|`codex`\|`pi`\|`claude`、`parentCommentId`）→ 201 `Comment`，事件 `comment.added`；`parentCommentId` 必须属于同一任务，允许回复任意层级评论；`mention` 未知渠道返回 400。在评审列添加带 `mention` 的评论时，评论保存后将卡片移回入口列（若项目定义了入口列），并发出 `task.moved`；普通评论不触发此回退。 |
| PATCH | `/api/tasks/{id}/comments/{commentId}` | `{"text":"..."}` → 200 `Comment`，事件 `comment.updated`；仅评论作者可编辑，否则 403；空文本返回 400。编辑只更新看板内容，不会重新投递评论。 |
| DELETE | `/api/tasks/{id}/comments/{commentId}` | → 204，事件 `comment.deleted`（含被删除评论 ID）；仅评论作者可删除，否则 403。直接回复在被删除的评论会提升为该任务的一级评论并发出 `comment.updated`，其后代层级保留。 |
| POST | `/api/tasks/{id}/execution/release` | 空请求体 → 200 `Task`，事件 `task.updated`；把该卡执行记录收敛为 `released`（写锁释放），`X-Agent` 记入 detail。无执行记录 404 `no_execution`；状态仍是 `submitting`/`running`/`waiting_approval` 时 409 `execution_active`（先在原渠道停掉该会话，避免并行写者） |
| POST | `/api/tasks/{id}/dispatch` | `{"runtime":"dsh"|"codex"|"pi"}` → 202 `{"taskId","runtime"}`；异步派发所选渠道（cwd=项目 workspacePath）；DSH 接已启动的桌面 Host，Codex/Pi/Claude 由 Herdr App 在匹配项目工作目录的既有 space 中新增可见 tab/agent terminal，执行 Agent 按技能认领并回写评论/状态；程序只跟踪连接和会话 |
| POST | `/api/projects/{id}/run` | `{"runtime":"dsh"|"codex"|"pi"|"claude"}` → 202 `ProjectRun`；开始一轮项目承接 |
| GET | `/api/projects/{id}/run` | 该项目最新一轮 `ProjectRun`；没有则 404 |
| POST | `/api/projects/{id}/run/stop` | 空请求体 → 200 `ProjectRun`；停止后续派发，不取消已经在跑的那一张卡 |
| 打开 Codex 会话 | 原生 UI 本地动作，不新增 HTTP 字段；有 session ID 的 Codex execution 在卡片右键菜单显示入口，Herdr Codex 卡片另显示可直接定位原 terminal 的对话图标（活动期间也可点，因为只聚焦原 terminal，不恢复或创建会话）；右下角 Herdr 状态图标在会话非活动时也可点击直达对应 terminal；旧 CLI session 使用记录的 session ID 与项目工作目录启动 Ghostty 中的 `codex resume <sessionID>`，活动期间置灰以避免并行恢复 |
| GET | `/api/events?projectId=` | SSE：每次连接先发 `board.snapshot`（含一致快照、序号及任务的可选 `execution` 会话记录），再发新于快照的 `event: <type>` + `data: <Event JSON>`；每 15s `: ping`。执行会话状态变化通过完整任务快照的 `task.updated` 推送，task version 不变；卡片列不因此自动改变。断线恢复依靠重连快照，不依赖事件历史重放 |

错误：`400` 参数缺失；`404` 不存在；`409` `Conflicterror:"version_conflict", task}`（列角色冲突为 `{"error":"role_conflict"}`）。版本冲突时客户端拉一次详情再对齐。

Dispatch 语义（2026-10-30）：`runtime` API 值兼容 `dsh`、`codex`、`pi`、`claude`。Codex/Pi/Claude 先按 space `folder` 与项目 `workspacePath` 精确匹配；没有精确目录匹配时，仅当同名 space 的 `folder` 是项目 `workspacePath` 的父目录，才按项目名回退；存在多个候选时优先唯一活动项，仍有歧义则明确失败。没有符合条件的 space 时，用 `space.new` 创建正式项目 space（名称为项目名、目录为 `workspacePath`），随后在该 space 通过 `tab.new` 创建可见任务 tab/agent terminal；不会为每张卡创建 space，也不会创建临时测试 space。`executions.session_id` 保存 Herdr terminal address。Herdr Core (`hc`) 跟踪状态、发送同一会话的评论跟进；进程结束、`done` 或 `idle` 只表示 turn 结束，不改变任务列。Herdr App 不可用、无法确认创建的项目 space 或无法确认新建 terminal 时，派发明确失败，禁止回退到独立 CLI 或另建会话。DSH 暂保留原桌面桥接与配置。Herdr 控制的 Codex/Pi/Claude 使用 Herdr 已配置的 agent/model；Taskboard 不改写 Herdr 或 CLI 的全局模型配置。旧 CLI session ID 不作为 Herdr terminal address 使用，旧会话继续按兼容路径跟进。`runtime` 的对外字段与菜单选项保持不变。

可在数据库旁的 `<数据库文件名>.runtimes.json` 设置看板独立连接，例如 `{"codex":{"baseUrl":"http://127.0.0.1:3425/v1","model":"codex/gpt-6-luna","reasoningEffort":"medium"}}`。仅 Codex 在配置该连接时按次使用 `--ignore-user-config` 和独立 Responses provider 参数；沿用 CLI 登录文件但不加载其全局模型/代理配置，工作区规则仍由 CLI 正常加载。本机的该配置通过已有 Magpie 网关调用 Codex 模型；它仍使用 Codex 原生执行器。配置文件不存在或未设置该连接时保持原有 CLI 行为；配置错误明确回写失败。文件在每次派发时读取，新配置不影响已经运行的任务。

DSH 使用已启动的 DeepSeek Harness 桌面端，在 desktop profile 启用 `@agents-anywhere/dsh-bridge-next`。通过 `~/.agents-anywhere/dsh-bridge/endpoint.json` 鉴权；不回退到 headless。测试或自定义位置可设置 `TASKBOARD_DSH_ENDPOINT`。渠道配置增加 `"dsh":{"provider":"<provider ID>","model":"<精确 model ID>","reasoningEffort":"high"}`（推理强度可省略），默认模式 standard、权限 workspace-write。SQLite `executions` 表及 `GET /api/tasks/{id}` 的 execution 只描述连接/会话：submitting/running/waiting_approval/unknown/disconnected/detached/returned/failed/released；旧 succeeded 保持兼容。`detached` 是桌面渠道的「看板停止跟踪、桌面会话仍在、下次启动恢复跟踪」，会话仍可续；`released` 是收敛终态，写锁已释放，见下方「执行记录收敛」。returned 表示会话交回，不是任务完成。会话标识先落盘再发送；回执丢失、断连、重启后接续原会话，不自动创建第二个。

项目自动承接（2026-10-28，修订 2026-10-30）：`POST /api/projects/{id}/run` 打开该项目的持续开关，并记住所选渠道；有进行中且执行仍活跃的卡片时也允许开启，记录其中一张为当前等待项，在它离开进行中前不派发下一张。开启期间，先处理进行中且执行记录不是活跃的卡，再处理待办；两类内部按看板从上到下。评审、完成、归档卡不自动处理。若有多张进行中且仍活跃的卡，当前等待项离开进行中后重新检查其余卡片，不并发派发。Agent 会话正常返回但卡片仍留在进行中时保持开关开启并等待人工移动卡片，不为该卡另建会话；派发失败、执行失败或卡片被归档时关闭开关并写明原因。服务正常关闭取消会话跟踪时保留开关，重启后恢复原有会话跟踪，不为该卡另建会话。同一项目开关已开启返回 409。每次只新开所选渠道的一个会话处理一张卡，不续原会话；该卡离开进行中后才处理下一张。当前没有可处理的卡时开关保持开启并等待。卡片进入待办，或停住的卡进入进行中时，如果当前没有卡在处理，就接上它。`POST /api/projects/{id}/run/stop` 关闭开关，不取消已经在跑的那一张卡。项目未设有效工作目录或渠道未知返回 400。原生顶栏在选中具体项目时提供「自动承接」开关：关闭时点开选渠道，开启时再点关闭。

Codex 会话入口（2026-10-30）：Board、任务列表及 SSE 一致快照中的每张任务都包含可选 `execution` 会话记录，供卡片右键菜单使用。对有 session ID 的 Codex execution 显示打开入口；Herdr 会话显示「在 Herdr 中定位 Codex 对话」，其他会话显示「在 Ghostty 打开 Codex 对话」。Herdr 入口无论执行状态如何都可用，因为它只聚焦原 terminal，不恢复或创建会话；旧 CLI 的 Ghostty resume 在执行仍活动时置灰，避免并行恢复同一会话。Herdr 会话通过 `herdr-app do agent.focus` 定位对应 terminal；其他会话以项目工作目录启动 `codex resume <sessionID>`。不经看板服务写数据，也不创建新的派发会话。缺少 session ID 时不显示入口，缺少工作目录时不能恢复旧 CLI 会话。

任务文本仅一句：`使用 manage-taskboard-native 执行任务 <任务 ID>。`。技能通过 CLI 读取原卡正文、全部评论和项目 ID，再查询项目的工作目录与状态列；不在提示词中复制任务正文、项目元数据或技能流程。工作目录由派发器设置。执行 Agent 按技能先读原卡及评论，再认领、执行、验证、回写结果评论、移到评审并 GET 确认。所有看板写请求用自身 X-Agent 署名；完成留给用户确认。派发程序不认领、不代写工作结果、不根据退出码、最终文字或会话正常结束自动移动卡片；只保存会话记录和派发/连接诊断。非默认看板实例由 CLI 的 `TASKBOARD_ADDR` 或 `--addr` 定位，不在提示词重复传递。执行 Agent 无法回写或中途异常时，保留卡片当前状态及技术诊断，恢复后由 Agent 核对原卡与产物、补齐缺失回写，不由程序猜测结论。`400`：runtime 未知、无有效 workspacePath；`409`：该卡已有执行或状态不确定的原会话，跨渠道同样互斥。服务 Close 仅停止 Herdr/DSH 跟踪、保留其原 terminal，下次启动接续；旧 CLI 进程按旧兼容路径处理。均不修改卡片工作状态。

新建任务的执行渠道选择仅供 `⌘Enter` 使用：创建后派发到该渠道并关闭浮层；未设置工作目录时不创建、不派发并保留草稿。Enter 仅创建并关闭，Shift+Enter 仅创建并继续。建卡与派发也可一步完成：`POST /api/tasks` 带 `dispatchRuntime`，CLI 对应 `tb add <标题> --dispatch dsh|codex|pi|claude`（两种模式：仅建卡 / 建卡并执行）。Codex 使用 `--skip-git-repo-check` 支持已有的非 Git 项目目录。真实 CLI 闭环探针：`TASKBOARD_LIVE_RUNTIME=pi go test ./internal/service -run '^TestLiveChannelDispatch$' -v -count=1`（使用临时工作区与数据库，验证本轮新文件、结果评论和评审状态）。

类型定义唯一来源：`internal/model/model.go`（JSON tag 即线格式）。

评论提醒渠道（2026-10-26）：评论可带一个 `mention` 字段，表示这条评论指定由哪个执行渠道跟进。它是结构化字段，不从正文解析——正文里写成 `@claude` 的散文只是文字，永远不会改变路由。原生详情在评论框输入词首 `@` 会在评论框上方浮出渠道菜单（不挤压草稿，点外部或 Esc 关闭），`@` 不进正文；已选渠道以 `@<渠道>` 角标显示，点击取消；选完渠道键盘回到输入框；角标在草稿之前，光标在开头时按退格删除角标而不动正文。在评审列添加带结构化 `mention` 的新反馈会将卡片退回入口列，让待处理工作重新进入待办。直接评论已有可续接原会话的任务时，评论保存后自动交回该会话；没有原会话时只记录评论。显式 `mention` 仍用于指定其他执行渠道，并在评论发送后自动跟进。

路由规则：无 `mention` 时沿用卡片已有会话；`mention` 与卡片当前渠道相同则续该会话；`mention` 是另一渠道、或卡片还没有会话时，在那个渠道新建会话并把记录替换为它——这条 `mention` 就是人类显式授权的换渠道，程序不会在没有 `mention` 时自行新建第二个会话。该卡正在执行或投递状态不确定时同样返回 409，不切换渠道。

评论跟进（2026-10-26）：`POST /api/tasks/{id}/comments/{commentId}/continue`（空请求体）返回 202，把已保存评论交回该卡的原会话；相同评论重复请求幂等，不重复执行。原会话正在运行或投递结果不确定时返回 409，无可用会话记录时返回 400，不新建会话作为替代。原生详情对已有可续接原会话的任务，发送普通评论后自动调用此接口；对没有原会话的任务，评论只保存，仍可对已发送评论显式「让 AI 跟进」。评论已保存但跟进失败时明确提示，保留评论供重试。

执行记录补充 `runtime`、`followupId`、`previousTurnId`；旧记录默认 DSH。Codex 新派发使用 JSON 事件保存精确 thread ID，续聊使用 `exec resume <ID>`；Pi 新派发指定独立 session 文件，续聊仅使用该已存在文件。旧 Codex/Pi 任务缺少记录时无法原地续聊。跟进提示只引用技能、任务 ID、评论 ID，由 Agent 重读原卡和全部评论；提示词明确要求按评论内容执行并回写，并禁止在被跟踪的会话里再次调用 `tb followup`（那会空转，只是把自己的投递再确认一次）。所有跟进仍由执行 Agent 回写任务结果及状态，服务不代判完成。

CLI 每次执行通过进程环境传递本实例的 `TASKBOARD_ADDR` 与执行身份 `TB_AGENT`；Codex 按次启用 `sandbox_workspace_write.network_access=true`，以访问本机看板 HTTP，文件写入仍受 workspace-write 限制。Herdr 启动的 Codex 交互会话也必须显式带上同一配置，否则其中的 `tb` CLI 无法访问本机 API，并会反复触发命令审批。该设置允许该会话的沙箱联网，不限于看板环回地址；文件写入仍由 workspace-write 限制。Pi 会话文件保存在数据库连接配置旁的 `.sessions/` 目录，备份时一并保留。CLI 可用 `tb followup <taskID> <commentID>` 对已保存评论发起跟进。

`followups` 表持久化每条评论的投递记录。发送前落盘，回执丢失记为 unknown，禁止自动重发；DSH 重启仅跟踪原会话，并排除上一轮 turn.end，不能把旧回合当成本轮完成。CLI 服务重启后标记 disconnected：进程已随服务结束，该记录不再阻塞后续派发或评论跟进（跟进仍续原会话 ID，派发另起新会话），也不自动重跑；重跑须由人显式派发或跟进。DSH 的 disconnected 表示原会话仍在、只恢复跟踪，语义不同。已确认未提交的失败可重试；执行已开始或是否接收不确定的评论不再重复投递。

执行记录收敛（2026-10-30）：`execution` 只是连接/会话记录，但必须能收敛——否则一个永久消失的会话会永久占住写锁，卡片既不能跟进也不能重新派发，UI 还会一直挂着「连接异常」。`released`（已释放）是收敛终态：原会话已确认不可达，写锁已释放，卡片可按人的指令重新派发或评论跟进。它与 `detached` 不同：`detached` 只表示看板停了跟踪而桌面会话还在，仍占锁并可续；桌面渠道不会用 Herdr 会话，因此 `detached` + `herdr:` 前缀的记录按 `released` 处理（旧版本遗留写法）。三条写入 `released` 的路径：(1) Herdr 跟踪期——`hc get` 连续不可达超过宽限期（默认 120s，每 2s 探一次）后写入并停止跟踪；宽限内恢复则继续跟踪。连接徽章有防抖：连续 3 次探测失败（默认约 6s）才写 `disconnected`，单次失联保持原状态，避免路由抖动闪出「连接异常」。(2) 服务重启——仍处于 `disconnected`/`unknown` 的 Herdr 记录重新进入同一宽限探测，因此永久消失的 terminal 在重启后自动收敛，不需要人工改库。(3) 人工释放——`POST /api/tasks/{id}/execution/release` 由人显式确认后写入，`tb execution release <taskID>` 是它的 CLI 入口；它用于没有活跃跟踪者的卡片，若该会话仍被跟踪，跟踪器会以自己的观测覆盖这条记录（跟踪者更了解实况）。终态集合 `succeeded`/`returned`/`failed`/`released` 都不再阻塞派发与评论跟进；`submitting`/`running`/`waiting_approval`/`unknown`/`disconnected`/`detached` 仍占锁。服务正常关闭（跟踪被取消）不写 `released`，保留原记录供下次启动接续。UI 在所有列显示已知的 Herdr 执行状态；「执行已返回」（兼容旧状态 `succeeded`）表示 AI 回合结束并交回控制权，只在「等你」列显示，便于对应人工评审；它不代表任务已验收，也不自动移动卡片。运行中、等待、断连、失败、已释放等状态在各列照常显示。永久断连依靠 `released` 收敛，不能靠隐藏状态掩盖。

任务的可选 `completedBy` 字段表示最近一次成功执行的 Agent，来自执行器写入的成功评论（Codex / DSH / Pi Agent），与最后操作人 `updatedBy` 分开。历史成功记录直接生效；手动编辑或移列不覆盖执行归属，失败和取消不算完成。卡片及详情的历史行优先显示「Codex 完成」等归属，没有成功记录时沿用最后操作人。

Herdr 状态在卡片上用有色图标表示，悬停提示和无障碍名称提供完整中文状态；状态变化仍通过 SSE 原地刷新。`returned`/旧版 `succeeded` 仅在「等你」列显示。

Pi 的看板独立模型配置为 `{"pi":{"provider":"magpie","model":"zhipu/glm-5.3-flash"}}`，与既有 Codex 项并存。派发时使用 `pi --print --provider magpie --model zhipu/glm-5.3-flash -- <任务文本>`，不改写 Pi 全局默认模型、扩展、MCP 或登录配置。本机备用模型 `codex/gpt-6-luna` 已通过 Pi 短请求探针；需要切换时修改同一 `pi.model` 字段，下一次派发即生效。执行失败后不自动换模型重跑，以免重复修改文件。

## CLI（cmd/tb）

```
tb projects [--json]
tb project add <name> [--path ABSOLUTE_DIRECTORY] [--json]
tb list [--project ID] [--column NAME|ID] [--assignee WHO] [--limit N --offset N] [--summary] [--json]
tb show <taskID> [--json]
tb add <title> [--project ID] [--column NAME|ID] [--body TEXT]
tb move <taskID> <column NAME|ID>
tb column <columnID> <role> [--json]   # role: intake|doing|review|done，空字符串清空
tb comment <taskID> <body> [--mention dsh|codex|pi|claude]
tb followup <taskID> <commentID>
tb execution release <taskID>   # 把执行记录收敛为 released（释放写锁）
tb archive <taskID>
tb serve [--addr ...]        # 前台跑服务（--detach 后台）
```

- 身份：`--agent` 或 `TB_AGENT`，默认 `local`。
- `tb column` 按列 ID（名字亦可）定位列所属项目，再写该列的角色；角色冲突时服务返回 409，CLI 报「该角色已被其他列占用」并退出 1。
- 除 `serve` 外，先 `GET /api/health`；不通则自动 `tb serve --detach` 并等 3s。仍不通报错退出。

## MCP（cmd/taskboard-mcp，stdio，换行分隔 JSON-RPC 2.0）

- `initialize` → `protocolVersion: "2025-06-18"`、`serverInfo{name:"taskboard-native"}`、tools 能力。
- `tools/list`：`list_projects`、`create_project`（name，workspacePath 可选）、`list_tasks`（projectId 必填，支持 columnId/assignee/limit/offset/summary）、`get_task`（id，正文及评论）、`create_task`（可选 `dispatchRuntime`：省略只建卡，带上则同一调用建卡并派发该渠道）、`update_task`、`move_task`、`add_comment`（可选 `mention`）、`list_comments`。
- `tools/call` → 调 HTTP API，`content:[{type:"text", text:<JSON>}]`。
- 身份：`TASKBOARD_AGENT`，默认 `mcp`。

## 原生 UI（cmd/taskboard）

- 任务详情的正文与评论支持粘贴剪贴板图片（PNG/JPEG/GIF/WebP，单张最多 25 MiB、最多 4000 万像素）。在光标/选区位置插入 Markdown 图片引用并显示预览；普通文本保持原粘贴行为。图片上传期间保留原文、禁止提交，失败可重试。
- 图片按自身密度换算成 DIP 展示，不按像素 1:1 放大：文件自带密度（PNG `pHYs`、JPEG JFIF 单位）时以其为准，缺失时对「至少一边 ≥ 400 像素」的图按主显示器 backing scale 推测（Retina 屏上从 macOS 截图复制到剪贴板会丢掉密度信息）。推测只用于缩小，不放大；小于该阈值的无密度图保持 1×。展示时保留图片原始像素，只将独立图片段落的控件尺寸按密度换算成 DIP，避免先降采样再由 Retina 屏放大；服务端原图字节不变。
- 正文和待发送评论的每张预览图均提供「移除」，只移除该次引用；同一图片重复插入可分别移除。正文移除后点击保存生效，评论草稿移除后随发送生效。已发送评论沿用现有只读规则。图片文件可能被其他任务引用，因此不随引用移除而删除。
- `POST /api/tasks/{id}/images` 接收原始图片字节，201 返回 `{url}`（稳定相对路径）；`GET /api/images/{sha256}` 返回图片。图片按内容摘要去重，保存在数据库旁的 `<数据库文件名>.images/`，随数据库一同备份。上传本身不改变任务版本；正文保存和评论发送沿用原接口及 SSE。未提交的图片暂留本地，不自动清理。
- 快速输入框（新建任务）同样支持粘贴剪贴板图片：任务尚未创建，图片先经 `POST /api/images`（不绑定任务，校验与存储规则同绑定端点）暂存，201 返回同一 `{url}`；提交时把图片 Markdown 一并写入任务正文。预览图可移除，取消创建留下的暂存文件不自动清理。

- 单窗口看板：项目切换、列、卡片、建卡/编辑/移动/评论（可回复任意评论；作者可从更多菜单编辑或删除自己的评论）/归档、加载态（`plugins/loading`）、空态、明暗主题跟随系统。
- 数据只经 HTTP 客户端；SSE 事件就地更新，不整表重建。版本冲突按契约拉详情对齐。
- 视觉规格见 `docs/design.md`（乔布斯产出）。

## 分工（写入边界）

- `internal/store/**`、`internal/service/**`、`cmd/tb/**`、`cmd/taskboard-mcp/**`：Linus（后端 + 代理接口）。
- `internal/client/**`、`cmd/taskboard/**`、`docs/assets/render/**`：Ive（原生 UI）。
- `internal/model/**`、`docs/contracts.md`：orchestrator。
- `docs/design.md`：乔布斯。`docs/design-review.md` + 截图：Dieter Rams。
- 禁止 `go get` / `go mod tidy`（依赖已固定）；禁止 git commit/push；禁止跑 `./...` 全量构建/测试（兄弟任务在写）。


## Agent 读取与工作区（2026-10-25）

本轮取舍与验收见 `docs/agent-read/plan.md`。项目路径使用 JSON 字段 `workspacePath`、数据库列 `workspace_path`；旧数据库首次打开仅补这一列，旧项目与旧名称创建均兼容。提供路径时，API 校验它是本机存在的绝对目录并规范化；不创建目录、不初始化 Git、不派发执行，不要求路径唯一。路径仅用于后续定位执行目录，本轮不引入自动调度。

读取推荐路径：`list_projects → list_tasks(summary=true, filters...) → get_task(id)`。摘要仅包含 id/projectId/columnId/title/version/updatedAt/updatedBy，assignee 非空时返回；不含正文与评论。需要完整内容时按 ID 获取正文与全部评论，不必拉取整个项目的正文。

`columnId` 必须属于项目；assignee 精确匹配；limit 为 1..200 的整数；offset 为非负整数且必须配合 limit。先过滤再分页，顺序与看板一致；offset 为当前数据的偏移分页，不提供并发变更下的快照保证。summary 仅接受 true/false。未传新参数时保持原有全量数组返回，空结果为 []。
