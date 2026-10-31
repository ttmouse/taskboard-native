# 自动承接本地日志

Taskboard 服务会将自动承接过程追加到当前数据库旁的
`taskboard.db.auto-handoff.jsonl`。配置了 `TASKBOARD_DB` 时，日志跟随该数据库，
文件名为 `<数据库路径>.auto-handoff.jsonl`。日志是 UTF-8 JSON Lines，每行一条
独立事件，可直接交给 Agent 按 `projectId`、`taskId` 和 `timestamp` 过滤分析。

事件包含承接开关请求与停止、队列扫描结果、卡片认领和派发、服务重启后的恢复、
执行会话收敛、卡片移入「等你」及失败类别。`errorType` 仅记录 Go 错误类型；
`reason` 使用受控原因，不包含错误原文。日志不记录任务标题或正文、Agent 输出、
工作目录、凭据或服务端点。事件时间戳采用 UTC RFC 3339 纳秒格式。

例如，可读取最近事件并按项目筛选：

```sh
tail -n 100 "$HOME/Library/Application Support/taskboard/taskboard.db.auto-handoff.jsonl"
```

实际数据库目录由应用数据目录和 `TASKBOARD_DB` 决定。日志写入失败不会中断任务
派发；服务会在自身日志中记录写入错误。
