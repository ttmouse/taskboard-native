# 设计验收：MyGo 原生看板 v0（taskboard-native）

**日期**：2026-10-24（20:12–21:05 实跑）
**对象**：`cmd/taskboard` 真实窗口，1280×800，服务 `127.0.0.1:47827`，独立库 `/tmp/tb-review/taskboard.db`
**依据**：`docs/design.md`（§1–§12）、`docs/review-brief.md`、`skill://design-review`
**性质**：只读验收，未修改任何 `.go` 与 `docs/design.md`

## 复现（本报告的全部截图来源）

```bash
go build -o /tmp/tb-review/taskboard ./cmd/taskboard
rm -f /tmp/tb-review/taskboard.db
TASKBOARD_ADDR=127.0.0.1:47827 TASKBOARD_DB=/tmp/tb-review/taskboard.db \
  /tmp/tb-review/taskboard                  # 加载态：再加 TASKBOARD_SLOW=900ms / 1200ms / 1500ms
# 明暗：osascript -e 'tell application "System Events" to tell appearance preferences to set dark mode to {true,false}'
# 截图：Quartz CGWindowListCreateImage（screencapture 本机单帧约 50s，故改用此链路）
# SSE：TASKBOARD_ADDR=127.0.0.1:47827 go run ./cmd/tb add "验收写入测试" --project local --agent rams
#      TASKBOARD_ADDR=127.0.0.1:47827 go run ./cmd/tb move <id> 进行中 --agent rams
```

截图均为 2× Retina 像素（2560×1600 ↔ 1280×800 DIP），存于 `docs/assets/review/`。

## Overall Impression

静态画面是**干净、克制、有明确体系**的：四列等宽、间距节奏与 `t.Space` 刻度严丝合缝、字号刻度有真实层级、中文字体与英文数字混排对齐良好、暗色不再是上一版的"近白卡片+浅字"故障。问题集中在**状态层**：明色悬停把卡片填色塌进列底、弹层里未提交的编辑会被外部推送静默冲掉、加载态同时出现两个板级指示器。这些不是"不够漂亮"，而是会被用户当成坏掉/丢数据的缺陷。


## 证据总览

| 明色 | 暗色 |
| --- | --- |
| ![明色看板](assets/review/window-light-board.png) | ![暗色看板](assets/review/window-dark-board.png) |
| ![明色加载态](assets/review/window-light-loading.png) | ![暗色加载态](assets/review/window-dark-loading.png) |
| ![明色空列](assets/review/window-light-empty.png) | ![暗色详情弹层](assets/review/window-dark-detail.png) |

加载态指示器与中文文案（明 / 暗）：

![加载态标签](assets/review/crop-loading-label.png)

暗色卡片悬停（39,39,42 → 50,50,54，可读）与静止对照：

![暗色悬停](assets/review/window-dark-card-hover.png)
![暗色静止](assets/review/window-dark-card-rest.png)

SSE 就地更新（CLI `add` / `move` 后窗口只重绘待办列、「进行中」列）：

![SSE 新增](assets/review/window-dark-sse-add.png)
![SSE 移动](assets/review/window-dark-sse-move.png)

## Findings

### High

**H1. 明色下卡片悬停后填色与列底完全相同，卡片看起来"漏成了洞"**
区域：看板列内卡片（任意列，静止 ↔ 悬停）。
证据：`docs/assets/review/window-light-card-rest.png`（静止）、`docs/assets/review/window-light-card-hover.png`（悬停）、局部裁切 `crop-light-rest-region.png` / `crop-light-hover-region.png`。

![明色卡片静止](assets/review/crop-light-rest-region.png)
![明色卡片悬停：卡片填色塌进列底](assets/review/crop-light-hover-region.png)
实测像素（同一坐标 300,470 处的卡片内部）：静止 `#ffffff` → 悬停 `#f4f4f5`；而列面板底色正是 `#f4f4f5`。即悬停瞬间卡片填充与列底对比度为 **1.00:1**，只剩描边变成 `#afafb3`（对比 1.99:1）。视觉结果是鼠标一进入卡片，卡片边界的那层"浮起感"消失，反而像列底被挖了一个矩形。
→ 修复：`cardHover(t)` 在明色下不能用 `t.Surface`（它与 `colFace(t)` 同值）。改走 `t.SurfaceHover`，并把悬停描边提到 `t.Border.Mix(t.Text, 0.35)` 左右；若要保持 §11"静止卡片无阴影"，最稳的是**明色卡片静止面改 `t.Surface`、悬停面改 `t.Background`**（与暗色方向互换），或给所有卡片一条比列底深一档的 1px 静止描边。注意 design.md §7 目前也把明色悬停写成 `t.Surface`，规格与实现同样需要改。

**H2. 弹层里已输入但未保存的标题，被外部代理的推送静默覆盖，随后"保存"还把弹层当成功关掉**
区域：任务详情弹层 → 标题/正文 → 收到他人更新（SSE）→ 点「保存」。
证据：`docs/assets/review/detail-title-edit-before-sse.png`（本地已键入，未保存）、`docs/assets/review/detail-title-edit-after-sse.png`（同坐标，外部 PATCH 到达后已被换成代理的标题）；整帧 `window-dark-detail.png` 为同一弹层的正常形态。

