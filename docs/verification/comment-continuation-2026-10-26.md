# 评论接回原始执行会话 — 2026-10-26

## 行为

- 普通发送仅保存评论。
- 「发送并跟进」先保存评论，再提交原会话；后一步失败时评论保留。
- 已发送评论下的「让 AI 跟进」只投递该评论，不另建评论。
- API：POST /api/tasks/{id}/comments/{commentId}/continue；CLI：tb followup <taskID> <commentID>。
- 每条评论持久化投递回执；重复请求不重复执行。
- 运行中拒绝新跟进；原会话缺失拒绝；投递结果不确定时停止重发。
- 执行 Agent 负责结果回写和状态流转；程序不代判任务完成。

## 真实模型验收

使用临时数据库、临时工作目录、独立运行配置；没有修改用户既有卡片或全局模型配置。所有探针模型均为本机目录确认存在的 codex/gpt-6-luna。Codex/DSH 配置 medium；Pi 沿用该 CLI 的推理默认值。

| 渠道 | 真实验收结果 | 原会话 |
| --- | --- | --- |
| Codex | 通过，85.21 秒 | 01a177c0-af35-7020-a2ca-23b127dc68fb |
| Pi | 通过，118.69 秒 | 独立会话文件 d7f9df21845f5447e47c5f3b8ec37c0f.jsonl |
| DSH | 通过，158.21 秒 | taskboard-f73f465d9c211a8ca4ab2f91ba0ba6d8 |

每次验收执行两轮：

1. Agent 从原卡读取任务，创建 proof.txt，结果回写并移到评审。
2. 主验收从卡片正文移除第一轮口令，关闭并重启看板，保留同一地址与数据库。
3. 新评论要求从原对话找出口令，更新文件、写结果评论、移回评审。
4. 发送两次同一评论跟进请求，检查原会话 ID 保持一致、文件内容正确、Agent 已回写结果及状态。
5. Codex/Pi 额外注入无效渠道配置，确认跟进报错且评论保留；恢复配置后可提交。

复现命令（配置文件需包含本机实际可用连接）：

```sh
TASKBOARD_LIVE_FOLLOWUP=codex TASKBOARD_LIVE_RUNTIME_CONFIG=/path/to/runtimes.json go test ./internal/service -run '^TestLiveCommentContinuation$' -v -count=1 -timeout=6m
# TASKBOARD_LIVE_FOLLOWUP 也可为 pi 或 dsh
```

前期 Codex 探针没有完成卡片回写，不能视为成功。检查真实会话记录定位到未传递实例地址、未安装 tb、workspace-write 默认禁止网络。现实现为 CLI 注入看板地址及执行身份、按次启用网络访问；验收工作区明确自己的看板地址，并允许使用 HTTP 回写。最终成功记录见上表。文件写入权限仍为 workspace-write。

## 自动检查

- go test ./internal/executor ./internal/service ./internal/client ./internal/store ./cmd/tb ./cmd/taskboard：相关包通过；新增 CLI 跟进测试最初发现成功响应解码 nil 的问题，修复后 cmd/tb 重新通过。
- go test -race ./internal/executor ./internal/service -run 'Test(CLIComment|Continue|PiMissing|DesktopFollowup|CodexResume)' -count=1：通过。
- go vet 对以上六个包：通过。
- git diff --check：通过。
- code-modify-safe 检查修改的 Go 文件：通过；Go 语义以编译、测试和 vet 为准。
- 原生 UI 测试实际点击普通发送、发送并跟进、已保存评论的跟进按钮；检查失败后评论仍在。人工查看过渲染截图。

故障测试通过真实 socket 协议的模拟桌面端覆盖：旧 turn.end 不算新一轮完成、回执不确定不重发、重启仅恢复跟踪。CLI 替身记录真实进程参数，验证精确会话恢复、跨重启去重、原卡状态不由派发器修改、非默认看板地址与身份传递。

## 范围与限制

- 三渠道均做了真实模型的成功路径及空闲后重启接续。Codex/Pi 真实注入了提交前配置故障。
- 桌面回执丢失和运行中 CLI 重启属于模拟故障覆盖，未对真实模型注入断网或崩溃。
- 服务重启发现尚未确定结束的 CLI 执行时标记 disconnected，不自动重跑；须人工核对原会话，当前未提供自动解除不确定状态的入口。
- 历史 Codex/Pi 派发没有保存会话 ID，无法回填原始会话。
- 代码在工作区，未替换已运行 App 或既有二进制。
- 模型路由团队技能要求的 Codex App 后台任务工具不可用，本轮后台团队任务数为 0；主 Agent 完成实现和检查。上表 Luna 是实际渠道验收使用的模型，不能算独立审查 Worker。

