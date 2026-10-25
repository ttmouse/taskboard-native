# Agent 读取闭环独立检查

检查范围：本轮 `internal/service`、`cmd/tb`、`cmd/taskboard-mcp` 读取接口，以及 `model.Project.WorkspacePath` 和 `internal/store` 迁移。按要求忽略动效相关改动；未修改实现、测试或其他文档。

## 结果

未发现本轮范围内可复现的缺陷。

- `POST /api/projects` 在路径非空时检查绝对路径、清理路径并确认目标是现存目录；省略路径仍调用空路径创建。服务只持久化路径并建立项目和默认列，没有创建工作区或触发执行（`internal/service/server.go:272-299`、`internal/store/mutations.go:356-394`）。
- 项目查询从存储读取 `workspace_path`，项目列表和 board 都序列化同一个 `model.Project`；旧项目空路径维持兼容。迁移仅在列不存在时添加带空字符串默认值的列，测试覆盖旧项目及任务重开后仍可读取（`internal/store/store.go:138-161`、`internal/store/queries.go:68-97,107-125`、`internal/store/project_workspace_test.go:63-117`）。
- 列筛选先验证该列属于指定项目，任务查询始终限定在 `projectId`；负责人精确匹配后再分页。limit/offset 和 summary 参数按约定校验，摘要使用专用结构体，空结果保持数组形态（`internal/service/server.go:311-439,441-460`、`internal/store/queries.go:146-171`）。
- CLI 将过滤和摘要参数传给 API；摘要 JSON 直接输出服务投影，没有先加载整板或在本地截断。`show` 直接读取单卡接口并保留 JSON 原文；人类输出包含正文和评论（`cmd/tb/main.go:143-260,263-319,772-779`）。
- MCP 的 `list_tasks` 可选参数与 API 名称对应，默认仍按旧调用省略可选参数；新增 `get_task` 和 `create_project` 转发到对应 API。单卡内容原样返回，项目列表原样保留路径（`cmd/taskboard-mcp/main.go:195-227,291-351`）。

## 验证

- `go test ./internal/store ./internal/service ./cmd/tb ./cmd/taskboard-mcp`：通过。
- `go build` 分别构建 CLI 与 MCP 到独占 `/tmp/taskboard-read-check-j9zbl6bds5md9iyjblgrul3j-*` 路径：通过。
- `python3 scripts/check-agent-read.py --tb ... --mcp ...`：PASS。脚本使用随机端口和临时数据库，验证项目路径列表及 board 持久化、列/负责人过滤、分页、摘要字段、空结果、单卡正文和评论、MCP 闭环及 HTTP 参数错误；未使用正式服务或用户数据库。

本报告为只读审查结论；没有对产品之外的调用语义或并发分页快照作额外保证。
