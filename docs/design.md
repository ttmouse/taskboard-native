# 视觉与交互规格（MyGo 原生看板 v0）

范围：`cmd/taskboard` 单窗口原生看板。所有度量可写进代码；所有颜色只能来自 `ui.Theme` 字段，
所有控件必须是 `mygo/ui` 与 `plugins/loading` 里真实存在的构造函数（见文末核对清单）。
文案全中文。术语：`t := c.Theme()`，`s(n) := t.Space(n)`（Spacing=4，即 s(1)=4）；`s1`…`s8` 是间距 token 名。

总则：**让布局自己说话**（avoid unnecessary secondary labels and descriptions — let the layout do the explaining）。
层级、归属与含义优先由位置、间距、字号/字重和颜色 token 表达，不靠解释性的次要标签与说明文字；
读屏 `Label` 是元数据、不渲染，不受此限。可执行禁令见 §12 第 15、16 条，本文件后续修订与本文件约束的一切改动均须遵守。

## 1. 窗口与布局网格

| 项 | 值 | 实现 |
| --- | --- | --- |
| 窗口 | 1280×800，最小 960×560 | `mygo.NewWindow(WindowOptions{Width:1280, Height:800, MinWidth:960, MinHeight:560, Content: ui.View(a.view)})` |
| 标题栏 | 系统标题栏（默认样式） | 不设 `TitleBarStyle`；视图自绘的顶栏不承担拖窗 |
| 顶栏高 | 52 | `ui.Row(c).Height(52).Padding(0, t.Space(4)).AlignItems(ui.Center)` + 下方 `ui.Divider(c)` |
| 看板边距 | s5 | `ui.ScrollHorizontal(c).Padding(t.Space(5))` |
| 列间距 | s1 | 列间 `Gap(t.Space(1))`（无描边后不再留 s2/s3 边框空档） |
| 列宽 | 等分，最小 240，最大 400 | `ui.Column(c).Width(w)`；宽窗口列宽到顶后整板居中，不左悬 |
| 列内边距 / 卡片间距 | s2 / s2 | `Padding(t.Space(2)).Gap(t.Space(2))` |
| 卡片内边距 / 内部间距 | s3.5 / s2 | `Padding(t.Space(3.5)).Gap(t.Space(2))` |
| 列头高 | 36 | `ui.Row(c).Height(36).AlignItems(ui.Center).Gap(t.Space(2))` |
| 列尾留白 | s6 | 卡片列表底 `Padding(0, 0, t.Space(6), 0)` |

- 1280 窗口四列各 298（1280 − 2×20 − 3×16 = 1192）；960 窗口降到 240 以下即触发整板横向滚动：
  外层 `ui.ScrollHorizontal(c).Grow(1)` 管横向，列内 `ui.Scroll(c).Grow(1)` 管纵向，两层各管一个方向。
- 列数由 `model.Project.Columns` 决定，三到四列是正常形态，**不写死 4**；超过 5 列横向滚动。
- 顶栏内容：左当前项目名文本（无下拉），切换中显示 `loading.Ring`；左侧常驻项目侧栏点选切换；中 `ui.Spacer`，
  右 `ui.Button("刷新")` + `ui.PrimaryButton("新建项目")`，最右同步点 + 同步状态词（见 §7 第 5 行）。
- 项目侧栏：常驻左侧安静切换器，规格见 §2；新建仍只在顶栏 `PrimaryButton`。
- 有侧栏时看板左内边距用 s2（右/上下仍 s5），列宽按「窗口宽 − 侧栏宽 − 左右内边距 − 列间距」计算并居中，避免侧栏+整窗边距叠出横向滚动条。

## 2. 项目侧栏 anatomy

安静的项目切换器，不是第二套看板。列面板仍是唯一的 L1 色面；侧栏贴 L0，避免与列同色糊成一块。

```
Column(width t.Space(44), FillHeight, padding s3, gap s2, bg t.Background, 无描边无外圆角)
├─ Text("项目", f-sm, TextMuted, 600)                         ← 区头，无次要说明
└─ Scroll(grow 1) → Column(FillWidth, gap s1)
   └─ 行…（项目项）
```

