# taskboard-native 优化项队列

这里记录有源码或运行时证据支撑的产品、性能、可靠性和开发体验优化。条目由 AI 提议进入 `inbox`；只有用户确认后才能改为 `accepted` 或 `doing`。

## 结构

- `items/YYYY-MM/OP-NNN-slug.md`：单条优化项，包含 YAML 风格 front matter 和四节正文。
- `INDEX.md` / `INDEX.html`：由 `python3 tools/backlog.py index` 生成，不手改。
- `tools/backlog.py`：唯一写入口，支持 `add`、`set`、`index`、`stats`、`dup`、`stale`。

## 状态

`inbox` → `accepted` → `doing` → `done`；无法继续用 `blocked`，不采纳用 `rejected`，暂缓用 `deferred`，同根因并入另一条用 `merged`（必须用 `-m "并入 OP-xxx"` 记录去向）。状态只能由 `backlog.py set` 修改。`done` 必须通过 `--verification` 提供验收证据路径；该字段与发现时的 `evidence` 分开保存，不能覆盖原始依据。`rejected` 与 `merged` 条目保留用于查重。

## 字段与分级

- `impact`: `high` / `medium` / `low`，按用户影响排序。
- `effort`: `S` / `M` / `L`，估算实现与验收工作量。
- `category`: `accessibility` / `performance` / `reliability` / `security` / `product` / `developer-experience`。
- `family`: 根因族，分诊单位。同一改动能关掉的条目归一族，索引按族排序。
- `evidence`: 可复核的文件行号、运行时观测或测量记录。

新增前先运行 `python3 tools/backlog.py dup <关键词>`，查重范围包含所有状态。没有证据的感觉不入库；同根因合并为一条。`inbox` 仅表示待筛选，不代表已批准实施。

## 分诊节奏

- 每周跑一次 `python3 tools/backlog.py stale --days 14`。默认只报告；确认后加 `--apply` 把超期未处理的条目转入 `deferred`。
- 新条目必须填 `family`。没有合适的族就新建一个，不要留空，也不要把不同根因塞进同一族。
- 族的数量就是决策单元的数量。族内先处理 `merged` 的合并，再按「错误数据 > 慢 > 难看」排序。

## 条目正文

每条必须包含：现状与证据、建议方向、验收标准、决策记录。索引只展示摘要，详情以条目文件为准。