![本地已键入未保存](assets/review/detail-title-edit-before-sse.png)
![外部推送到达后：输入被覆盖](assets/review/detail-title-edit-after-sse.png)
复现：打开卡片详情 → 在标题末尾键入文字 → `curl -X PATCH /api/tasks/{id} -H 'X-Agent: rams' -d '{"version":N,"title":"被代理抢先改的标题"}'` → 标题框内容直接被替换，无 toast、无冲突弹层。
成因（代码位置）：`cmd/taskboard/board.go:467 touchDetail()` 在 `b.saving == false` 时无条件覆写 `b.detail.title/body`，没有"本地是否已改"的判断；随后 `saveDetail()` 走 `title == cur.Title` 的早退分支（`board.go:536`）直接把弹层关掉——用户的输入既丢了、又拿到了"成功关闭"的假象，且 `updatedBy` 已变成 `rams`/`mcp`。这与 design.md §8"有未提交编辑 → 弹「任务已被修改」"的契约相反（§8 只描述了 409 路径，SSE 路径没被同一原则覆盖）。
→ 修复：`detail` 增 `dirty` 标记（与打开时快照比较），`touchDetail` 在 `dirty` 时只更新卡片和"最新版本号"，不动输入框，并给弹层一行提示"该任务已被 X 更新到 vN"；`saveDetail` 在 `dirty` 且版本已前进时按 409 同一套 `AlertDialog` 处理（`使用最新版本` / `保留我的修改`）。§8 需补一行：SSE 到达时若本地有未提交编辑，走同一冲突弹层。

（注：我尝试在真实窗口里再触发纯 409 弹层，两次都被 H2 抢先：外部更新先抹平了本地编辑，保存于是被判为"无改动"静默关闭，故**没有拿到 409 AlertDialog 的窗口截图**；`go test ./cmd/taskboard -run TestConflictAligns` 通过，说明该分支代码路径本身可用。）

### Medium

**M1. 初次载入时同时存在两个"板级"指示器，违反 §6 的"同一时刻最多一个"**
区域：顶栏最右 + 看板正中。
证据：`docs/assets/review/window-light-loading.png`、`docs/assets/review/window-dark-loading.png`、`crop-loading-label.png`。
实测：正中是 §6 第 1 行的 `Arc 20 / t.TextMuted / "正在载入看板"` + 文案"正在载入看板…"（符合）；**同时**顶栏最右还有一枚 12px 的 `Arc`（`view.go:131`，Label 正在连接本地服务）在转。§6 结尾明确"同一时刻最多一个全板级指示器（1、6）"，且"连接中"这一态在 §6 映射表里根本没有条目。
→ 修复：初次载入期间顶栏同步点画静止灰点（或干脆不画），把"连接中"补进 §6 作为行内态（尺寸/颜色/中文 Label 明确写死）；或者把板级 Arc 改为顶栏单点，二者选一。

**M2. 卡片上的"保存中"指示器尺寸 10，规格要求 14**
区域：卡片右下角时间戳位置。
证据：`cmd/taskboard/view.go:377`（`LinearDots Size: 10`）对照 §6 第 3 行（Size 14）；弹层内同一指示器 `view.go:504` 才是 14。
→ 修复：两处统一 14（`Size: 14`），并把尺寸写成一个常量，避免下次再分叉。

**M3. 暗色主按钮文字对比度 3.68:1，低于 WCAG AA 的 4.5:1**
区域：顶栏「新建任务」、弹层「保存」（暗色外观）。
证据：`docs/assets/review/window-dark-board.png` 顶栏右侧蓝钮。计算：`DarkTheme.Accent #3b82f6` 上的 `AccentText #ffffff` = **3.68:1**；明色同一位置 `#2563eb`/白 = **5.17:1**（通过）。按钮标签是 13px 正文尺寸，按 AA 需要 4.5:1。
→ 修复：这是 mygo `DarkTheme` 的取值，不改框架的前提下，在应用侧复制主题并覆盖 `Accent`（暗色用 `#2563eb` 一族，或把 `AccentText` 保持白色但把填充压到 `#1d4ed8`）；这类覆盖属于 §7 "由 Theme 字段表达"的合法范围。

**M4. 明色下虚线空态框与列描边几乎看不见（1.41:1 / 1.28:1）**
区域：空列占位「还没有任务」、所有列面板外框（明色）。
证据：`docs/assets/review/window-light-empty.png`。
计算：`Border #d9d9de` 对白卡 = **1.41:1**；对列底 `#f4f4f5` = **1.28:1**。非文本 UI 边界的最低建议是 3:1，实测列框在明色下要靠"知道那里有框"才看得出；暗色同类值为 1.7:1（`#3f3f46` 对 `#18181b`），也偏弱但比明色好。
→ 修复：明色列框/空态框改用 `t.Border.Mix(t.Text, 0.12)` 一档更深的描边（约 1.5 阶），空态虚线再深一档；这不引入新色值，仍是 Theme 派生。

**M5. 列容器缺少 `Role(RoleGroup)` + 列级 Label，计数 Label 挂在徽标上**
区域：每列（列头 + 计数）。
证据：`cmd/taskboard/view.go:217-236` —— 列 `ui.Column` 未设 `Role(ui.RoleGroup)`；`"待办列，3 个任务"` 这个串落在 `ui.Badge` 的 `.Label()` 上，而列容器本身对读屏只是一个无名字的容器。
这与 §3/§10"列容器 `Role(ui.RoleGroup).Label("待办列，3 个任务")`，让读屏进列时一次报出列名 + 计数"不符（当前要 Tab 到卡片组 `FocusGroup("待办列任务")` 才听到类似信息，且不含计数）。
→ 修复：给列面板加 `Role(ui.RoleGroup).Label(fmt.Sprintf("%s列，%d 个任务", col.Name, len(tasks)))`，徽标保留 `"N 条任务"` 之类的短 Label 以免重复朗读。

**M6. 详情弹层的"评论载入中"指示器不在 §6 映射表内**
区域：弹层「评论」标题右侧。
证据：`cmd/taskboard/view.go:456`（`BouncingDots Size 12 / t.TextMuted / "正在载入评论"`）。规格表只定义了 8 行，评论载入没有条目，因此尺寸/颜色/是否与"发送评论"同形（同为 BouncingDots）都无据可依；同一弹层内 BouncingDots 会先后承担"载入评论"和"发送评论"两种语义。
→ 修复：在 §6 增行（建议 `LinearDots 12 / t.TextMuted`），与"正在发送评论"（BouncingDots / Accent）区分开，避免同形不同义。

### Low