行（每项一个可点 `Button`，必须去铬边）：

```
Button(key="project-"+id, name).FillWidth().Radius(t.Radius)
  .Padding(s1.5, s2).BorderWidth(0)
  .FontSize(f-base).TextColor(t.Text)
  .Label("切换到项目 "+name)
```

| 态 | 底 | 字重 |
| --- | --- | --- |
| 未选 | `ui.Transparent` | 500 |
| 悬停 | `t.SurfaceHover` | 500 |
| 选中 | `t.Accent.Alpha(0.14)` | 600 |
| 选中+按下 | `t.Accent.Alpha(0.18)` | 600 |
| 未选+按下 | `t.SurfacePressed` | 500 |

- 宽 `t.Space(44)`（Spacing=4 时 176）：够放中文项目名，不挤占列区。
- 容器背景必须是 L0 `t.Background`，**禁止** `colFace(t)`（那是列面板 L1）。
- 容器无描边、无外圆角（贴窗左缘的全高条）；描边只留给卡片与控件。
- 禁止实心 Accent 块、禁止左侧竖条装饰、禁止渐变与自造 Hex。
- 新建入口不在侧栏：只在顶栏 `ui.PrimaryButton("新建项目")`。
- 读屏：容器 `Label("项目列表")`；行 `Label("切换到项目 "+name)`；选中只靠视觉（淡 Accent + 字重），不另加可见「当前」标签。
- 无项目时不渲染侧栏（空态走 §9）。

## 3. Tokens

间距只用 Theme 的 Spacing 刻度（`t.Space(n)`），禁止裸数字。

| Token | 值 | 用途 |
| --- | --- | --- |
| s1 | `t.Space(1)` = 4 | 徽标内边、图标与文字、焦点环间距 |
| s1.5 | `t.Space(1.5)` = 6 | 卡片内部行距、列头元素间距 |
| s2 | `t.Space(2)` = 8 | 卡片间距、列内卡片间 |
| s2.5 | `t.Space(2.5)` = 10 | 列面板内边距 |
| s3 | `t.Space(3)` = 12 | 卡片内边距、列间距、弹层内边距 |
| s4 | `t.Space(4)` = 16 | 列间距、顶栏左右边距 |
| s5 | `t.Space(5)` = 20 | 看板边距 |
| s6 | `t.Space(6)` = 24 | 列尾留白 |
| s8 | `t.Space(8)` = 32 | 空态上下留白、弹层大间距 |

字号：以 `t.FontSize`（macOS 13 / Windows 14）为基准的相对刻度，写 `.FontSize(...)` 数值。

| Token | 值 | 用途 |
| --- | --- | --- |
| f-xs | `t.FontSize-2` | 徽标数字、版本号、评论计数 |
| f-sm | `t.FontSize-1` | 卡片摘要、时间、占位文案、错误说明 |
| f-base | `t.FontSize` | 正文、卡片标题（+`FontWeight(500)`）、按钮、输入框、列名（+`FontWeight(600)`） |
| f-md | `t.FontSize+2` | 列名（+`FontWeight(600)`）、弹层标题、顶栏项目名（+`FontWeight(600)`） |
| f-lg | `t.FontSize+7` | 空态标题、初次加载标题（+`FontWeight(600)`） |

圆角：按钮/输入框 = `t.Radius`（6）；卡片 = `t.Radius+2`（8）；列面板/弹层面板 = `t.Radius+4`（10）；徽标用 `ui.Badge` 自带 999。

描边：一律 1 DIP `t.Border`；列面板不描边（两套外观都靠色阶成列，描边只在卡片上）；悬停态卡片 `t.Border.Mix(t.Text, 0.22)`；
拖动悬停列 `2` DIP `t.Accent`；不使用渐变色、不使用 `ui.Hex`/`ui.RGBA` 自造颜色（唯一例外：拖动浮层阴影 `RGBA(0,0,0,0.18)`）。

