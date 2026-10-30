# 派发技能交接验收 · 2026-10-26

> 历史验收记录：后续已按用户要求改为执行 Agent 依据技能自行认领、验证和回写。本文中的程序自动迁移或只读执行者描述不再代表当前行为，当前契约见 `docs/contracts.md`。

派发说明现在显式要求加载 `manage-taskboard-native`，提供技能源文件路径、项目/任务 ID、工作目录和服务实际 URL。接收者先读取原卡及全部评论；自动派发模式由派发器负责认领、结果回写和状态迁移，接收者只读看板并完成本卡。

`TestLiveDispatchSkillHandoff` 使用真实 DSH 桌面会话和模型，运行 57.18 秒通过。隔离看板使用随机端口 59240，口令只存在于原卡评论，没有进入派发文本。模型生成的文件包含正确口令，结果回写并进入评审。

另外独立读取了桌面工具轨迹，确认以下操作都成功：

1. `skill` 加载 `manage-taskboard-native`。
2. `read` 读取 `/Users/douba/.agents/skills/manage-taskboard-native/SKILL.md`。
3. `bash` 以 curl 读取测试看板的 `/api/tasks/022a5f24897f580653dcdccd6c937a9c`。

具体工具调用见 `dispatch-skill-handoff-2026-10-26.json`。这验证了技能和原卡确实被读取，超出仅在提示词中出现技能名的静态检查范围。

构造派发说明的回归测试另行确认了自定义端口、任务 ID 和职责说明；派发/服务的竞态测试及 `go vet ./...` 通过。

补充：带技能说明的 `TestLiveDesktopRecoveryFlow` 再次通过，耗时 169.72 秒，覆盖鉴权拒绝、失败重派、断连、重启同会话接续、防重复派发及接收回执丢失后恢复。测试服务重启沿用原端口，保证派发文本中的原卡读取地址继续有效。证据见 `dispatch-skill-recovery-2026-10-26.json`。