- **L1 顶栏多了一个 §1 未描述的「新建任务」主按钮**（`view.go:110`）。§1 的顶栏规格是"刷新 + 新建项目 + 同步点"，实际是"刷新 + 新建项目 + 新建任务(primary) + 同步点"，`新建项目` 与 `新建任务` 都开 Modal、文案仅一字之差，新用户第一眼容易点错。证据：`window-light-board.png` / `window-dark-board.png` 右上角。→ 文档补齐并考虑把任务创建收敛到列内输入框（该入口已存在且很好用）。
- **L2 空态占位框比卡片矮一截**：测得占位框高约 48 DIP（`empty` 帧 x=300 竖扫 361→457 像素 @2x），卡片 `MinHeight(64)`。同一列里"占位"与"卡片"不是同一节拍。证据：`window-light-empty.png`。→ 占位框设 `MinHeight(64)`，与卡片同高，空→有内容时不跳变。
- **L3 顶栏实测约 56 DIP，规格 52**：标题栏下沿到分隔线实测 168px@2x，减去系统标题栏约 28 DIP 后为 56 DIP（含 1 DIP 分隔线仍是 4 DIP 偏差；此项受标题栏实测误差影响，标为待复核）。规格写 `Height(52)`，实现也是 52，说明多出来的部分来自 Row 的内边距或分隔线基线。→ 复核后二选一：改实现或改文档。
- **L4 悬停描边提升有限**：明色 1.99:1、暗色 2.65:1（`Border.Mix(Text,0.22)` 计算值）。与 H1 叠加后，明色悬停的"唯一"可感知变化就是这条 1.99:1 的描边。→ 与 H1 一起调。

## 逐项规格核对（docs/design.md）

| 规格 | 判定 | 证据 / 说明 |
| --- | --- | --- |
| §1 窗口 1280×800、四列等分 303、列间距 s3、看板边距 s4 | **符合** | 实测列框边界 32/637、662/1292、1292/1922、1922/2527（@2x）→ 列宽 302.5 DIP、间隙 12.5 DIP、左右边距 16 DIP |
| §1 顶栏高 52 | **存疑（L3）** | 实测约 56 DIP，含分隔线 |
| §1 顶栏内容（刷新 / 新建项目 / 同步点） | **不符合（L1）** | 多出「新建任务」主按钮 |
| §2 间距只用 `t.Space`、字号只用 f-* 刻度 | **符合** | 通读 view.go/board.go：无裸像素间距；`fxs/fsm/fmd/flg` 四个函数覆盖全部字号 |
| §2 圆角（控件 6 / 面板与弹层 10 / 徽标 999） | **符合** | 目视与代码一致；列面板 `Radius+4`、卡片 `Radius` |
| §2 描边 1 DIP、悬停 `Mix(Text,0.22)` | **符合但效果偏弱（L4）** | 计算 1.99 / 2.65 |
| §2 背景分层由 `colFace/cardFace/cardHover` + `t.Dark` 分支 | **符合** | `view.go:26-45`，仅这三处分支 |
| §3 列头 32 高、`RoleHeading.Level(2)`、徽标计数 | **符合**（Role 见 M5） | 列头文字与徽标基线对齐 |
| §3 列容器 `Role(RoleGroup)` + 列级 Label | **不符合（M5）** | 仅 `FocusGroup` 有 Label |
| §3 列内输入框 Placeholder「添加任务，回车创建」+ Label | **符合** | `view.go:264` |
| §4 卡片 MinHeight 64、标题 `MaxLines(2)+Ellipsis`、摘要两行 | **符合** | `window-dark-card-truncation.png`：长摘要在第二行末截断并带省略号 |
| §4 版本徽标 / 评论计数 Label / 代理徽标 / 相对时间 | **符合** | 「v2」「rams/mcp」「刚刚」均在卡片右下 |
| §5 悬停变色 200ms、按下 `SurfacePressed` | **不符合（H1：明色）** | 暗色悬停 39,39,42 → 50,50,54（实测，见 `window-dark-card-hover.png`）；明色填充退化 |
| §5 键盘焦点环保留默认 | **未能验证** | 自动化按键后未取得可信焦点环截图，不下结论 |
| §6 行1 初次载入 `Arc 20 / TextMuted / 正在载入看板` + 文案 | **符合** | `window-light-loading.png` / `window-dark-loading.png` |
| §6 行2 切换项目 `Ring 16 / Accent` + 旧内容 `Opacity(0.55)` | **符合** | `view.go:100-102`、`boardArea` 的 `Opacity(0.55)`；窗口内未取到时序截图 |
| §6 行3 卡片保存 `LinearDots 14 / TextMuted / 正在保存任务` | **不符合（M2，卡片侧 10）** | 弹层侧 14 正确 |
| §6 行4 发评论 `BouncingDots 12 / Accent` + 输入框 Disabled | **符合** | `view.go:483-490` |
| §6 行5 同步点：静止 8×8 圆点 `Success`、事件后 `Pulse 10 / Accent` 1.2s | **基本符合** | `view.go:118-125`；实测 900ms 脉动（`flash()`）与规格 1.2s 有 300ms 差，属可接受偏差 |
| §6 行6 离线 `Comet 12 / Warning` + 文案 + 重试按钮 | **代码符合，窗口未取到** | `view.go:136-147`；窗口内起服务即进程自身，杀服务=杀窗口，无法在窗口内制造离线；`go test -run TestOfflineBanner` PASS |
| §6 行7 归档 `Arc 14 / TextMuted` | **符合** | `view.go:500` |
| §6 "最多一个全板级指示器" | **不符合（M1）** | 载入时中心 + 顶栏两个 |
| §6/§10 同屏两个指示器 Label 不同 | **不符合（隐患）** | 弹层保存时卡片页脚 `正在保存任务`（`view.go:377`）与弹层按钮左侧 `正在保存任务`（`view.go:504`）**同时**渲染，Label 重名，违反 §10 最后一条 |
| §7 暗色卡片不再近白、文字可读 | **符合（旧缺陷已修）** | 实测卡片面 `#27272a`（39,39,42），正文 `#f4f4f5` → 12.2:1；次要文字 `#a1a1aa` → 5.81:1，均过 AA |
| §7 暗色列面 = Background、卡片 = Surface | **符合**（视觉偏平，M4 同源） | 列面与窗底同为 24,24,27，卡片 39,39,42（1.19:1），仅靠 1.7:1 描边分层 |
| §7 明色列面 = Surface、卡片 = Background | **符合** | 244,244,245 / 255,255,255 |
| §7 危险色仅文字（归档） | **符合** | 弹层「归档」为红字无填充 |
| §7 主按钮 Accent + AccentText | **符合但对比度不足（M3）** | 暗色 3.68:1 |
| §8 整板无任务：每列虚线占位「还没有任务」 | **符合**（高度差异见 L2） | `window-light-empty.png` 四列各一块 |
| §8 无项目空态 | **未能验证** | 无删除项目入口，无法构造"零项目"；代码路径 `noProjectState` 存在 |
| §8 版本冲突 409 → AlertDialog | **未能验证（被 H2 抢先）** | 见 High 段说明；单测 PASS |
| §10 卡片 `RoleButton` + `Label("打开任务：…")` | **符合** | `view.go:317-320` |
| §10 按钮可读名（刷新看板/关闭详情/保存任务/归档任务/发送评论） | **符合** | 均显式 `.Label(...)` |
| §10 输入框一律有 Label | **符合** | 任务标题/任务正文/移动到列/写评论/项目名 |
| §11 无轮询、无全表重建、SSE 就地更新 | **符合** | 全仓无 ticker 拉任务（仅 SSE 心跳）；CLI 写入实测只重绘待办列区域（差异包围盒 x 54–1155 / y 243–703，其余列逐像素相同） |
| §11 无 `ui.Hex` / `ui.RGBA` 自造色 | **符合** | `grep` 无命中 |
| §11 静止卡片/列/按钮无阴影 | **符合** | 无 `Shadow` 调用（除框架 Modal） |