背景层级（每个角色只从 Theme 的相邻两档取一档）：

| 层 | 角色 | 明 | 暗 |
| --- | --- | --- | --- |
| L0 | 窗口、看板底、顶栏、项目侧栏容器 | `t.Background` | `t.Background` |
| L1 | 列面板（无描边，靠色阶成列） | `t.Surface` | `t.Background`（与底同档，卡片亮一档成列） |
| L2 | 卡片静止 | `t.Background` | `t.Surface` |
| L2h | 卡片悬停 | `t.SurfaceHover` | `t.SurfaceHover` |
| L2p | 卡片按下 | `t.SurfacePressed` | `t.SurfacePressed` |

写成三个取值函数并由 `t.Dark` 分支，避免明暗两套写死的常量：`colFace(t)`、`cardFace(t)`、`cardHover(t)`。
弹层（`ui.Modal`）用框架自带外观；遮罩、阴影由 Modal 负责，不另加。

## 4. 列 anatomy

```
Column(padding t.Space(2), gap t.Space(2), radius t.Radius+4, bg colFace(t)，无描边)
├─ Row(height 36): Text(列名, f-md, 600).Role(ui.RoleHeading).Level(2)
│                 + Text(计数, f-sm, TextMuted).Label("3 个任务")            ← 安静数字，不用徽标
├─ TextInput(草稿).Placeholder("添加任务，回车创建").Label("在「待办」中添加任务")
└─ Scroll(grow 1) → Column(gap t.Space(2), padding bottom t.Space(6))
   └─ 卡片…（以及空态占位，见 §9）
```

- 列面板不再画描边：明色下 Surface 色块浮在 Background 上、暗色下卡片亮一档成列，
  分层靠色阶本身；描边只留在卡片与控件上，板面只有四块安静的色域。
- 列入场动效：每列 `ElementTransition{Enter: {Y: s3, Opacity: 0}, Duration: 280ms, Ease: EaseOut}`，
  `Animation.Delay = index × 40ms` 逐列错位，首次载入与切换项目时整板读作一次入场。
- 列整体可拖放：`ui.DragOver[cardDrag](panel)` 取到被拖卡片后，以源卡实际高度的淡色、无文字虚影预览落位；列外观保持不变。
  指针转换为窗口坐标，插入位置按排除源卡和虚影后的自然布局计算（指针越过邻卡上边缘即让位，不等中线），避免邻卡让位后落点抖动；
  `ui.Drop[cardDrag](panel)` 提交预览槽位，取消恢复原顺序。
- 列头用 `Role(ui.RoleHeading).Level(2)`，名称就是列名文字本身（RoleHeading 以文字为名，不要再设 Label 覆盖）；
  列容器本身 `Role(ui.RoleGroup).Label("待办列")`，计数由列头数字（`Label("3 个任务")`）报出——
  两处 Label 不重复同一句，避免读屏连续朗读两遍「待办列，3 个任务」。
- 列名不加全大写、不加字距，保持 sentence case 的中文原样。

## 5. 卡片 anatomy

```
Box(clickable, key=task.ID, radius t.Radius+2, border 1 t.Border,
    padding t.Space(3.5), gap t.Space(2), bg cardFace(t))
├─ ui.Text(c, task.Title).FontSize(t.FontSize).FontWeight(500).MaxLines(2).Ellipsis("…")
├─ [可选] ui.Text(c, 摘要).FontSize(t.FontSize-1).TextColor(t.TextMuted).MaxLines(2).Ellipsis("…")  ← Description 首两行
└─ Row(height 20, AlignItems Center, gap t.Space(1.5)):
     ui.Avatar(c, task.Assignee, nil)（Assignee 为空则不画）
     + 出处行 provenance(task) f-xs TextMuted：「v3 · ive · 09:30」（版本 >1 才有 v 段，
       UpdatedBy 非本窗口才有代理段，时间恒有；保存中换成指示器，见 §7 第 3 行）
     + Spacer
     + [len(task.Comments) > 0] ui.Badge(c, strconv.Itoa(len(task.Comments))).Label("3 条评论")
```

