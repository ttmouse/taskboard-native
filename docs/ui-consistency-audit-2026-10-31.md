# UI 一致性审计：taskboard-native ↔ DSH / design.md

- **日期**：2026-10-31（Asia/Shanghai）
- **范围**：只读源码核对（`cmd/taskboard/*.go`），对照 `docs/design.md` 与 DSH 面板规范（`dsh-companion/docs/dsh-design-spec.md`、`src/client/companion-app.tsx` / theme tokens）
- **性质**：不改 UI 代码；可复用上一轮要点并经源码再核
- **看板评论**：已在项目 `taskboard-native` 卡 **LOCAL-29**（项目文档）POST 摘要；其余卡均为 done，未新建卡

## 严重度说明

| 级 | 含义 |
| --- | --- |
| 高 | 明色可用性/分层失效，或与 design 硬规格直接冲突且用户可感知为「坏了」 |
| 中 | 同屏层级/控件语汇不一致，对照 DSH 或 design 有明确偏差 |
| 低 | 微调/观感；或规格与实现差一小档 |

---

## Top 差异（优先）

| # | 差异 | 严重度 | 证据 | 建议 |
| --- | --- | --- | --- | --- |
| 1 | **明色 `cardFace ≡ cardHover`**（悬停无填色变化） | **高** | `view.go` `cardFace`→`t.Surface`；`cardHover` 明色分支也 `return t.Surface`（暗色才用 `SurfaceHover`） | 对齐 design §7：明色静止用可与窗底分层的面 + 悬停再走一档；或明色悬停改 `SurfaceHover`/`SurfacePressed` 派生，并补回归断言「明色 face≠hover」 |
| 2 | **卡片无描边**（`BorderWidth(0)`） | **高** | `taskCard`：`BorderWidth(0)`；design §4/§7 要求静止 `t.Border`、悬停加深；DSH 卡/设置卡用 **0.5px hairline**（`settingsStroke` / `separator`） | 恢复 1 DIP `cardEdge`（或逼近 DSH 的发丝线观感）；明色尤其依赖描边成形 |
| 3 | **详情区头 / 关系子标签字号混用** | **中** | 区头「关系」「评论」「自动承接」=`fxs`（FontSize−2）；子标签「副任务 / 阻塞与 / 阻塞于」=`fsm`（−1） | 区头统一 `fxs`+600；子标签降为同档或明确「子级用 fsm、区头用 fxs」写进 design |
| 4 | **列头高度 28 ≠ 规格 36** | **中** | design §1：`Height(36)`；`columnHeader`：`Height(t.Space(7))` = **28** | 改 `Height(36)` 或 `t.Space(9)`，与横向顶栏节奏对齐 |
| 5 | **关系折叠用文字箭头 `∨`/`›`，非 chevron** | **中** | 关系头拼接 `"  ∨"` / `"  ›"`；列头已正确用 `chevronDown`/`chevronRight` | 与列头同套 lucide chevron；去文字箭头 |

---

## 其他差异

| 差异 | 严重度 | 证据 | 建议 |
| --- | --- | --- | --- |
| 关联行用全铬 `ui.Button` 打开相关卡 | 中 | `renderLinked` 内 `ui.Button(...).Label("#n title")`；DSH 关联/时间线偏安静文字+发丝 | 改为 `ButtonBase`/去边链接行，危险操作（×）单独弱铬 |
| 关系行距偏紧 | 低 | 关系块 `Gap(t.Space(1))`；详情主柱 `Gap(t.Space(4))` | 子列表提至 `s1.5`/`s2`，贴近卡片内部行距 |
| 详情外层 padding | 低 | `Padding(t.Space(5), t.Space(6))`；DSH 面板 gutter/composer 更克制 | 与 design 详情条对齐后写死；左右可收敛到 s5 |
| 评论气泡 alpha / 头像 | 低 | 气泡 `SurfaceHover.Alpha(0.55)`，头像 20；composer `Alpha(0.4)`；design：淡底+Avatar 20 | 明色下提高不透明或改实心 `Surface`；保持头像 20 |
| 多处裸 `FontSize-2` | 低 | `detail_images`/`compose_images` 用 `t.FontSize-2`，未走 `fxs` | 一律 `fxs(t)`，避免刻度分叉 |
| `composeLiftPercent=14` | 低 | 常量 14；design 交互表写 **12%** | 改 12 或改规格为 14 并注明最小窗仍可见 |
| 搜索输入字号 `FontSize+5` | 低 | `searchView`；远超 f-md(+2)/f-lg(+7 空态) | 降到 `fmd` 或 `FontSize+2`，与弹层标题同级 |
| 设置行名 | 低（基本符合） | 「外观 / 显示名 / 全局新建任务 ⌘⇧N」；控件不重复行名 | 保持；若增 Git 等开关，继续「左名右控」 |

---

## 对照 DSH 的原则差距（摘要）

1. **发丝线分层**：DSH 浅色多层 `bg` 同色时靠 **0.5px border / elevation**；native 明色去列面后，卡片再无描边 → 轮廓依赖面色，悬停一塌就「挖洞」。
2. **折叠控件**：DSH/宿主用 chevron + 统一 disclosure；native 列头已对齐，关系区仍用字符箭头。
3. **交互底**：DSH `interactive-bg-hover` 明暗都有可见洗色；native 明色 hover 面与静止面同值。
4. **圆角档**：DSH 按角色选 radius token；native 多用 `t.Radius(+2)`，整体可接受，但浮层/卡与 DSH `xl/panel` 语汇不同属产品差异，不单列缺陷。

---

## 建议落地顺序

1. 修明色 `cardHover` + 恢复卡片描边（高）  
2. 列头高度 36、关系 chevron、关联行去铬（中）  
3. 区头字号表、composeLift/搜索字号、评论 alpha（低）

---

## 看板评论结果

- API：`127.0.0.1:47825`（DeepSeek 看板服务）可用  
- 项目：`taskboard-native`（`f8b81940-…`）仅 3 张卡且均为 **done**（LOCAL-29/32/33）  
- 已向 **LOCAL-29**（项目文档）POST 审计摘要评论；未新建卡  