## What Looks Good

- **间距与字号是一套真体系**：列内 `Padding(s2.5)/Gap(s2)`、卡片 `Padding(s3)/Gap(s1.5)`、列头 32 高，肉眼能看到 4 的倍数在起作用；四列宽度实测 302.5/12.5 DIP 与 303/12 的规格误差在 1px 内。
- **暗色不再是"白卡+浅字"**：卡片面 `#27272a`、正文对比 12.2:1，上一轮的 High 缺陷确认修复；`TestAgentUpdateFlashesCard` 也已从超时变为 0.71s 通过。
- **层级靠明度而非花招**：没有渐变、没有阴影、没有装饰性大写标签，卡片标题 500 字重 + 摘要 muted 的两级信息密度正确，长摘要两行 + 省略号（`window-dark-card-truncation.png`）表现干净。
- **空态与文案到位**：四列各自"还没有任务"，中文全链路（占位、按钮、Label、Toast）没有英文漏字；相对时间「刚刚」与代理名并排时右对齐稳定。
- **SSE 就地更新是真的就地**：`tb add` 后窗口只重绘待办列（像素级验证），无闪烁、无整板重建；`tb move` 后卡片出现在「进行中」。这正是 §11 想要的效果。
- **弹层结构清晰**：字段标签在上、控件在下，`关闭` 在右上、`归档`（红字）在左、`保存`（主按钮）在右，阅读与操作动线符合习惯。

## Top 3 Fixes

1. **重做明暗两套的"静止 / 悬停"面关系**（H1 + L4 + M4）：明色悬停面不能等于列面（当前 1.00:1）。建议方向——明色：卡片 `t.Surface`、悬停 `t.Background`＋更深描边；暗色：保持现状但把列框提到 3:1。design.md §7 表格同步改。
2. **保护弹层里未提交的编辑**（H2）：`dirty` 判定 + SSE 到达时不覆写输入框 + 保存时走与 409 同一套冲突弹层；§8 增补 SSE 路径。
3. **收敛加载指示器**（M1 + M2 + M6 + 同屏 Label 重名）：一屏只留一个板级指示器；卡片/弹层保存统一 14；"载入评论"补进 §6 并换形；同屏保存时不重复渲染两枚同名指示器。

## 结论

**CONDITIONAL PASS（有条件通过）**

- 通过项：布局网格与间距体系、字号刻度、圆角描边一致性、暗色可读性（旧缺陷已修）、空态、SSE 就地更新无轮询、无障碍 Label 覆盖主体、无自造色/无 WebView 外壳。
- 条件项（必须修完再视为发布级）：**H1 明色悬停使卡片与列底融为一片**、**H2 未提交编辑被推送静默覆盖**；以及 M1/M2/M5/M3 四项规格偏差。
- 建议在修完后重跑：真实窗口明色悬停对比、弹层"边输入边被推送"场景、`TASKBOARD_SLOW=900ms` 首屏指示器计数。

## 未能覆盖的部分（如实记录）

- **离线横幅**：窗口进程内即服务进程，无法在窗口存活时切断 `/api/events`；只有 `TestOfflineBanner` 通过作为证据。
- **409 AlertDialog**：两次真实尝试均被 H2 的"静默覆盖 → 判定无改动 → 关闭"吃掉，未截到窗口图；仅单测 `TestConflictAligns` PASS。
- **无项目空态**：产品无删除项目入口，构造不出零项目。
- **键盘焦点环**：自动化按键（CGEvent）未能取得可判读的焦点环截图，未下结论。
- `screencapture -R/-l` 在本机单帧耗时约 50s（疑似触发了权限扫描），故全部窗口截图改用 Quartz `CGWindowListCreateImage` 抓取（同一窗口 ID `81912`/`81957`/`81966`/`81977`/`81986`，内容与 `screencapture` 一致）。