- 版本不再用徽标：它和代理署名、时间一起构成一行安静的出处文字，
  「谁在动这块板」是这个产品的身份，让它常驻但低语。徽标只留给评论计数。

- 卡片是自定义可点元素：`ui.Box(c.Key(task.ID)).Focusable().Cursor(ui.CursorPointer)
  .Role(ui.RoleButton).Label("打开任务：" + task.Title)`，用 `.Clicked()` 判定；
  `Focusable` + `Clicked` 让它自动可被 Enter/Space 激活。
- 正文只在详情里编辑，卡片上只显示不编辑（避免误触与二次焦点）。
- 卡片不显示完整评论内容，只显示计数；评论列表在详情弹层。

## 6. 交互

| 动作 | 规格 |
| --- | --- |
| 卡片悬停 | 背景 `cardHover(t)`、描边 `t.Border.Mix(t.Text, 0.22)`，共用 B 柔和弹簧的颜色过渡 |
| 卡片按下 | 背景 `t.SurfacePressed`；按下即 `CursorPointer` 已设，不做缩放 |
| 卡片点击 | 打开详情：`ui.Modal(c, &a.detailOpen, …)`，点击遮罩 / Escape 关闭 |
| 键盘焦点 | 保留默认焦点环（`t.Focus` 绘制），不 `FocusRing(false)`；每列卡片组 `FocusGroup(ui.Vertical).Label("待办列任务")`，Tab 进列，↑↓ 选卡，Enter 打开 |
| 拖动 | 卡片 `.Drag(cardDrag(task.ID))`；拖动中源卡隐藏并退出布局，邻卡收拢；目标列显示等高、无文字占位虚影，不显示轨道蓝框；按自然布局计算插入位置，释放才提交 API；取消从指针位置平滑返回 |
| 右键移动 | 卡片 `.ContextMenu(func(m *ui.Menu){…})`：`m.Submenu("移动到", func(s *ui.Menu){ 每列 s.Item(列名).Checked(当前列).Chosen() })`、`m.Separator()`、`m.Item("打开详情").Chosen()`、`m.Item("归档").Chosen()` |
| 列内新建 | 列头下方输入框，Enter 提交并清空（保持焦点），Escape 放弃；创建成功后列内顶部插入新卡 |
| 详情弹层 | 内容宽 s130（520 DIP），最大为窗口宽减 s20；标题 f-md/500，正文最小高 s30；标题、正文、选列和评论共用一个纵向滚动区（最大高为窗口高减 s56），顶部关闭、底部评论输入和保存固定可见。输入框用 Placeholder 与读屏 Label，不另加可见字段名。选列右侧右对齐显示任务出处 |
| 评论 | 作者 f-sm 600 + 时间 f-xs `TextMuted` + 正文 f-base；长作者名单行省略并提供 Tooltip。固定输入框 + 次级 `Button("发送")`，回车与点击共用提交入口；发送中禁用输入和按钮并阻止重复提交，失败恢复草稿；保存是详情内唯一主按钮 |
| 归档 | 不弹确认：`c.ToastAction("已归档「X」", "撤销", undo)`，撤销走 PATCH 还原 `archived=false` |
| 变更高亮 | 收到他人更新后 600ms 内把 `AnimateColor("flash", t.Accent.Alpha(0.12), ui.Animation{Duration: 600 * time.Millisecond})` 的目标设为强调色淡底，随后把目标切回 `cardFace(t)`，颜色平滑回落 |

详情弹层的保存是显式的（点「保存」才 PATCH），避免输入过程中每键写服务。

## 7. 加载态映射表

所有指示器必须带中文 `Label`（读屏读作"未知长度的进度指示器"），`ReduceMotion` 下插件自动静止。

