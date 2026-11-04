---
id: "OP-116"
title: "任务关系按钮把内部 key 和任务 ID 当作显示文字"
slug: "任务关系按钮把内部-key-和任务-id-当作显示文字"
status: "inbox"
impact: "medium"
effort: "S"
category: "product"
created: "2026-11-04"
evidence: "cmd/taskboard/view.go:2057-2080,2155-2171; /Users/douba/go/pkg/mod/github.com/egoist/mygo@v0.6.3/ui/api_gen.go:744-754; /Users/douba/go/pkg/mod/github.com/egoist/mygo@v0.6.3/ui/element.go:1295-1297"
evidence_status: "source-confirmed-rendered-label-mismatch"
evidence_note: "MyGo ui.Button(c,label) 会把 label 作为按钮可见子文本；node.Label 只保存辅助技术名称。关系区打开按钮传入 keyPrefix+\"open\"+relation.ID，再设置 Label(\"#number title\")；删除按钮传入 keyPrefix+\"rm\"+relation.ID，再设置 Label(\"×\")。因此可见按钮文字实际是 sub-open{ID}/blocks-open{ID}/blocker-open{ID} 与对应 rm 字符串，而非预期的人类任务名或乘号。relation 数组分别渲染副任务/阻塞/被阻塞关系。OP-002 覆盖这些控件的辅助技术读名，但未覆盖可见文字错误；尚未运行 UI 截图验证。"
verification: ""
resolution: ""
---

## 现状与证据

展开任务详情的关系区后，每条关联任务都渲染打开与移除按钮。当前用 Button 的首参承担内部 key 的作用，但该 API 首参是显示标签；`.Label(...)` 不会替换它。用户可能看到关系类型前缀和内部任务 ID，无法快速辨认关联卡片，也无法看到预期的紧凑删除图标。

## 建议方向

用 ButtonBase 配合稳定的 c.Key(keyPrefix+action+relation.ID) 构造控件 key，再用 Children 提供可见的人类标签/图标；将读屏名称单独设为含关系类型及目标编号/标题的 Label，和 OP-002 一并验收。

## 验收标准

在父任务、子任务、blocks 和 blocked_by 关系行中，打开按钮可见文本为人类可读的任务号/标题，删除按钮可见 ×；界面不再显示内部 key 前缀或完整 task ID。Accessibility 树仍能区分关系类型与目标对象，所有按钮可用原有点击动作，添加/删除后关系状态及时刷新；以 UI 截图或运行时可访问性树验证。

## 决策记录

- 初始状态：inbox（待筛选，未经批准实施）。
