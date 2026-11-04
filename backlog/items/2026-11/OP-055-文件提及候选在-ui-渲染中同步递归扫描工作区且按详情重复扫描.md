---
id: "OP-055"
title: "文件提及候选在 UI 渲染中同步递归扫描工作区且按详情重复扫描"
slug: "文件提及候选在-ui-渲染中同步递归扫描工作区且按详情重复扫描"
status: "inbox"
impact: "medium"
effort: "M"
category: "performance"
created: "2026-11-04"
evidence: "cmd/taskboard/file_mentions.go:36-75; cmd/taskboard/mention_menu.go:148-153,171-183; cmd/taskboard/board.go:1412-1427; cmd/taskboard/view.go:2199"
evidence_status: "current-source-confirmed"
evidence_note: "文件提及搜索首次出现非空 @query 时，filteredWorkspaceMentions/taskFileMentionMenu 在 UI 构建路径中直接调用 workspaceMentionTargets。该函数递归 os.ReadDir 到深度 8 并排序，最多返回 500 项；但 os.ReadDir 会先物化每个被访问目录的全部条目，因此 500 项上限不限制单个超大目录的读取成本。结果缓存在 detail/form 自身；openDetail 每次都新建 detail，故不同任务详情在同一 workspace 使用 @ 搜索时会重复扫描。扫描没有异步化或跨详情共享缓存，潜在代价是输入响应卡顿；实际影响依目录规模而异，尚未测量。"
verification: ""
resolution: "- 2026-11-04: 补充已保存文件提及的渲染路径（2026-11-04）：detailMarkdown 每次调用都会先运行 expandWorkspaceMentions 与 linkifyAbsolutePaths，再交给 MyGo Markdown View（cmd/taskboard/detail_images.go:270-303）。commentBodyView 在每次详情视图构建中对每条评论/评论片段调用 detailMarkdown（cmd/taskboard/body_table_ui.go:151-170,235-239）。expandWorkspaceMentions 对每个合法 @{relativePath} token 在 UI 调用路径里执行 filepath.Abs/EvalSymlinks、Rel 安全校验、再次 EvalSymlinks 及 os.Stat 判断文件/目录（cmd/taskboard/file_mentions.go:189-248）；即使 Markdown View 后续复用解析状态，这些文件系统元数据查询和完整文本转换仍逐帧重复。缓存可按原始文本与 workspace 根路径复用，并在二者变化时失效；验收需测量带多个有效 token 的长正文/多评论稳定帧，不重复 EvalSymlinks/Stat/字符串转换，同时文件类型标签与路径安全校验保持正确。\n- 2026-11-04: 补充候选缓存失效边界（2026-11-04）：quickFileMentionTargets 仅比较 quickFileMentionProjectID 与 b.cur；同一项目更新 workspacePath 后，即使 ProjectChange 已刷新 b.projects，仍复用旧目录的文件候选（cmd/taskboard/mention_menu.go:240-259; cmd/taskboard/board.go:773-801）。任务 form 的缓存同样只比较 fileMentionProjectID 与 projectID；detail 只用 nil 表示未加载，没有记录扫描根路径（mention_menu.go:145-183）。此外工作区文件新增/删除后，在当前表单或详情持续打开时不会主动失效候选快照。优化建议须同时解决项目 ID、workspacePath 与目录内容变化的失效策略；验收覆盖同项目切换工作目录、文件增删及多详情复用，确认新路径候选出现、旧路径候选不残留。"
---



## 现状与证据

评论、任务编辑和快速新建的文件提及首次搜索都会枚举 workspace 文件路径；候选查询触发在 UI 构建函数中，扫描结果只缓存到当前 detail 或 form。

## 建议方向

将目录枚举移出 UI 渲染热路径，使用项目级可复用缓存并以后台任务刷新；在扫描未完成时保留可用界面和键盘交互，限制扫描成本并处理 workspacePath 变化/失效。保留排除隐藏目录、符号链接、依赖目录与现有候选排序语义。

## 验收标准

对包含单个超大目录及多层目录的大型 workspace 打开详情后输入 @query，UI 线程不执行同步递归扫描；同一项目多个任务详情复用同一份缓存，workspacePath 改变后能刷新；候选限制、排序、隐藏路径排除与键盘选择行为保持一致，并记录首次候选延迟及 UI 卡顿测量。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。

补充已保存文件提及的渲染路径（2026-11-04）：detailMarkdown 每次调用都会先运行 expandWorkspaceMentions 与 linkifyAbsolutePaths，再交给 MyGo Markdown View（cmd/taskboard/detail_images.go:270-303）。commentBodyView 在每次详情视图构建中对每条评论/评论片段调用 detailMarkdown（cmd/taskboard/body_table_ui.go:151-170,235-239）。expandWorkspaceMentions 对每个合法 @{relativePath} token 在 UI 调用路径里执行 filepath.Abs/EvalSymlinks、Rel 安全校验、再次 EvalSymlinks 及 os.Stat 判断文件/目录（cmd/taskboard/file_mentions.go:189-248）；即使 Markdown View 后续复用解析状态，这些文件系统元数据查询和完整文本转换仍逐帧重复。缓存可按原始文本与 workspace 根路径复用，并在二者变化时失效；验收需测量带多个有效 token 的长正文/多评论稳定帧，不重复 EvalSymlinks/Stat/字符串转换，同时文件类型标签与路径安全校验保持正确。

补充候选缓存失效边界（2026-11-04）：quickFileMentionTargets 仅比较 quickFileMentionProjectID 与 b.cur；同一项目更新 workspacePath 后，即使 ProjectChange 已刷新 b.projects，仍复用旧目录的文件候选（cmd/taskboard/mention_menu.go:240-259; cmd/taskboard/board.go:773-801）。任务 form 的缓存同样只比较 fileMentionProjectID 与 projectID；detail 只用 nil 表示未加载，没有记录扫描根路径（mention_menu.go:145-183）。此外工作区文件新增/删除后，在当前表单或详情持续打开时不会主动失效候选快照。优化建议须同时解决项目 ID、workspacePath 与目录内容变化的失效策略；验收覆盖同项目切换工作目录、文件增删及多详情复用，确认新路径候选出现、旧路径候选不残留。