---

## 第二轮复验（2026-10-24）

**对象**：修复后的 `cmd/taskboard`，真实窗口 1280×800，服务 `127.0.0.1:47832`，独立库 `/tmp/tb-r2/taskboard.db`
**性质**：只读复验，未修改任何 `.go` 与 `docs/design.md`；下文所有数值来自真实窗口像素或应用自身的 `ui.Tester` 渲染帧
**基线**：`go build ./... && go vet ./... && go test ./...` 全绿（`cmd/taskboard` 1.441s，其余 cached / ok）

### 复现

```bash
go build -o /tmp/tb-r2/taskboard ./cmd/taskboard
TASKBOARD_ADDR=127.0.0.1:47832 TASKBOARD_DB=/tmp/tb-r2/taskboard.db \
  /tmp/tb-r2/taskboard                       # 加载态另加 TASKBOARD_SLOW=1200ms / 2500ms
# H2：curl -X PATCH .../api/tasks/<id> -H 'X-Agent: rams' -d '{"version":N,"title":"被代理抢先改的标题",...}'
```

截图取自 Quartz `CGWindowListCreateImage`（2× Retina：2560×1600 ↔ 1280×800 DIP），新图统一前缀 `r2-`。

### 逐条判定

| 编号 | 原问题 | 判定 | 第二轮实测 |
| --- | --- | --- | --- |
| H1 | 明色悬停填色与列底同色（1.00:1），卡片塌成洞 | **已修复** | 悬停面实测 `#e9e9ec`，列面 `#f4f4f5`：**1.102:1**（方向正确：悬停比列面深）；悬停面 vs 静止面 `#ffffff` = **1.212:1**。窗口截图见 `r2-light-card-rest.png` / `r2-light-card-hover.png`。⚠ 见「仍存疑」第 1 条 |
| H2 | 未提交编辑被 SSE 静默覆盖；保存还静默关闭 | **部分修复** | 保留输入框 + 黄色提示行 **已修复**（实测见下）；但 `saveDetail` 在真实板形下**不走冲突弹层**，直接 PATCH 成功关闭 → 见「仍存疑」第 2 条 |
| M1 | 载入时两个板级指示器 | **已修复** | `TASKBOARD_SLOW=1200ms` 首屏：中心 `Arc 20` 独占，顶栏最右是**静止 8×8 `#d9d9de` 圆点**（实测 bbox 18×18 设备像素 = 9×9 DIP，含 0.5 DIP 抗锯齿；`#d9d9de` 占 164 px 单一色，跨帧像素签名不变），无一枚转圈。见 `r2-loading.png`、`r2-loading-dot-crop.png` |
| M2 | 卡片保存指示器 10，规格 14 | **已修复** | 帧差分（指示器出现前后）得 ink bbox **高 13 DIP × 宽 37 DIP**，与 `LinearDots Size 14` 的圆点直径 14 相符（首轮为 10）。见 `r2-card-saving.png` |
| M3 | 暗色主按钮白字 3.68:1 | **已修复** | 暗色帧里主按钮填充实测 `#2563eb`（1952 px），白字 **5.169:1**，过 AA；原 `#3b82f6` = 3.678:1。见 `r2-dark-detail-modal.png` |
| M4 | 明色列框 1.28:1、空态虚线 1.41:1 | **已修复** | 列框实测 `#8c8c90` vs 列面 `#f4f4f5` = **3.048:1**；空态虚线实测 `#828285` vs 列面 = **3.482:1**；暗色列框 = 4.956:1。三处均过 3:1 非文本边界线。见 `r2-light-empty-crop.png` |
| M5 | 列容器缺 `Role(RoleGroup)` + 列级 Label | **已修复（但引入重复朗读）** | `view.go:252-253` 列面板已带 `Role(ui.RoleGroup).Label("待办列，2 个任务")`；但 `view.go:267` 的徽标 **同时** 挂了完全相同的 Label，读屏进列时会听到两遍「待办列，2 个任务」（首轮建议「徽标保留短 Label」未被采纳）。见「仍存疑」第 3 条 |
| M6 | 评论载入指示器不在 §6 表内 | **已修复** | 代码为 `LinearDots 12 / t.TextMuted / "正在载入评论"`（`view.go:495`），与「正在发送评论」的 `BouncingDots 12 / Accent` 分形分色；§6 已增行 7b。见 `r2-detail-dirty-pushed.png` 同款弹层结构 |
| L1 | 顶栏多「新建任务」主按钮 | **已修复** | 顶栏文本序列实测为 `[项目切换 local 刷新看板 刷新 新建项目 实时同步已连接 …]`，无「新建任务」；暗色整板蓝色填充像素数 **0**（无主按钮残留）。见 `r2-window-light-board.png` / `r2-window-dark-board.png` |
| L2 | 空态占位框比卡片矮 | **已修复** | 占位虚线框高 **94 DIP**（含外扩测量带），卡片框高 **158 DIP 的同一测量带内**；两者 `MinHeight(64)` 一致，占位框不再矮一截。见 `r2-light-empty-crop.png` |
| L4 | 悬停描边提升有限 | **已修复（数值）/ 仍偏弱（观感）** | 悬停描边实测 `#95959a`，对悬停面 `#e9e9ec` = **2.461:1**、对列面 = 2.712:1（首轮 1.99:1）。提升真实存在，但仍低于 3:1 |
| L3 | 顶栏 56 vs 52 | **误报，已澄清** | `ui.Tester` 帧内量得顶栏到第一条分隔线 **52 DIP**，与规格一致；首轮 56 是系统标题栏计入的测量误差 |

### H2 两次窗口截图（本轮重点）

**① 键入后被推送：输入仍在 + 黄色提示行**