| # | 场景 | 指示器 | Size | Color | 中文 Label | 位置与文案 |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | 初次载入看板 | `loading.Arc` | 20 | `t.TextMuted` | 正在载入看板 | 看板区居中，下方 `ui.Text("正在载入看板…", f-sm, t.TextMuted)` |
| 2 | 切换项目 | `loading.Ring` | 16 | `t.Accent` | 正在切换项目 | 顶栏项目名右侧内联；旧列内容整体 `Opacity(0.55)`，不清空 |
| 3 | 保存卡片 | `loading.LinearDots` | 14 | `t.TextMuted` | 正在保存任务 | 卡片右下角时间戳位置；弹层内在「保存」按钮左侧 |
| 4 | 发评论 | `loading.BouncingDots` | 12 | `t.Accent` | 正在发送评论 | 「发送」按钮左侧内联；输入框 `Disabled(true)` |
| 5 | SSE 更新后过渡 | `loading.Pulse` | 10 | `t.Accent` | 正在同步更新 | 顶栏最右同步点 + 状态词「同步中」（f-sm）；静止态是 8×8 `Radius(999)` 的 `t.Success` 圆点 + 「已同步」，事件到达后换成 Pulse 显示 1.2s（其自身周期）再回静止态 |
| 5b | 服务连接中（行内） | `loading.Arc` | 12 | `t.TextMuted` | 正在连接本地服务 | 顶栏最右 + 状态词「连接中」；离线时 Comet + 「重连中」词用 `t.Warning` 色。**初次载入期间（行 1 显示中）改为静止 8×8 `t.Border` 圆点、不显状态词**：数据还没同步过，不预支「已同步」；也保证同一时刻只有一个板级指示器 |
| 6 | 服务离线重连 | `loading.Comet` | 12 | `t.Warning` | 正在重新连接本地服务 | 看板上方横幅内 + 文字「本地服务未连接，正在重试…」+ `ui.Button("重试")` |
| 7 | 归档 | `loading.Arc` | 14 | `t.TextMuted` | 正在归档任务 | 弹层底部按钮行左侧 |
| 7b | 载入评论 | `loading.LinearDots` | 12 | `t.TextMuted` | 正在载入评论 | 详情弹层「评论」标题右侧；与行 4 的 `BouncingDots` 区分，避免同形不同义 |
| 8 | 移动列 | 不用指示器 | — | — | — | 乐观更新 + §10 的过渡；失败才回滚并 `c.Toast` |

同一时刻最多一个全板级指示器（1、6）；2/3/4/5/5b/7/7b 都是行内小尺寸，可并存。行 5b 在行 1 显示期间退化为静止圆点且不出状态词，避免两个板级旋转指示器，也不预支「已同步」。

## 8. 明暗主题对照

主题不写死，全部由 `ui.LightTheme()` / `ui.DarkTheme()` 的字段表达；`t.Dark` 只用来在三处结构性差异上分支。

