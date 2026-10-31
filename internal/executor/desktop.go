package executor

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"taskboard-native/internal/model"
)

// The desktop bridge publishes a private, rotating loopback endpoint. Never
// launch a second Host or fall back to headless when the desktop is unavailable.
type desktopEndpoint struct {
	Version int    `json:"version"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Token   string `json:"token"`
}

type desktopBridge struct {
	net.Conn
	reader *bufio.Scanner
	seq    int
}

func desktopSelections(configPath string) (map[string]string, error) {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("请在看板渠道配置 %s 中为 dsh 指定 provider 和 model：%w", configPath, err)
	}
	var connections map[string]runtimeConnection
	if err := json.Unmarshal(raw, &connections); err != nil {
		return nil, fmt.Errorf("看板渠道配置无效：%w", err)
	}
	c := connections[RuntimeDSH]
	if strings.TrimSpace(c.Provider) == "" || strings.TrimSpace(c.Model) == "" {
		return nil, fmt.Errorf("DSH 桌面派发需要在看板渠道配置中指定 provider 和 model")
	}
	var effort any
	if c.ReasoningEffort != "" {
		effort = c.ReasoningEffort
	}
	model, _ := json.Marshal([]any{c.Provider, c.Model, effort})
	return map[string]string{"model": "dsh:model:" + base64.RawURLEncoding.EncodeToString(model), "permission": "dsh:permission:" + base64.RawURLEncoding.EncodeToString([]byte("workspace-write"))}, nil
}

func connectDesktop(ctx context.Context) (*desktopBridge, error) {
	path := os.Getenv("TASKBOARD_DSH_ENDPOINT")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, ".agents-anywhere", "dsh-bridge", "endpoint.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("无法连接 DSH 桌面端，请启动 DeepSeek Harness 并启用 Agents Anywhere 桥接组件：%w", err)
	}
	var endpoint desktopEndpoint
	if json.Unmarshal(raw, &endpoint) != nil || endpoint.Version != 1 || endpoint.Host != "127.0.0.1" || endpoint.Port < 1 || endpoint.Port > 65535 || endpoint.Token == "" {
		return nil, fmt.Errorf("DSH 桌面端连接配置无效，请在桌面端重新启用桥接组件")
	}
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(endpoint.Host, strconv.Itoa(endpoint.Port)))
	if err != nil {
		return nil, fmt.Errorf("DSH 桌面端连接失败，请检查桌面端和桥接组件是否运行：%w", err)
	}
	b := &desktopBridge{Conn: conn, reader: bufio.NewScanner(conn)}
	b.reader.Buffer(make([]byte, 4096), 8*1024*1024)
	var hello struct {
		Features struct {
			ReadOnly bool `json:"readOnly"`
		} `json:"features"`
	}
	if err := b.call(ctx, "initialize", map[string]any{"authToken": endpoint.Token, "protocolVersion": "1.0", "runtime": "dsh", "sessionNamespace": "taskboard-native"}, &hello); err != nil {
		conn.Close()
		return nil, err
	}
	if hello.Features.ReadOnly {
		conn.Close()
		return nil, fmt.Errorf("DSH 桌面桥接处于只读模式，无法接收任务")
	}
	return b, nil
}

func (b *desktopBridge) call(ctx context.Context, method string, params any, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline := time.Now().Add(65 * time.Second)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	if err := b.SetDeadline(deadline); err != nil {
		return err
	}
	// Also unblock network reads immediately when the board stops tracking.
	stop := context.AfterFunc(ctx, func() { b.Close() })
	defer stop()
	b.seq++
	request := map[string]any{"jsonrpc": "2.0", "id": b.seq, "method": method, "params": params}
	if err := json.NewEncoder(b.Conn).Encode(request); err != nil {
		return fmt.Errorf("发送桌面请求失败：%w", err)
	}
	for b.reader.Scan() {
		var reply struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(b.reader.Bytes(), &reply); err != nil {
			return fmt.Errorf("桌面桥接响应无效：%w", err)
		}
		if reply.ID != b.seq {
			continue
		}
		if reply.Error != nil {
			return fmt.Errorf("DSH 桌面请求 %s 失败：%s", method, reply.Error.Message)
		}
		if len(reply.Result) == 0 {
			return fmt.Errorf("DSH 桌面请求 %s 未返回结果", method)
		}
		return json.Unmarshal(reply.Result, result)
	}
	return fmt.Errorf("DSH 桌面连接中断：%v", b.reader.Err())
}

type desktopItem struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Role    string `json:"role"`
	TurnID  string `json:"turnId"`
	Content struct {
		Text string `json:"text"`
	} `json:"content"`
}

func (b *desktopBridge) result(ctx context.Context, session string) (string, bool, error) {
	return b.resultAfter(ctx, session, "")
}

func (b *desktopBridge) snapshot(ctx context.Context, session string) ([]desktopItem, error) {
	var items []desktopItem
	params := desktopSessionParams(session)
	for {
		var page struct {
			SessionID        string        `json:"sessionId"`
			Items            []desktopItem `json:"items"`
			NextCursor       *string       `json:"nextCursor"`
			SnapshotComplete bool          `json:"snapshotComplete"`
		}
		if err := b.call(ctx, "session.getSnapshot", params, &page); err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
		if page.NextCursor == nil {
			if !page.SnapshotComplete {
				return nil, fmt.Errorf("桌面会话记录不完整，无法确认任务结果")
			}
			break
		}
		params["cursor"] = *page.NextCursor
		params["sessionId"] = page.SessionID
	}
	return items, nil
}

func lastDesktopTurn(items []desktopItem) string {
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].Type == "turn.end" {
			return items[i].TurnID
		}
	}
	return ""
}

func (b *desktopBridge) resultAfter(ctx context.Context, session, previous string) (string, bool, error) {
	items, err := b.snapshot(ctx, session)
	if err != nil {
		return "", false, err
	}
	// An idle snapshot before the first turn starts is not completion. Only
	// a recorded terminal turn, followed by an idle Host, can finish dispatch.
	lastEnd := -1
	for i, item := range items {
		if item.Type == "turn.end" {
			lastEnd = i
		}
	}
	if lastEnd < 0 {
		return "", false, nil
	}
	end := items[lastEnd]
	if previous != "" && end.TurnID == previous {
		return "", false, nil
	}
	if end.Status != "done" {
		return "", false, &desktopSessionError{message: fmt.Sprintf("DSH 桌面会话结束，状态：%s", end.Status)}
	}
	var answer []string
	for _, item := range items[:lastEnd] {
		if item.TurnID == end.TurnID && item.Type == "message" && item.Role == "assistant" && item.Status == "done" && strings.TrimSpace(item.Content.Text) != "" {
			answer = append(answer, item.Content.Text)
		}
	}
	text := strings.Join(answer, "\n\n")

	return text, true, nil
}

type desktopSessionError struct{ message string }

func (e *desktopSessionError) Error() string { return e.message }

func (d *Dispatcher) desktopState(run *model.Execution, status, detail string) error {
	if run.Status == status && run.Detail == detail {
		return nil
	}
	next := *run
	next.Status = status
	next.Detail = detail
	if err := d.store.SaveExecution(next); err != nil {
		return err
	}
	*run = next
	return nil
}

func (d *Dispatcher) desktopFailure(task model.Task, run *model.Execution, actor string, err error) {
	if run != nil {
		if saveErr := d.desktopState(run, "failed", err.Error()); saveErr != nil {
			d.comment(task.ID, actor, "记录执行失败状态失败，请先核对桌面会话："+saveErr.Error())
			return
		}
	}
	d.comment(task.ID, actor, "DSH 桌面连接或会话执行失败，程序未改动卡片状态；请由执行 Agent 接续处理："+err.Error())
}

func (d *Dispatcher) runDesktop(ctx context.Context, task model.Task, dir, text, actor, by, followupID string) {
	defer d.finishProjectCard(task.ID)
	run, err := d.store.Execution(task.ID)
	if err != nil {
		d.comment(task.ID, actor, "读取桌面执行记录失败："+err.Error())
		return
	}
	resuming := run != nil && run.Active()
	var selections map[string]string
	if !resuming {
		selections, err = desktopSelections(d.runtimeConfigPath)
		if err != nil {
			d.desktopFailure(task, nil, actor, err)
			return
		}
	}
	b, err := connectDesktop(ctx)
	if err != nil {
		if resuming {
			d.desktopState(run, "disconnected", err.Error())
			d.comment(task.ID, actor, "DSH 桌面跟踪中断，原会话保留，重试只恢复跟踪："+err.Error())
		} else {
			d.desktopFailure(task, nil, actor, err)
		}
		return
	}
	defer b.Close()
	if !resuming {
		run = &model.Execution{TaskID: task.ID, SessionID: fmt.Sprintf("taskboard-%x", randomDesktopID()), Status: "submitting", Runtime: RuntimeDSH, FollowupID: followupID}
		// Commit identity BEFORE sending. A lost acknowledgement must never cause
		// a second session after a board restart or a repeated dispatch request.
		if err := d.store.SaveExecution(*run); err != nil {
			d.comment(task.ID, actor, "保存派发记录失败，尚未提交桌面任务："+err.Error())
			return
		}
		var accepted struct {
			Accepted          bool   `json:"accepted"`
			OK                *bool  `json:"ok"`
			Message           string `json:"message"`
			ExternalSessionID string `json:"externalSessionId"`
		}
		err = b.call(ctx, "session.createAndStart", map[string]any{"sessionId": run.SessionID, "clientMessageId": run.SessionID, "cwd": dir, "content": text, "selections": selections, "agentPreset": "standard"}, &accepted)
		if err != nil || !accepted.Accepted {
			if err == nil && accepted.OK != nil && !*accepted.OK {
				d.setFollowup(followupID, "failed")
				d.desktopFailure(task, run, actor, fmt.Errorf("桌面端拒绝接收：%s", accepted.Message))
				return
			}
			// The comment may or may not have been received; never replay it.
			d.setFollowup(followupID, "unknown")
			detail := fmt.Sprintf("%v %s", err, accepted.Message)
			d.desktopState(run, "unknown", detail)
			d.comment(task.ID, actor, "DSH 桌面端未确认接收；保留会话 "+run.SessionID+"，再次派发只核对原会话，避免重复执行："+detail)
			return
		}
		d.setFollowup(followupID, "sent")
		if err := d.desktopState(run, "running", ""); err != nil {
			d.comment(task.ID, actor, "桌面已接收，但执行记录更新失败："+err.Error())
			return
		}
		if followupID == "" {
			d.syncRunningTaskColumn(task.ID, actor)
		}
		if followupID != "" {
			d.comment(task.ID, actor, fmt.Sprintf("已按评论 @%s 改由 DSH 桌面端新建会话处理（工作目录 %s）。会话 ID：%s", actor, dir, accepted.ExternalSessionID))
		} else {
			d.comment(task.ID, actor, fmt.Sprintf("已交给 DSH 桌面端（工作目录 %s；发起人 %s）。请在项目工作目录下查看本次任务“%s”的会话。会话 ID：%s", dir, by, task.Title, accepted.ExternalSessionID))
		}
	} else {
		d.comment(task.ID, actor, "恢复 DSH 桌面会话跟踪："+run.SessionID+"；不重新提交任务。")
	}
	d.trackDesktop(ctx, b, task, run, actor)
}

func (d *Dispatcher) trackDesktop(ctx context.Context, b *desktopBridge, task model.Task, run *model.Execution, actor string) {
	var err error
	waiting := false
	var blockedSince time.Time
	for {
		if ctx.Err() != nil {
			d.desktopState(run, "detached", "")
			d.comment(task.ID, actor, "看板已停止跟踪 DSH 桌面任务；桌面会话继续保留，下次启动会恢复跟踪。")
			return
		}
		var state struct {
			Status string `json:"status"`
		}
		err = b.call(ctx, "session.getState", desktopSessionParams(run.SessionID), &state)
		if err == nil {
			switch state.Status {
			case "waiting_approval":
				err = d.desktopState(run, "waiting_approval", "")
				if !waiting {
					d.comment(task.ID, actor, "DSH 正在等待你的确认，请打开桌面端对应会话处理。")
					waiting = true
				}
			case "idle":
				var done bool
				_, done, err = b.resultAfter(ctx, run.SessionID, run.PreviousTurnID)
				if err == nil && done {
					if err := d.desktopState(run, "returned", "会话已结束；任务结果以执行 Agent 的原卡回写为准"); err != nil {
						d.comment(task.ID, actor, "保存会话结束记录失败："+err.Error())
					}
					return
				}
			case "running":
				waiting = false
				err = d.desktopState(run, "running", "")
			case "blocked":
				if blockedSince.IsZero() {
					blockedSince = time.Now()
				}
				if time.Since(blockedSince) > 30*time.Second {
					err = fmt.Errorf("桌面会话持续不可读取")
				}
			case "error":
				err = &desktopSessionError{message: "桌面会话执行失败，请查看桌面端错误详情"}
			default:
				err = fmt.Errorf("无法识别桌面会话状态：%q", state.Status)
			}
		}
		if state.Status != "blocked" {
			blockedSince = time.Time{}
		}
		if err != nil {
			if ctx.Err() != nil {
				continue
			}
			var terminal *desktopSessionError
			if errors.As(err, &terminal) {
				d.desktopFailure(task, run, actor, err)
			} else {
				d.desktopState(run, "disconnected", err.Error())
				d.comment(task.ID, actor, "DSH 桌面任务跟踪失败；原会话保留，再次派发或重启看板只恢复跟踪，不重新执行："+err.Error())
			}
			return
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
}

func randomDesktopID() []byte {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		panic(err)
	}
	return id
}
