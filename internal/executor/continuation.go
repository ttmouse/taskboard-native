package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"taskboard-native/internal/model"
	"taskboard-native/internal/store"
)

// Continue sends a saved comment to the exact original session. It never creates
// a replacement conversation, and only explicit calls here can wake an agent.
func (d *Dispatcher) Continue(taskID, commentID, by string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return &store.InvalidError{Msg: "看板执行服务已关闭"}
	}
	if d.recoveryErr != nil {
		return d.recoveryErr
	}
	task, err := d.store.Task(taskID)
	if err != nil {
		return err
	}
	if task.Archived {
		return &store.InvalidError{Msg: "任务已归档，不能继续执行"}
	}
	found := false
	var mentioned model.Comment
	for _, cm := range task.Comments {
		if cm.ID == commentID {
			found = true
			mentioned = cm
			break
		}
	}
	if !found {
		return store.ErrNotFound
	}
	status, err := d.store.FollowupStatus(taskID, commentID)
	if err != nil {
		return err
	}
	if status == "sent" {
		return nil
	}
	if status == "unknown" {
		return ErrInProgress
	}
	if status == "reserved" {
		if _, busy := d.inflight[taskID]; busy {
			return nil
		}
		return ErrInProgress
	}
	if _, busy := d.inflight[taskID]; busy {
		return ErrInProgress
	}
	run, err := d.store.Execution(taskID)
	if err != nil {
		return err
	}
	if run != nil && run.Active() && !run.Stalled() {
		return ErrInProgress
	}
	// A comment may @ one channel. Addressing the channel that already owns the
	// card keeps that conversation; addressing a different one is a human
	// instruction to switch, so it opens a new session there instead of
	// silently reaching the wrong agent. No @ keeps the current session.
	runtime := ""
	switching := false
	if mention := strings.TrimSpace(mentioned.Mention); mention != "" {
		if !KnownRuntime(mention) {
			return &store.InvalidError{Msg: fmt.Sprintf("评论 @ 的执行渠道不可用：%s", mention)}
		}
		if run == nil || run.Channel() != mention || run.SessionID == "" {
			switching = true
			runtime = mention
			if run == nil {
				run = &model.Execution{TaskID: taskID}
			}
			run.Runtime = mention
			run.SessionID = ""
			run.PreviousTurnID = ""
		}
	}
	if !switching {
		if run == nil || run.SessionID == "" {
			return &store.InvalidError{Msg: "没有可接续的原会话；先在评论里 @ 一个执行渠道，或明确派发一次"}
		}
		runtime = run.Channel()
		if !KnownRuntime(runtime) {
			return &store.InvalidError{Msg: "原会话的执行渠道不可用"}
		}
	}
	project, err := d.store.Project(task.ProjectID)
	if err != nil {
		return err
	}
	if info, err := os.Stat(project.WorkspacePath); err != nil || !info.IsDir() {
		return &store.InvalidError{Msg: "项目工作目录不可用"}
	}
	ctx, cancel := context.WithCancel(context.Background())
	preflight, stopPreflight := context.WithTimeout(ctx, 10*time.Second)
	defer stopPreflight()
	ex := &execution{cancel: cancel, done: make(chan struct{})}
	text := fmt.Sprintf("使用 manage-taskboard-native 处理任务 %s 的评论 %s：读该评论，按其中要求执行并回写。不要调用 tb followup——这条消息就是它投递的。", taskID, commentID)
	var cmd *exec.Cmd
	var bridge *desktopBridge
	// Switching channels opens a session, so there is no original session to
	// check: runWithText does the rest of that path.
	if !switching {
		if runtime == RuntimeDSH {
			bridge, err = connectDesktop(preflight)
			if err == nil {
				err = bridge.requireIdle(preflight, run.SessionID)
			}
			if err == nil {
				var items []desktopItem
				items, err = bridge.snapshot(preflight, run.SessionID)
				if err == nil {
					run.PreviousTurnID = lastDesktopTurn(items)
					if run.PreviousTurnID == "" {
						err = fmt.Errorf("无法确认原会话上一轮记录，请先核对桌面会话")
					}
				}
			}
		} else if _, ok := herdrAddress(run.SessionID); ok {
			var remote herdrStatus
			remote, err = herdrGet(preflight, strings.TrimPrefix(run.SessionID, herdrSessionPrefix))
			if err == nil {
				state := herdrState(remote)
				if state != "idle" && state != "done" {
					err = fmt.Errorf("Herdr 原 terminal 当前状态为 %q；先在 Herdr 中处理后再跟进", state)
				}
			}
		} else {
			cmd, err = execCommand(ctx, project.WorkspacePath, text, runtime, d.runtimeConfigPath)
			if err == nil {
				err = d.prepareCLI(cmd, run, true)
			}
		}
	}
	if err != nil {
		cancel()
		if bridge != nil {
			bridge.Close()
		}
		return &store.InvalidError{Msg: err.Error()}
	}
	run.FollowupID = commentID
	run.Status = "submitting"
	run.Detail = "评论跟进已保存，等待原会话接收"
	reserved, err := d.store.ReserveFollowup(*run)
	if err != nil || !reserved {
		cancel()
		if bridge != nil {
			bridge.Close()
		}
		return err
	}
	d.inflight[taskID] = ex
	go func() {
		defer cancel()
		defer close(ex.done)
		defer func() { d.mu.Lock(); delete(d.inflight, taskID); d.mu.Unlock() }()
		definition, _ := runtimeDefinitionFor(runtime)
		if switching {
			// The comment addressed another channel: open a session there and
			// hand the same instruction over, keeping the follow-up record.
			d.runWithText(ctx, task, project.Name, project.WorkspacePath, runtime, by, text, commentID)
			return
		}
		if bridge != nil {
			defer bridge.Close()
			d.continueDesktop(ctx, bridge, task, run, text, definition.Label)
			return
		}
		if _, ok := herdrAddress(run.SessionID); ok {
			d.sendHerdrFollowup(ctx, taskID, definition.Label, by, run, text)
			return
		}
		d.comment(taskID, definition.Label, "正在原会话跟进评论 "+commentID+"（发起人 "+by+"）。")
		d.executeCLI(ctx, cmd, task, run, definition.Label, time.Now())
	}()
	return nil
}

