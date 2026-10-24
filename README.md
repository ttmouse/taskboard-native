# taskboard-native

MyGo 原生看板客户端（设计文档阶段）。数据层由本应用自持；DSH 插件不是运行时依赖。初始可用既有 SQLite 作种子，之后以本服务为权威源。

目标：轻量（体积与内存对标 PULSE，几 MB 级）、MyGo 原生 UI、CLI + MCP 供代理读写、按项目作用域查询。

详见 [docs/](docs/)。