- `docs/assets/review/r2-h2-typed.png` —— 打开「欢迎使用 taskboard」详情，在标题里键入「（验收补丁）」，此时未保存。
- `docs/assets/review/r2-h2-pushed.png` —— `curl -X PATCH … -H 'X-Agent: rams' -d '{"version":1,"title":"被代理抢先改的标题",…}'` 返回 `HTTP 200`、服务端到 v2。窗口内：
  - 输入框内容仍是 **「欢迎使用 taskboard（验收补丁）」**，未被覆盖（首轮此处被抹成代理的标题）；
  - 弹层顶部出现一行 **「该任务已被 rams 更新到 v2，你的编辑尚未保存。」**；
  - 背后卡片已变成「被代理抢先改的标题 v2」，右下角代理徽标 `rams`。
- 结论：**H2 的「不覆盖 + 提示」这一半确实修好了**，测试 `TestDirtyDetailSurvivesPush` 也覆盖了同一路径。

**② 点「保存」后：没有出现「任务已被修改」AlertDialog**

- `docs/assets/review/r2-h2-after-save.png` —— 点「保存」后，弹层直接关闭，卡片是「欢迎使用 taskboard（验收补丁）**v3**」，代理徽标变成 `gui`（本窗口身份），也就是**用户的编辑被直接写成功、没有任何冲突提示**。
- 这与 design.md §8 新增的「点保存时按 409 同一套 AlertDialog 处理」不符。成因见下条。

### 仍存疑 / 未通过（3 项）

**第 1 条（新发现，Medium）：明色悬停的方向与「更深一档」的观感目标仍偏弱**

修复把明色悬停设成 `SurfaceHover #e9e9ec`，方向正确（比列面深），但 1.102:1 的差值在 2× Retina 上只是极细微的灰阶变化；真正的可见线索仍主要来自描边的 2.461:1。`TestHoverFaceDiffersFromColumn` 只断言「不等于」，没有断言最小差值。若目标是「一进卡片就有明确的浮起感」，建议把明色悬停再压一档（如 `SurfacePressed` 或 `Border.Mix` 派生的中间色），或把悬停描边提到 3:1。**此为观感判据，非对照契约的硬缺陷**，故 H1 仍判「已修复」。

**第 2 条（High，H2 的另一半）：保存路径在真实板形下绕过冲突弹层**

`saveDetail`（`board.go:554-575`）的冲突分支条件是 **`title == cur.Title`**，即「用户没改标题时才承认冲突」。真实板形下（`rams` 的推送 = 服务端当前版本），用户改过的标题必然 `≠ cur.Title`，于是走到常规 PATCH 分支，用 **推送后的 `cur.Version`** 提交，服务端接受 → 编辑覆盖代理的改动，弹层正常关闭。

我用 `/tmp` 下的仓库副本加日志探针验证（未改仓库代码）：
```
SAVE branch-check: title="欢迎使用 taskboard（验收补丁）" cur.Title="被代理抢先改的标题" body_eq=true dirty=true curVer=2 snapVer=1
SAVE sent with version=2 -> err=<nil>
SAVE success: detail.open=false
```
即 §8 要求的「dirty + 版本已前进 → AlertDialog」在**用户确实改了文本**时永远不触发；它只在「dirty 但内容恰好等于新版」这种自相矛盾的条件下触发。

`TestDirtyDetailSurvivesPush` 之所以 PASS，是因为它的 fake 把版本推到 8 而服务端权威版本仍是 1，保存必然 409，走的是 `recover()` 的弹层 —— 掩盖了上面这条分支。`r2-conflict-dialog.png`（`ui.Tester` 帧）能渲染出「任务已被修改」弹层，正是靠这个 fake 版本错位，真实窗口复现不出。

→ 建议：把条件改为 `d.dirty() && cur.Version > d.snap.Version`（与「本地是否有未提交编辑」正交），并让 fake 的 push 与权威版本保持一致，否则回归测试继续掩盖该路径。

**第 3 条（Low，M5 的副作用）：列名 + 计数被朗读两次**

`view.go:252-253` 列面板与 `view.go:267` 徽标挂了同一个字符串 `"待办列，2 个任务"`，违反 §10「同一屏内两个指示器的 Label 不得相同」的同类原则（一个列组里两个同名节点）。`ui.Tester` 的文本序列实测该串在每列出现 **两次**：`… 待办列，2 个任务 待办 待办列，2 个任务 2 …`。→ 徽标保留短 Label（如「2 条任务」）即可。

### 新截图清单（14 张）

真实窗口（Quartz）：
- `r2-window-light-board.png`、`r2-window-dark-board.png` —— 明/暗整板，顶栏无「新建任务」
- `r2-light-card-rest.png`、`r2-light-card-hover.png` —— H1 静止/悬停对照
- `r2-light-empty-crop.png` —— 明色空态虚线框与列框
- `r2-loading.png`、`r2-loading-dot-crop.png` —— M1 单指示器（顶栏静止圆点）
- `r2-h2-typed.png`、`r2-h2-pushed.png`、`r2-h2-after-save.png` —— H2 三段

应用渲染帧（与窗口同一套 `ui.Tester` 渲染管线）：
- `r2-dark-detail-modal.png` —— M3 暗色主按钮 `#2563eb`
- `r2-card-saving.png` —— M2 保存指示器
- `r2-detail-dirty-pushed.png` —— 弹层提示行形态
- `r2-conflict-dialog.png` —— 「任务已被修改」弹层（仅在 fake 版本错位下可渲染，见第 2 条）

### 第二轮结论

**仍 CONDITIONAL PASS**

- **已修复（10/12 报告项）**：M1、M2、M3、M4、M6、L1、L2、L4，以及 H1（数值方向正确）、M5（主诉求达成）。
- **未通过 1 项 High**：H2 的「保存 → 冲突弹层」在真实板形下不触发，未提交编辑会被直接写成功、覆盖代理的改动（§8 契约仍未兑现）。
- **未通过 1 项 Low**：M5 引入的列 Label 重复朗读。
- **两项观感/证据待跟进**：明色悬停 1.102:1 偏弱；`TestHoverFaceDiffersFromColumn` 只断言不等、不断言最小差值，`TestDirtyDetailSurvivesPush` 的 fake 版本错位会继续掩盖第 2 条。
- 若把第 2 条修好（条件改为「dirty 且版本前进」并同步 fake），本报告可升为 **PASS**；第 1、3 条属建议级。

