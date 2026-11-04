# 错误处理审计 · taskboard-native · 2026-10-31

对照范围：`cmd/taskboard`（board.go / view.go / git_commit.go / detail_images.go 等）+ `internal/client`。
原则：**只列缺口与优先级，不改代码**。成功路径与确认框（删除项目/评论、版本冲突）已有基础覆盖，下文只写差距。

更新：2026-10-31 Asia/Shanghai

---

## 总览

| 优先级 | 条数 | 主题 |
|--------|------|------|
| 高 | 3 | toast 类型误用、静默失败、空列无引导 |
| 中 | 4 | 原始 err 直出、部分成功不一致、图片加载空白、冲突后静默刷新失败 |
| 低 | 3 | prefs 写失败静默、Subscribe 启动错误丢弃、首帧前 toast 丢失 |

已做得较好（对照用，非缺口）：
- `loadErr` 全页「看板没能加载」+ 重试
- `offlineBanner`「本地服务未连接，正在重试…」+ 重试按钮；SSE Offline 指数退避
- 校验类 toast：空标题 / 无项目 / 无入口列 / 无工作目录 / 未知渠道
- 冲突：`recover` → AlertDialog「使用最新版本 / 保留我的修改」
- 部分成功已有范例：「已创建，关联失败」「评论已保存，AI 跟进失败」
- Git 主路径：跳过 / 失败 / 无改动 / 成功均有 toast
- 图片粘贴：超限、服务不支持、上传失败均有 toast

---

## 高优先级

### 1. 成功/中性提示一律走 error 型 toast
- **位置**：`board.toast`（board.go ~1271）固定 `Type: "error"`；`infoToast` 存在但几乎未被成功路径调用。
- **现象**：`已创建` / `已派发` / `已提交 Git` / `已保存` / `自动承接已开启` 等成功文案以错误样式弹出，干扰感知、易误判。
- **建议**：成功改 `info`/`success`；仅失败走 `toast`；或拆 `okToast`。

### 2. Git 验收提交后写评论静默失败
- **位置**：`git_commit.go` ~50：`_, _ = b.api.AddComment(taskID, "验收完成自动提交 "+sha)`
- **现象**：commit 已 toast「已提交 Git」，评论写入失败用户无感知；卡片时间线缺一条记录。
- **建议**：失败时补 toast（可沿用「已提交，评论写入失败」部分成功句式）。

### 3. 横板空列无空态文案
- **位置**：`columnView`：空列仍渲染空白 Scroll；竖板折叠空列只留表头。`TestRenderEmpty` 明确断言**不出现**「还没有任务 / 添加任务，或拖入」。
- **现象**：新用户/空项目横板各列空白，无「从这里加任务」指引；依赖顶栏/快捷添加，发现成本高。
- **建议**：空列轻量占位（muted 一行 + 可选点按打开创建），与测试约定一并更新。

---

## 中优先级

### 4. 用户可见 toast 大量直出 `err.Error()`
- **位置**：移动/归档/关系/创建/派发/评论/删除/保存/图片上传等失败分支。
- **现象**：HTTP/客户端英文或技术串直接进 UI（如 status、URL 片段），与中文产品语气不一致；偶发过长撑爆 toast。
- **建议**：映射常见 `HTTPError`/`ConflictError`/网络错为短中文；原文可折叠到日志或「详情」。

### 5. 「部分成功」策略不一致
- **已有**：创建后关联失败、评论后 AI 跟进失败 → 明确双态 toast。
- **缺口**：Git 评论（高-2）、图片插正文时「正文已变化」有 toast 但加载失败见下条。
- **建议**：约定「主操作成功 + 附属失败」一律双态文案，禁止附属 `_ =`。

### 6. 详情/评论内联图加载失败 → 空白无提示
- **位置**：`detail_images.go` 加载路径 `err != nil` 时不画图、无 toast（上传失败有 toast，展示失败无）。
- **现象**：坏链/服务短暂不可用时正文出现空洞，用户不知是权限、网络还是格式问题。
- **建议**：占位块 + muted「图片加载失败」；可选点击重试。

### 7. 非冲突失败时 `recover` 二次拉取失败静默
- **位置**：`recover`：非 `ConflictError` 先 `toast("操作失败")`，再 `Task(id)`；`ferr != nil` 时不刷新也不追加提示。
- **现象**：用户看到失败 toast，但详情/看板可能仍是过期乐观状态。
- **建议**：拉取失败时 `load()` 或明确「未能同步最新版本」。

---

## 低优先级

### 8. `saveUIPrefs` 全链路 `_ =`
- 折叠列、主题、布局等写盘失败无提示；重启后「设置丢了」难排查。可接受为低；至少 stderr/一次 info toast。

### 9. `Subscribe` 启动错误被丢弃
- `board.go`：`_ = b.api.Subscribe(...)`；依赖后续 Offline 状态机。启动瞬间失败可能短窗口无横幅。

### 10. 首帧前 `frameCtx == nil` 时 toast 丢弃
- `toast`/`infoToast` 在 `ctx()==nil` 直接 return；注释已提到启动早期失败。罕见，但派发/加载竞态可能吞提示。

---

## 建议落地顺序（不实施，仅排队参考）

1. **P0** toast 类型分流（成功 ≠ error）— 改动面小、体感最大  
2. **P0** Git 评论失败可见化  
3. **P1** 空列占位文案（需同步测试）  
4. **P1** 错误文案中文化/截断 + 部分成功约定扫尾  
5. **P2** 图片加载占位、prefs/Subscribe/首帧 toast 加固  

---

## 证据索引

| 主题 | 文件:附近 |
|------|-----------|
| toast Type=error | board.go:1271 |
| infoToast 闲置 | board.go:1280 |
| Git 评论静默 | git_commit.go:48-51 |
| 空列无文案 | view.go:columnView；render_test.go:TestRenderEmpty |
| recover / 冲突框 | board.go:2045；view.go:conflictView |
| offline / loadErr | view.go:995；view.go:1020 |
| 图片上传 vs 展示 | detail_images.go:60-84 vs 218-360 |

---

*只读审计。完成后队列第 2 项标完成。*