| 角色 | 明（light） | 暗（dark） | 需显式处理 |
| --- | --- | --- | --- |
| 窗口 / 顶栏 / 看板底 | `t.Background` | `t.Background` | 否：直接引用 |
| 列面板 | `t.Surface`（比底略深一档，无描边） | `t.Background`（与底同档，卡片亮一档成列） | **是**：`t.Dark` 分支，暗色下若用 `Surface` 会与卡片同色而糊成一片 |
| 卡片静止 | `t.Background`（浮在列面上） | `t.Surface`（亮一档） | **是**：同上，明暗互换方向 |
| 卡片悬停 | `t.SurfaceHover` | `t.SurfaceHover` | **是**：明色卡片为 `Background` 浮在 `Surface` 列面上，悬停须再深一档到 `SurfaceHover`（用 `Surface` 会与列面 1.00:1、卡片塌成洞）；暗色同值 |
| 卡片按下 | `t.SurfacePressed` | `t.SurfacePressed` | 否 |
| 描边（卡片静止） | `t.Border` | `t.Border` | 否 |
| 描边（卡片悬停） | `t.Border.Mix(t.Text, 0.35)` | 同 | 否：悬停面被填充后，仅 0.22 的描边不够区分 |
| 强调填充（主按钮） | `t.Accent` | `#2563eb` | **是**：暗色主题的 `#3b82f6` 配白字仅 3.68:1，低 AA；应用侧复制主题并把 Accent/AccentHover/AccentPressed 换成 `#2563eb` 系（hover/pressed 用 mygo 的 `Mix` 规则派生），白字 5.17:1。高对比偏好下不改 |
| 主文字 / 标题 | `t.Text` | `t.Text` | 否 |
| 次要文字 / 摘要 / 时间 | `t.TextMuted` | `t.TextMuted` | 否；高对比模式由 Theme 自动加深 |
| 强调 / 主按钮 / 选中 | `t.Accent`，文字 `t.AccentText` | `t.Accent`，文字 `t.AccentText` | 否（系统强调色由 Theme 的 `follow` 接管） |
| 危险（归档） | `t.Danger` | `t.Danger` | 否；仅文字色，不做红色填充 |
| 成功（同步点静止态） | `t.Success` | `t.Success` | 否 |
| 警告（离线横幅） | `t.Warning.Alpha(0.14)` 底 + `t.Warning` 指示器 | 同 | 否：只调透明度不换色 |
| 焦点环 | `t.Focus`（Theme 已带透明度） | `t.Focus` | 否；不自绘焦点环 |
| 滚动条 | `t.Scrollbar` | `t.Scrollbar` | 否 |
| 拖动浮层 | `Shadow(0, 6, 18, 0, ui.RGBA(0,0,0,0.18))` | 同 | 否：两套主题下都是半透明黑，暗色下也不发白 |
| Toast / Tooltip | `ui.Toast`，框架用 `t.Inverse` / `t.InverseText` | 同 | 否 |

只有三行需要 `t.Dark` 分支（列面、卡片静止、卡片悬停），写成 `colFace(t)` / `cardFace(t)` / `cardHover(t)` 三个函数，其余一律直接读字段。
切换主题时不需要重载数据：`Context.Theme()` 每帧返回当前主题，`c.Invalidate()` 由框架在系统外观变化时自动触发。

## 9. 空态与错误态

| 场景 | 内容 |
| --- | --- |
| 无项目 | 居中 `Column(gap t.Space(2), PaddingY(t.Space(8)))`：`Text("还没有项目", f-lg, 600)` + `Text("创建第一个项目，开始整理任务。", f-sm, t.TextMuted)` + `PrimaryButton("新建项目")` |
| 整板无任务 | 每列一条安静占位：`Box(Padding(t.Space(4)))` 内两行 f-sm、`TextMuted` 文字——「还没有任务」+「从上方新建，或拖入卡片」；不再叠加透明度，不画虚线框 |
| 无评论 | `Text("还没有评论", f-sm, t.TextMuted)`，位于分隔线下、输入框上 |
| 服务离线 | 横幅：`Row(Padding(0, t.Space(4)), Height(36), Background(t.Warning.Alpha(0.14)))` + 指示器 6 + 文案 + 「重试」；不遮断已载入的列与卡片（只读） |
| SSE 到达但弹层有未保存编辑 | **不覆盖输入框**：卡片与版本号照常更新，弹层顶部显示一行「该任务已被 <agent> 更新到 vN，你的编辑尚未保存。」；点「保存」时按 409 同一套 AlertDialog 处理（使用最新版本 / 保留我的修改）。无未保存编辑时静默替换 |
| 版本冲突 409 | 拉一次 `GET /api/tasks/{id}` 对齐。本地无未提交编辑 → 静默替换 + `c.Toast("任务已被其他代理更新，已同步最新版本")`；有未提交编辑 → `ui.AlertDialog(c, &a.conflict, "任务已被修改", "「X」已被 <agent> 更新到 v4。", "使用最新版本", "保留我的修改")`，返回 1 时以新 version 重发 PATCH |
| 请求失败（非 409） | `c.AddToast(ui.Toast{Title:"操作失败", Description: err, Type:"error", Timeout: 6*time.Second})`；不弹模态 |

## 10. 动效

