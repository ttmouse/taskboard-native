# 验收环境与流程（Rams 用）

窗口已实测可开：1280×800，暗色（系统当前外观），四列 待办/进行中/评审/完成，种子卡 3 张。

## 启动（真实窗口）
```bash
cd /Users/douba/Projects/taskboard-native
go build -o /tmp/tb-review/taskboard ./cmd/taskboard
mkdir -p /tmp/tb-review && rm -f /tmp/tb-review/taskboard.db
TASKBOARD_ADDR=127.0.0.1:47827 TASKBOARD_DB=/tmp/tb-review/taskboard.db /tmp/tb-review/taskboard &
```
注意：macOS 可能弹「访问文件夹」权限框，若出现，截图后不要点它，直接 `kill` 重试；如持续出现，改为用免窗口渲染图（`docs/assets/render/*.png`，1280×800）+ 你独立跑的 `go test ./cmd/taskboard` 结果做走查，并在报告里注明。

## 截图（窗口级）
```bash
B=$(osascript -e 'tell application "System Events" to tell (first process whose name is "taskboard") to get {position, size} of window 1' | tr -d ' ' | tr ',' ' ')
screencapture -x -R$(echo $B | tr ' ' ',') /tmp/tb-review/window.png
```
（实测可用；也可 `screencapture -x` 全屏后裁。）

## 必须覆盖的检查
1. 视觉：列/卡片层级、间距节奏、字号刻度、圆角与描边一致、顶栏对齐、暗色对比度（上一版曾出现卡片近白+浅字不可读，确认已修）。
2. 状态：加载态（`TASKBOARD_SLOW=800ms` 启动，观初次载入 Arc + 「正在载入看板…」）、空态（新建空项目）、离线横幅、409 冲突、SSE 就地更新（用 CLI 写入触发）。
3. 明暗两种外观（系统外观切换或渲染图对照）。
4. loading 插件使用是否符合规格表（docs/design.md §6）：指示器选型、尺寸、颜色、中文 Label。
5. 布局自解释（design.md 总则与 §11 第 15、16 条）：无多余次要标签、字段前缀与说明文字，含义由位置、层级、间距、字号表达。

## 真实数据操作（触发 SSE）
```bash
cd /Users/douba/Projects/taskboard-native
go run ./cmd/tb --agent rams list --project local
go run ./cmd/tb --agent rams add "验收写入测试" --project local
go run ./cmd/tb --agent rams move <taskID> 进行中
```
窗口应就地更新（卡片滑入、计数变化），无整表刷新。

## 交付
`docs/design-review.md`：Overall / High / Medium / Low / What Looks Good / Top 3 Fixes；截图存 `docs/assets/review/`。只报告不修改代码。结论里明确写 PASS 或 CONDITIONAL PASS。