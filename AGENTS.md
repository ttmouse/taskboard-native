# 项目开发约定

## 功能实现与交付

**强制版本约定（2026-10-31 起）：** 凡对 taskboard-native 的功能改动、UI 变化或行为调整，都必须：① 在 `mygo.json` 递增版本号（patch 起跳，如 `0.1.77` → `0.1.78`）；② 重新编译并安装 `/Applications/Taskboard.app`；③ 在回报里明确写出新版本号，便于用户对照 App 内版本确认是否生效。禁止「改了代码却不 bump」。纯文档/注释且不影响运行行为的改动可不 bump。

每个可见功能按以下顺序完成：

1. **自我验证** — 运行最低校验命令 `go build ./... && go vet ./... && go test ./...`；失败时先修复或如实记录阻塞。
2. **更新版本号** — 在 `mygo.json` 中按语义化版本递增 patch/minor/major（见上方强制约定），并确保版本号显示在应用界面中。
3. **更新 `.app`** — 运行版本生成脚本并重建 macOS `.app`，安装到 `/Applications/Taskboard.app`，让用户能直接体验；交付 `.app`，可附 ZIP 便于传递。**不制作 DMG。**
4. **一句话总结** — 回复末尾用粗体写一句自包含总结，必须先说清本次具体做的任务或解决的问题，再说明结果、**新版本号**和当前状态（已完成、待评审、受阻等），并说明工作是否仍在继续。不能只写状态或流程结果；脱离任务内容的「已完成并等待验收」不合格。示例：`**已让 Herdr Codex 卡片上的执行状态可点击以定位原终端，功能已实现，版本 0.1.78，当前等待人工验收。**`

## Code Review Rules

机械检查（格式、编译、测试）归 CI / 本地 `go test`；下列规则只抓「会弄坏生产或契约」的行为。写清后果与 safe path；假阳性第二次出现就改这条规则，而不是无限争论。

### 契约与唯一写者

- 改 HTTP / SSE / CLI / MCP 的行为或字段前，必须先改 `docs/contracts.md`，再改代码。
  Safe path: 先开契约 diff，再动 `internal/model` 与 handler；禁止只改实现让文档落后。
- 看板数据的写入只经本服务；GUI / CLI / MCP 都是客户端。禁止为「方便」直写 SQLite 或绕过服务。
  Safe path: 新写路径走现有 API / `tb` / MCP；GUI 只靠 SSE 更新，禁止轮询。

### 执行与状态语义

- 派发程序不得根据退出码、最终文字或会话正常结束自动把卡片标完成或擅自移列；完成留给人确认。
  Safe path: 执行 Agent 按 `manage-taskboard-native` 认领、回写评论、移到评审；程序只跟踪连接/会话。
- 无人类 `mention` 授权时，禁止为同一任务新建第二个执行会话或静默换渠道。
  Safe path: 续聊走原会话；换渠道必须有结构化 `mention` 或显式新派发。

### 信任边界与本地数据

- 不要把 token、cookie、API key、endpoint 鉴权文件内容写入日志、评论或任务正文。
  Safe path: 只记稳定 request / task / session ID；失败写可操作的诊断，不写密钥原文。
- 默认绑定本机环回（`127.0.0.1:47826`）。若改监听地址或暴露面，必须在 PR / 任务说明里写明风险与回滚。
  Safe path: 保持环回；需要覆盖时用 `TASKBOARD_ADDR`，并说明谁能连上。

### 兼容与回滚

- 破坏外部已消费的 JSON 字段名、事件类型或 CLI 旗标时，必须提供兼容期或迁移路径。
  Safe path: 先增后删（expand → migrate → contract）；旧客户端至少能读或得到明确 4xx。

## 开 PR / 提交前

1. 跑绿上面的校验命令。
2. 本地预审（有 Codex 则 `/review` 或 `codex review --base main`；有 Claude 则 `/code-review`；涉派发/鉴权再跑安全预审）。
3. 说明：做了什么 / 怎么测 / 风险 / 如何回滚。
4. AI 审稿意见不是合并许可；合并权在人。高风险改动（契约、派发、数据迁移、监听面）人必须再读关键 diff。

## 周期任务

启动与例行工作前阅读 [docs/periodic-tasks.md](docs/periodic-tasks.md)；结果写回看板「周期任务表」卡。
