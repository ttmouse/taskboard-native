package executor

import (
	"strings"

	"taskboard-native/internal/model"
)

type taskType string
type executionMode string

const (
	taskTypeCodeGeneration taskType = "code_generation"
	taskTypeSimpleChange   taskType = "simple_change"

	executionModeSingleAgent executionMode = "single_agent"
	executionModeCodeReview  executionMode = "codex_claude_review"
)

// Keep this mapping in code: task authors do not need to choose a workflow.
var taskExecutionModes = map[taskType]executionMode{
	taskTypeCodeGeneration: executionModeCodeReview,
	taskTypeSimpleChange:   executionModeSingleAgent,
}

// Code-generation markers deliberately require an explicit implementation or
// software-structure cue. Ambiguous cards fall back to one agent rather than
// paying for or blocking on a review stage the task may not need.
var codeGenerationMarkers = []string{
	"实现", "新增", "开发", "构建", "接入", "集成", "接口", "模块", "流水线",
	"代码生成", "pipeline", "api ", "herdr 派发路径",
}

func classifyTask(task model.Task) taskType {
	text := strings.ToLower(task.Title + "\n" + task.Description)
	for _, marker := range codeGenerationMarkers {
		if strings.Contains(text, marker) {
			return taskTypeCodeGeneration
		}
	}
	return taskTypeSimpleChange
}

// BuildDispatchTaskText adds a hidden execution policy to the normal card
// handoff. Only Codex code-generation dispatches start the Claude review stage;
// other explicitly selected runtimes keep their existing single-agent flow.
func BuildDispatchTaskText(task model.Task, runtime string) string {
	base := buildTaskTextForRuntime(task, runtime)
	typeOfTask := classifyTask(task)
	mode, ok := taskExecutionModes[typeOfTask]
	if !ok || mode != executionModeCodeReview || runtime != RuntimeCodex {
		return base
	}
	return base + `

系统已按卡片内容选择执行模式：Codex 实现 + Claude Code 评审。请按以下顺序完成同一张卡，不要询问用户是否需要流水线：
1. 你负责实现和项目要求的本地验证；先不要写最终结果评论或把卡移到「等你」。
2. 读取 hc --skill 与 hc templates。用 hc run 启动一个 Claude Code 评审子终端，工作目录为本项目，任务只做只读审查，不得创建子 Agent。把父终端地址传给评审 Agent，要求它结束时用 hc send 回报 findings 和对应文件/行号。
3. 等待并读取评审结果。修复确认的问题并重跑受影响的验证；之后按 manage-taskboard-native 的流程回写一次最终结果并移入「等你」。若 hc/Claude 不可用或评审无法完成，明确记录阻塞，不得把代码冒充为已通过流水线评审。
`
}
