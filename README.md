# taskboard-native

MyGo 原生看板客户端。数据层由本应用自持；DSH 插件不是运行时依赖。以本服务为唯一写者，初始用 SQLite 种子，GUI / CLI / MCP 都是它的客户端。

目标：轻量、MyGo 原生 UI（非 WebView 外壳）、CLI + MCP 供代理读写、按项目作用域查询、写入经 SSE 实时推到已打开的窗口（不轮询）。

## 运行

```bash
# 看板窗口（内建启动本地服务，127.0.0.1:47826）
go run ./cmd/taskboard

# 代理接口
go run ./cmd/tb --agent myagent list --project local
go run ./cmd/tb --agent myagent add "新任务" --project local
go run ./cmd/tb move <taskID> 进行中
go run ./cmd/taskboard-mcp            # stdio MCP，工具见 docs/contracts.md
```

环境变量：`TASKBOARD_ADDR`、`TASKBOARD_DB`、`TB_AGENT` / `TASKBOARD_AGENT`。`TASKBOARD_SLOW=<duration>` 仅为加载态截图用的测试钩子。

## 文档

- [docs/product-direction.md](docs/product-direction.md) · [architecture.md](docs/architecture.md) · [realtime-sync.md](docs/realtime-sync.md)
- [docs/contracts.md](docs/contracts.md)：HTTP/SSE/CLI/MCP 契约与写入边界（改接口先改这里）
- [docs/design.md](docs/design.md)：视觉与交互规格（tokens、加载态映射、明暗、无障碍）
- [docs/design-review.md](docs/design-review.md)：设计验收报告与复验记录
- [docs/review-brief.md](docs/review-brief.md)：验收环境与流程

## 校验

```bash
go build ./... && go vet ./... && go test ./...
```

免窗口渲染图（1280×800，明暗/空态/加载态）：`docs/assets/render/`；验收截图：`docs/assets/review/`。