| 变化 | 规格 |
| --- | --- |
| 整板入场 | 每列 `Enter: &ui.Motion{Y: t.Space(3), Opacity: 0}`，280ms `ui.EaseOut`，`Animation.Delay = index × 40ms` 逐列错位；切换项目时随新键重放 |
| 卡片插入 | `Enter: &ui.Motion{Y:t.Space(2), Collapse:true, Scale:0.985}`，420ms 柔和弹簧（回弹 0.04） |
| 卡片删除/归档 | 直接移除，邻卡平滑收拢；不保留退出卡面，避免跨列重复残影 |
| 跨列移动 | 独立浮层接续源卡位置，抬升 160ms（上浮 8 DIP、缩放 1.025、展开阴影）→ 420ms 柔和弹簧移动 → 680–900ms 收拢高度与阴影；同批视觉起步 0 / 40 / 80ms（最多等待 80ms），API 与状态更新保持并行；中途改道保留动画身份、当前位置及当前高度 |
| 列内重排 | 邻卡采用 420ms 柔和弹簧让位，保持原尺寸与阴影；手动释放保留抓取偏移，直接接续落位，不重复抬升。正式版与 Playground 共用渲染实现；候选参数、演示按钮及临时服务仍仅在 Playground |
| 他人更新高亮 | `AnimateColor("flash", t.Accent.Alpha(0.12), ui.Animation{Duration: 600 * time.Millisecond})` 后切回 `cardFace(t)` |
| 详情弹层 | `ui.Modal` 无内置入场动画（遮罩+面板直接出现）；不叠加位移或缩放，只用框架的遮罩 40% 黑 |
| ReduceMotion | 不自行实现位移（`Transition` 已在此偏好下（`c.Preferences().ReduceMotion`）不动，Enter/Exit 仅就地淡入淡出，颜色仍平滑）；loading 插件自动取静止姿态；自定义 `Animate`/`AnimateTo` 自动直达目标 |

卡片布局及跨列位移使用上述柔和弹簧；普通曲线位移时长 ≤300ms，不给打开/关闭详情弹层叠加弹簧。
不使用 `Loop` 做装饰动画；不允许无限动画（除 loading 指示器）。

## 11. 无障碍

- 每个 loading 必须有 `Label`，文案见 §7 的 Label 列；同一屏内两个指示器的 Label 不得相同。
- 按钮可读名称（默认文字即名称，图标化时必须显式 `Label`）：
  刷新看板 / 新建项目 / 项目切换（左侧侧栏点选）/ 保存任务 / 归档任务 / 发送评论 / 重试连接 / 关闭详情。
- 卡片 `Role(ui.RoleButton)` + `Label("打开任务：<标题>")`；列头 `Role(ui.RoleHeading).Level(2)`（以文字为名）；列容器 `Role(ui.RoleGroup)` + `Label("待办列")`；计数另由列头数字的 `Label("3 个任务")` 承载。
- 输入框一律 `Label`：任务标题 / 任务正文 / 移动到列 / 在「待办」中添加任务 / 写评论。
- 评论计数徽标 `Label("3 条评论")`，否则 `ui.Badge` 的裸数字会被读成孤立数字。
- 焦点顺序：顶栏（项目 → 刷新 → 新建项目）→ 列 1（新建输入 → 卡片组）→ 列 2 …；弹层打开时焦点进入面板首字段（`AutoFocus()`），关闭后回到原卡片。
- 仅靠颜色传达的信息必须配文字：离线横幅有文案、版本冲突有弹层标题、保存中有指示器 + Label。

## 12. 反模式（禁止）

