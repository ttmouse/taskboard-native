# 设计师相关可测项短清单（P1 向）

- 日期：2026-10-31 Asia/Shanghai
- 执行：研发三（只分析落盘；**本次未改码**）
- 来源优先：`docs/ui-consistency-audit-2026-10-31.md`、`docs/design-review.md`、`docs/design.md`（列头高等硬规格）
- 说明：对照 **当前** `cmd/taskboard/view.go` 再核；审计里已修项仍列入「回归可测」，避免回退。

---

## 可测项

| # | 来源条目 | 可测断言（怎么测） | 优先级建议 | 源码现况（2026-10-31） |
|---|----------|-------------------|------------|------------------------|
| 1 | UI 审计 #1：明色 `cardFace`≠`cardHover` | **单测**：`cardFace(light) != cardHover(light)` 且均 ≠ `colFace`（已有 `render_test` 同类断言，保持红灯防回退）。**截图**：明色悬停卡面较静止面可辨。 | P0 回归 / 原审计「高」 | **已修**：明色 face=`Background`，hover=`Surface` |
| 2 | UI 审计 #2：卡片静止描边 | **单测/静态**：`taskCard` 使用 `Border(1, cardEdge(t))`；悬停边 `Border.Mix(Text,0.35)`。**截图对比**：明色静止卡有可见轮廓。 | P0 回归 / 原「高」 | **已修**：`cardEdge` + `Border(1,…)` |
| 3 | UI 审计 #4 / design §1：**列头高度 36** | **量尺寸**：`columnHeader` 根控件 `Height` == **36**（或 `t.Space(9)` 若 Space 单位=4）。当前为 `Height(t.Space(7))`≈**28**，与 design「列头高 36」冲突。**UI 测试**：布局探测 header 外接矩形高；或截图标尺对比顶栏 36 横幅。**回归**：改完后 `Height(36)` 常量/辅助函数单测。 | **P1（必测）** | **仍偏差**：`view.go` `columnHeader` → `Height(t.Space(7))` |
| 4 | UI 审计 #5：关系折叠用 lucide chevron | **UI/截图**：详情「关系」展开/折叠控件为与列头同套 `chevronDown`/`chevronRight`，**不得**在标题字符串拼 `∨`/`›`。**静态扫**：`rg '∨|›' cmd/taskboard/view.go` 关系头路径为零。 | **P1** | **仍偏差**：`headLabel += "  ∨"` / `"  ›"` |
| 5 | UI 审计 #3：详情区头 vs 关系子标签字号 | **断言**：区头「关系 / 评论 / 自动承接」=`fxs`+FontWeight 600；子标签「副任务 / 阻塞与 / 阻塞于」字号 ≤ 区头且角色一致（全 `fxs` 或文档写明「子级 fsm」）。**截图**：同屏三级字阶可读。 | P1 | **仍混用**：区头 `fxs`；子标签 `fsm` |
| 6 | UI 审计「关联行全铬 Button」 | **截图/控件树**：`renderLinked` 打开相关卡的控件非默认实心 `ui.Button` 铬（改为 `ButtonBase`/弱链）；危险「×」单独弱铬。 | P1 | **仍偏差**：`ui.Button(...).Label("#n title")` |
| 7 | UI 审计「关系行距」 | **量尺寸**：关系块子列表 `Gap` ≥ `t.Space(1.5)`（或 s2），不低于卡片内行距观感。 | P2 | 现 `Gap(t.Space(1))` |
| 8 | UI 审计「评论气泡 alpha / 头像 20」 | **截图明色**：气泡底不透明到可辨正文；头像尺寸 **20**。对比度抽检 Text on bubble ≥ AA 目标。 | P2 | 气泡 `SurfaceHover.Alpha(0.55)` 等 |
| 9 | UI 审计「裸 `FontSize-2`」 | **静态**：`detail_images`/`compose_images` 等统一走 `fxs(t)`，禁止散落 `t.FontSize-2`（允许 `fxs` 定义处一处）。 | P2 | 审计仍列多处裸减档 |
| 10 | UI 审计 `composeLiftPercent` | **断言**：`composeLiftPercent` == design 交互表 **12**，或 design 改为 14 并注明。单测/常量审查即可。 | P2 | 常量 **14**（`view.go`） |
| 11 | UI 审计「搜索输入字号」 | **量/截图**：`searchView` 输入字号 ≤ `fmd`（FontSize+2），不得 `FontSize+5`。 | P2 | 审计指 `FontSize+5` |
| 12 | design-review / design §3：列头 Role 与计数 Label | **无障碍/UI 测试**：列头 `RoleHeading.Level(2)`；计数 `Label("%d 个任务")`；列容器 `RoleGroup`+`"<列名>列"`。列头折叠按钮 Label 含「折叠/展开」+列名。 | P1（a11y 回归） | 列头路径基本符合；改高度时勿破坏 Role |
| 13 | design-review 白空间叙事：列头 32→36 | 与 #3 合并测：修高后看板白空间节奏与 `board-light` 类截图基线更新（若有 golden）。 | P1 | 依赖 #3 落地 |

---

## 建议测试顺序

1. **列头高度 36**（#3）— 设计师 P1 显式点名；改动面小、可量。  
2. 关系 chevron（#4）+ 关联行去铬（#6）+ 区头字号表（#5）。  
3. 锁住已修高项回归（#1/#2）。  
4. 其余低项按需。

---

## 本次未改码

仅产出本清单；未改 UI/测试/规格正文。