### 本轮的限制（如实记录）

- 复验进行中本机**锁屏**（`CGSSessionScreenIsLocked=true`），锁屏期间 `CGEvent`/`CGEventPostToPid` 均不再送达窗口，故 H2 的第二次点击之后的交互无法再在真实窗口里重放；`r2-h2-*` 三张图为锁屏前取得，`r2-dark-detail-modal.png` / `r2-card-saving.png` / `r2-conflict-dialog.png` / `r2-detail-dirty-pushed.png` 改用应用自身的 `ui.Tester` 渲染帧（与 `docs/assets/render/` 同源管线），并在正文标注了来源。
- 首轮遗留的「离线横幅」「无项目空态」「键盘焦点环」仍未能取到窗口级证据，本轮未重复尝试。

---

## 复验三：视觉升级（award 级逐维体检）

**日期**：2026-10-24（第 2 轮，接 H1/H2/M1 修复之后）
**范围**：仅视觉与交互层，`cmd/taskboard/view.go`；写入路径、SSE、冲突逻辑未动
**渲染**：`docs/assets/render/`（测试内落定动画后截图：`board-light/dark/empty/loading`）

### 逐维结论

| 维度 | 改动 | 证据 |
| --- | --- | --- |
| typography | 列头升 f-md/600，与卡标题（f-base/500）、出处行（f-xs）拉开三级；顶栏项目名 600 | board-light.png 列头/卡标题对比 |
| whitespace | 板边距 16→20、列距 12→16、列内 10→12、卡内 12→14、内部行距 6→8、列头 32→36 | board-light.png |
| visual_hierarchy | 「新建项目」升 PrimaryButton（accent 唯一落点）；列计数灰 pill → 安静数字；版本 pill 并入出处行；pill 只留评论计数 | board-light/dark.png |
| color | 列面板去 1px 描边，明 Surface/暗 Background 纯色域分层；暗色 accent 保持 AA 蓝；同步状态词（已同步/同步中/连接中/重连中）+ 离线 Warning 色 | board-dark.png 顶栏 |
| motion | 新增整板入场：列按 index×40ms 逐列上升淡入（280ms EaseOut），切项目重放；保留卡 enter/exit、跨列 Snappy 弹簧、flash 高亮 | view.go columnView Transition |
| micro_interaction | 拖放从「列描边变 Accent」升级为实时插入线（2px Accent 圆角条，`insertIndex` 按指针 Y 落位，空列/列尾也画）；初次载入期间同步点退化中性圆点且不出状态词（不预支「已同步」） | TestInsertIndexSlots；loading-light.png |
| responsive | 列宽 clamp 240..400；宽窗口列宽到顶后整板居中（左 Spacer lead/2）；960 最小窗仍横滚 | boardArea lead 计算 |
| originality | 卡片出处行「v3 · ive · 09:30」（版本+代理署名+时间合一行低语文本）——「谁在动这块板」成为常驻身份；空列占位改两行安静提示（去虚线框） | board-light.png 进行中列 |

### 验证

- `go build ./... && go vet ./... && go test ./...` 全绿（含新增 `TestInsertIndexSlots`）。
- 渲染测试新增 `settle()`：截图前推进 700ms，保证四张渲染图是落定态而非入场动画中途。
- 规格文档 `docs/design.md` §1/§2/§3/§4/§5/§6/§7/§8/§9 已同步本表全部数值。

---

## 复验四：详情编辑与边界状态（2026-10-25）

完整八维检查、发现与证据见 [本轮设计验收](../.jez/artifacts/design-review.md)。

- 详情内容区 380→520 DIP；单一内容滚动区，顶部关闭、底部评论输入及保存固定可见；最小 960×560 和长评论下检查控制按钮边界。
- 保存是详情唯一主按钮；删除重复可见字段名，保留输入占位和读屏 Label；新建项目作同类调整。
- 评论的回车发送先经键盘测试复现失败，再接通；发送中防重入，失败恢复草稿。保存也增加进行中防重入。
- 卡标题与出处行可查看完整 Tooltip，长代理名和作者名单行省略；空列指引恢复正常次级字号与不透明主题色。
- 暗色修正不再覆盖高对比主题，回归检查焦点环保持不透明。
- 新增明暗详情、最小详情、长评论最小详情、960 宽长文本看板和 1800 宽看板共六张截图。
- `go test ./...`、`go build ./...`、`go vet ./...`、`git diff --check` 通过。

团队执行边界：本会话无 Codex App 后台任务接口，因此按路由 Skill 回退到主 Agent 本地执行；没有创建真实 teammate。乔布斯 / Linus / Dieter Rams 是审查视角，不是独立模型意见。

结论边界：本轮是有截图与行为测试支持的定向优化，不重复此前“全部维度无剩余改进”的过度结论。尚无本轮真实窗口动效录屏和独立奖项级评审；响应式范围为原生桌面最小窗口以上。

## 复验五：连续新增与并行动画（2026-10-25）

用户要求：API/CLI 驱动，连续创建三个卡片、持续状态流转、带节奏的并行移动；状态截图拼接不足以验收流畅性。

已复现：原匹配移动仍在目标列 Scroll 内绘制，第一移动帧的可见范围为 W=0/H=0，卡片经过列间时被裁掉。回归测试 `TestMovingCardStaysVisibleAcrossColumns` 修复前失败。

处理：上层 Overlay 绘制移动卡片，目标列透明占位保留布局；原卡与上层卡共享 task ID 匹配，落位后交回列内。420ms、Bounce 0.04 的弹簧承接位置变化；新卡从 8 DIP 下方淡入和展开。移除源卡 Exit 副本，防止重影。减少动态效果偏好下不创建移动浮层。