1. 禁止 WebView / HTML / CSS 外壳：全部走 `ui` 包原生控件。
2. 禁止 `ui.Hex`、`ui.RGBA` 自造色值（阴影例外）；明暗差异必须靠 Theme 字段表达。
3. 禁止在静止卡片、列、按钮上加 `Shadow`；只有拖动浮层与 `ui.Modal` / 菜单这类真正悬浮的东西有阴影。
4. 禁止轮询：不用 ticker/定时器拉 `/api/tasks`；一切更新来自 SSE。
5. 禁止 SSE 事件后整表重建或 `c.Invalidate()` 全清：靠 `c.Key` + `Match` 就地更新。
6. 禁止把列数、列名、种子文案写死在视图里；一律来自 `Board.Columns`。
7. 禁止裸 `Height(52)` 之类的魔法尺寸散落各处：间距走 `t.Space`，字号走 §3 的 f-* 刻度。
8. 禁止无 `Label` 的 loading / 无 `Label` 的图标按钮 / 用 emoji 当图标。
9. 禁止正文小于 `t.FontSize-1`；禁止全大写、字距拉宽的装饰性小标签。
10. 禁止把完整正文塞进卡片：摘要 `MaxLines(2)`，超出在详情里看。
11. 禁止在列内再嵌横向滚动，或在整板横向滚动内再套纵向滚动容器（每层只滚一个方向）。
12. 禁止乐观更新失败后不提示：任何回滚必须伴随 `c.Toast`。
13. 禁止禁用按钮却不给理由（如「保存」在无改动时应直接不提交，而不是灰着不解释）。
14. 禁止自定义焦点环或隐藏焦点：一律用框架的 `t.Focus` 环。
15. 禁止用文字解释布局：不加「评论：」「状态：」类字段前缀，不加可见字段标签（输入框靠 Placeholder 与所在分组表意），不加区域用途说明小字；读屏 `Label` 照设，不属此类。
16. 判断法：删掉一段次要文字后，布局仍能传达同样的含义（如计数徽标之于「评论：3」），这段文字就不该存在；空态的操作指引（§9）不在此列。

## 13. 核对清单（全部已在 mygo v0.6.3 中存在）

本文引用的全部 mygo v0.6.3 标识符，已逐个在模块缓存里 `grep` 核对存在（构造函数在 `ui/api_gen.go`，方法在 `Element`/`Context`，常量在对应源文件，指示器在 `plugins/loading`）。

- 控件：`Box` `Row` `Column` `Text` `Button` `PrimaryButton` `Badge` `Avatar` `Divider` `Spacer` `Scroll`
  `ScrollHorizontal` `Modal` `AlertDialog` `MenuButton` `Select` `TextInput` `TextArea` `Tooltip`（方法）
- 元素方法：`Padding` `PaddingY` `Gap` `Grow` `Width` `Height` `MinWidth` `MaxWidth` `MinHeight` `Radius`
  `Border` `BorderStyle` `Background` `TextColor` `FontSize` `FontWeight` `MaxLines` `Ellipsis` `Opacity` `Fill`
  `AlignItems` `Label` `Role` `Level` `Focusable` `FocusGroup` `FocusRing` `AutoFocus` `Cursor` `Disabled`
  `ContextMenu` `Drag` `DragOver` `Drop` `Dragging` `PointerPosition` `Clicked` `Shadow` `Transition`
  `AnimateColor` `Animate` `AnimateTo`
- 常量与类型：`ui.Center` `ui.Stretch` `ui.Horizontal` `ui.Vertical` `ui.CursorPointer` `ui.BorderDashed`
  `ui.RoleButton` `ui.RoleHeading` `ui.RoleGroup` `ui.EaseOut` `ui.Snappy` `ui.Motion` `ui.ElementTransition`
  `ui.Animation` `ui.Toast`
- Context：`c.Theme()` `c.Key()` `c.Invalidate()` `c.Toast()` `c.ToastAction()` `c.AddToast()`
  `c.Preferences().ReduceMotion`
- loading 插件：`loading.Arc` `loading.Ring` `loading.LinearDots` `loading.BouncingDots` `loading.Pulse`
  `loading.Comet`、`loading.Options{Size, Color, Easing, Paused}`、`.Label(...)`

共核对 111 个 API 标识符，0 处不存在。未引用的控件（`Table` `Tree` `Calendar` `Tabs` 等）不在本规格内。

2026-10-25：用户授权正式接入已验收的 B 动效及 0 / 40 / 80ms 多卡错峰。正式构建和 Playground 共用动效引擎；回归包含真实 HTTP/SSE 并行移动、悬浮/落稳途中改道、等待起步时改道、邻卡让位、手动落位及减少动效。