func (d *Dispatcher) prepareCLI(cmd *exec.Cmd, run *model.Execution, resume bool) error {
	n := len(cmd.Args) - 2 // keep per-invocation configuration before -- and prompt
	prompt := cmd.Args[len(cmd.Args)-1]
	args := append([]string{}, cmd.Args[:n]...)
	switch run.Channel() {
	case RuntimeCodex:
		// The agent must reach this board's loopback HTTP API while retaining
		// workspace-only file writes. Codex disables network by default here.
		args = append(args, "-c", "sandbox_workspace_write.network_access=true")
		if resume {
			args = append(args, "resume", "--json", "--", run.SessionID, prompt)
		} else {
			args = append(args, "--json", "--", prompt)
		}
	case RuntimeClaude:
		// Claude Code takes the session id up front: --session-id for a new
		// conversation, --resume for the recorded one. It must be a UUID, and
		// it is persisted before the process starts like every other channel.
		if resume {
			if !isSessionUUID(run.SessionID) {
				return fmt.Errorf("Claude 原会话标识不可用，不能新建会话替代：%q", run.SessionID)
			}
			args = append(args, "--resume", run.SessionID, "--", prompt)
		} else {
			run.SessionID = newSessionUUID()
			args = append(args, "--session-id", run.SessionID, "--", prompt)
		}
	default: // Pi Agent
		if resume {
			if info, err := os.Stat(run.SessionID); err != nil || !info.Mode().IsRegular() {
				return fmt.Errorf("Pi 原会话文件不存在，不能新建会话替代")
			}
		} else {
			root := d.runtimeConfigPath + ".sessions"
			if d.runtimeConfigPath == "" {
				var err error
				root, err = os.MkdirTemp("", "taskboard-sessions-")
				if err != nil {
					return err
				}
			}
			if err := os.MkdirAll(root, 0700); err != nil {
				return err
			}
			run.SessionID = filepath.Join(root, fmt.Sprintf("%x.jsonl", randomDesktopID()))
		}
		args = append(args, "--session", run.SessionID, "--", prompt)
	}
	cmd.Args = args
	if !resume {
		return d.store.SaveExecution(*run)
	}
	return nil
}

// newSessionUUID builds the RFC 4122 v4 identifier Claude Code requires for
// --session-id, so a run's session can be resumed later by exact id.
func newSessionUUID() string {
	id := randomDesktopID()
	id[6] = id[6]&0x0f | 0x40
	id[8] = id[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16])
}

func isSessionUUID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, r := range id {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				return false
			}
		}
	}
	return true
}

// Codex JSONL announces identity before tool execution. Persist immediately,
// including when the process later fails; plain output is never parsed as an ID.
type sessionObserver struct {
	d       *Dispatcher
	run     *model.Execution
	pending []byte
	err     error
}

func (w *sessionObserver) Write(p []byte) (int, error) {
	if w.run.Channel() != RuntimeCodex {
		return len(p), nil
	}
	w.pending = append(w.pending, p...)
	for {
		i := bytes.IndexByte(w.pending, '\n')
		if i < 0 {
			break
		}
		var event struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
		}
		if json.Unmarshal(w.pending[:i], &event) == nil && event.Type == "thread.started" && strings.TrimSpace(event.ThreadID) != "" {
			if w.run.SessionID != "" && w.run.SessionID != event.ThreadID {
				w.err = fmt.Errorf("Codex 返回了不同会话，停止跟踪")
				return 0, w.err
			}
			w.run.SessionID = event.ThreadID
			if err := w.d.store.SaveExecution(*w.run); err != nil {
				w.err = err
				return 0, err
			}
		}
		w.pending = w.pending[i+1:]
	}
	if len(w.pending) > 8*1024*1024 {
		w.pending = nil
	}
	return len(p), nil
}