验证：逐帧检查跨列卡片可见、三张并行移动、中途再次改状态不跳位、结束后仅一个可见副本、减少动态效果偏好。`go test ./...`、`go vet ./...`、签名验证与 `git diff --check` 通过，更新 `.app` 和 `.dmg`。

真实 API 场景见 `scripts/demo-motion.py`：180ms 间隔三卡新增，120ms 错峰跨列；另有两卡同时返回不同列、再次完成。请求实测时间写入 `docs/assets/demo/motion-events.json`，所有最终状态经 GET 回读。

连续实窗录屏：13.03 秒，2560×1600，平均约 56fps；转换为 1280×800 / 30fps 的 `docs/assets/demo/api-three-cards.gif` 交付，未补间、未把状态截图当作原生过渡。原始录屏保存在本机 `/tmp/taskboard-motion-capture/api-three-cards.mov`。演示期间原有任务也发生了外部更新，未修改或清理这些任务。

场景时序属于演示脚本，未给生产 SSE 队列添加人工延迟。

## 后续裁决：动效先在 Playground 验收（2026-10-25）

用户要求独立原生 Playground 展示多种候选，验收后才合入；并明确不要用手机录屏代替可操作的原型。

已将复验五的实验动效隔离至 `motionplayground` 构建；普通构建恢复实验前的 200ms EaseOut 与原卡片进入/退出行为。相关裁切修复也暂留实验，不把未批准的效果作为正式方案发布。

独立 App 的三种可切换候选、相同 API/SSE 场景、临时数据库和验收重点见 [playground/README.md](../playground/README.md)。未选定方案、未合入生产动效。

### Playground 拖放接续修复（2026-10-25）

用户偏好 A，但指出拖放释放后起点错误。鼠标真实按下、移动、释放的 headless 回归先复现：释放首帧返回旧位置 `(32,198)`，拖动副本应在 `(689,447)`。修复后不仅验证首帧，还逐帧约束到目标落位的路径，防止首帧正确、下一帧再次跳回。

原因是 API 匹配移动读取了旧列布局位置，未交接拖动副本的释放位置和抓取偏移；同时列内指针 Y 被误当作窗口坐标，透明占位也参与动画，乐观排序未预判实际落位。

仅在 Playground 记录抓取偏移和释放矩形，用独立坐标动画接向静止目标占位；拖动落位不加错峰延迟；转换落点坐标并预演目标列顺序，使 API 确认前后落位一致。覆盖跨列、反向、同列重排、空列；原 API 并行移动、连续改目标和减少动态效果检查继续通过。A 是默认候选，仍未合入正式动效。

### Playground 默认方案与源卡显示（2026-10-25）

最新用户指定默认方案为 B · 柔和弹簧。针对拖起后原位仍有卡片的问题，已将 Playground 的源卡不透明度从 0.4 改为 0，只保留布局占位；MyGo 的指针预览独立绘制，不受源卡不透明度影响。像素回归先复现源位置仍有卡面，修复后检查原位为列底色、预览仍可见、Escape 取消后原卡恢复。正式构建保持旧规则，等待用户验收后合入。

### Playground 拖入边框不改变布局（2026-10-25）

复现悬停蓝框使列头与输入框的 X/Y 增加 2 DIP、输入宽度减少 4 DIP；原因是仅在 DragOver 时增加占据布局空间的 Border。Playground 改为常驻 2 DIP 透明边框并平衡 Padding，悬停仅改变 BorderColor。有卡片与空列的几何回归通过；API 并行动效、拖动隐藏源卡、释放起点和路径检查继续通过。仍只在实验构建启用。


### 2026-10-25 · Playground 动态避让纠正（待用户验收）

上一轮隐藏源卡仍保留布局高度，造成原列空洞，不能视为完成。现在源卡只作为框架拖动预览的隐藏载体，脱离文档流；原列邻卡平滑收拢。目标列以源卡实际高度的无文字淡色占位块预览落位，邻卡随预览让位；空列也显示占位块。几何命中基于无占位的自然顺序，避免占位推动卡片后反复切换插入点。同列和跨列释放都提交显示的预览槽位，取消从当前指针位置回弹。蓝框仍预留固定边宽，不改变列头和输入框几何。

验证：原列首卡/中间卡移走后的收拢、不同高度卡片的等高占位、空列及有卡列悬停稳定、悬停不写状态、取消恢复、同列/跨列落位与占位一致、释放和取消第一帧保留抓取偏移；既有 API 并行移动、中途改道、减少动效和蓝框回归通过。正式构建仍走原来的渲染钩子；实验未合入正式动效。

## 修复补记（2026-10-25，orchestrator）

第二轮留下的 **H2 保存路径（High）** 与 **M5 重复朗读（Low）** 已修，复验者未再跑窗口（当时本机锁屏，CGEvent 无法送达），以下为代码 + 自动化证据：

- **H2**：`saveDetail` 改为先判 `d.stale()`（`dirty()` 且弹层打开后有推送到达），命中即弹「任务已被修改」的 AlertDialog，不再依赖「文本恰好等于服务器当前值」这一永不成立的条件；选「使用最新版本」时同步清空本地编辑与 `pushed` 标记。
  回归测试 `TestDirtyDetailSurvivesPush` 已改为通过 `fake.external()` **同时更新权威副本**（复现真实服务的行为）——修复前该用例会静默覆盖并关闭弹层，现在断言必须出现冲突弹窗且弹层保持打开。
- **M5**：列容器 Label 改为 `"<列名>列"`，计数由列头数字的 `Label("N 个任务")` 承载，两处不再重复同一句；`docs/design.md` §3/§10 同步。

验证：`go build ./... && go vet ./... && go test ./...` 全绿；`TestDirtyDetailSurvivesPush`、`TestConflictAligns`、`TestHoverFaceDiffersFromColumn`、`TestDarkAccentMeetsContrast` 均 PASS。
