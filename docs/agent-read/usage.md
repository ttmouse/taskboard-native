# 给 Agent 的读取入口

目标：先找相关任务，再只读取要执行的那一张及其评论。服务默认地址为 `http://127.0.0.1:47826`；CLI 用 `--addr`、MCP 用 `TASKBOARD_ADDR` 指向其他实例。

## CLI

仓库已构建的入口为 `build/bin/tb` 与 `build/bin/taskboard-mcp`。下面的 `tb` 可替换为 `./build/bin/tb`；无需安装全局命令。

```sh
tb projects --json
tb project add "原生看板" --path /Users/douba/Projects/taskboard-native --json
tb list --project PROJECT_ID --column 待办 --assignee luna --limit 20 --offset 0 --summary --json
tb show TASK_ID --json
```

`--column` 同时接受列名和列 ID。`--assignee` 精确匹配，可省略。列表 JSON 摘要没有正文和评论；`show` 返回单卡正文、评论及当前版本。去掉 `--json` 适合人阅读。默认旧 `tb list --project ID` 仍可使用。

项目路径是已存在的本机绝对目录；省略 `--path` 仍可创建名称项目。创建项目仅建立项目及默认四列，不创建工作目录，不初始化 Git，也不启动 Agent。

## MCP

1. `list_projects {}` 获取项目 ID、列 ID 与工作路径。
2. 如需新项目：`create_project {"name":"原生看板","workspacePath":"/Users/douba/Projects/taskboard-native"}`。
3. `list_tasks {"projectId":"PROJECT_ID","columnId":"COLUMN_ID","limit":20,"offset":0,"summary":true}` 只读相关摘要。
4. `get_task {"id":"TASK_ID"}` 读取正文及全部评论，确认当前要求后再执行。

写入仍使用现有 version 保护；本轮没有新增自动认领、租约或调度。`workspacePath` 提供定位信息，不代表自动执行授权。

## 分页与兼容

过滤先于分页，排序与看板一致。limit 为 1..200，offset 为非负整数且与 limit 一起使用。偏移分页反映当前数据；任务在读取期间被移动或新增时，应重新读取，不把它当作冻结快照。结果少于 limit 时已到当前末尾。

旧项目没有工作路径也能继续读取。旧数据库升级只添加 workspace_path，项目和任务保留。

## 独立验证

```sh
go build -o /tmp/taskboard-read-tb ./cmd/tb
go build -o /tmp/taskboard-read-mcp ./cmd/taskboard-mcp
python3 scripts/check-agent-read.py --tb /tmp/taskboard-read-tb --mcp /tmp/taskboard-read-mcp
```

此脚本自行启动随机端口服务及临时数据库，检查 HTTP、CLI、MCP 的完整读取路径，不使用默认看板数据库。
