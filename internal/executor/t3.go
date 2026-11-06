package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"taskboard-native/internal/model"
)

// T3 Code is a second execution transport beside Herdr: it owns an agent
// thread on a running T3 server, but it never becomes a new public runtime.
// The board's runtime id stays codex/claude, and the session id carries the
// transport prefix `t3:` exactly like `herdr:` does.

const t3SessionPrefix = "t3:"

// t3InitialTurnPendingDetail marks the window between `thread start` and the
// first observed turn. It is not evidence of completion.
const t3InitialTurnPendingDetail = "T3 线程已创建，等待初始 turn 开始"

// t3ProviderDefaults maps a board runtime to the provider id T3 exposes.
// The sidecar config can override either one.
var t3ProviderDefaults = map[string]string{
	RuntimeCodex:  "codex",
	RuntimeClaude: "claudeAgent",
}

const (
	t3CommandTimeout = 30 * time.Second
	t3ProbeTimeout   = time.Second
)

// t3RuntimePlan is the resolved T3 transport configuration for one dispatch.
// A nil plan means "do not use T3 for this runtime"; an error means the
// operator's config is broken and the dispatch must fail explicitly.
type t3RuntimePlan struct {
	bin      string
	baseDir  string
	port     int
	provider string
	model    string
	preferT3 bool
}

// t3Plan resolves T3 for runtime by reading the same sidecar file as the
// other channel connections. Missing pieces mean "not set up", so the
// dispatcher moves on to the next transport instead of blocking the card.
func t3Plan(configPath, runtime string) (*t3RuntimePlan, error) {
	defaultProvider, supported := t3ProviderDefaults[runtime]
	if !supported {
		return nil, nil
	}
	config, err := loadT3Config(configPath)
	if err != nil {
		return nil, err
	}
	if config.Enabled != nil && !*config.Enabled {
		return nil, nil
	}
	bin := strings.TrimSpace(config.Bin)
	if bin == "" {
		bin = strings.TrimSpace(os.Getenv("TASKBOARD_T3_BIN"))
	}
	if bin == "" {
		bin = resolveT3Bin()
	}
	if bin == "" {
		return nil, nil
	}
	// A sidecar shared across machines may name a binary this host lacks;
	// that is "not set up here", not an operator error.
	if info, err := os.Stat(bin); err != nil || info.IsDir() || info.Mode()&0111 == 0 {
		return nil, nil
	}
	baseDir := expandT3Path(config.BaseDir)
	if baseDir == "" {
		baseDir = expandT3Path("~/.t3-dispatch")
	}
	if baseDir == "" {
		return nil, nil
	}
	provider := strings.TrimSpace(config.Provider[runtime])
	if provider == "" {
		provider = defaultProvider
	}
	model := strings.TrimSpace(config.Model[runtime])
	if model == "" {
		model = t3ManifestDefaultModel(baseDir, provider)
	}
	return &t3RuntimePlan{
		bin:      bin,
		baseDir:  baseDir,
		port:     config.Port,
		provider: provider,
		model:    model,
		preferT3: config.Prefer == "t3",
	}, nil
}

// canStart reports whether a new thread can be created. Tracking an existing
// thread needs only bin and baseDir, so a missing model never strands a
// running session after a restart.
func (p *t3RuntimePlan) canStart() bool {
	return p != nil && p.bin != "" && p.baseDir != "" && p.provider != "" && p.model != ""
}

// resolveT3Bin follows the same Finder-safe order as the other channels:
// explicit env, the two known installs, then PATH.
func resolveT3Bin() string {
	for _, candidate := range []string{"~/.t3/bin/t3", "~/.t3-zh/bin/t3"} {
		path := expandT3Path(candidate)
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path
		}
	}
	if path, err := exec.LookPath("t3"); err == nil {
		return path
	}
	return ""
}

func expandT3Path(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	return path
}

// t3ManifestDefaultModel reads T3's own provider defaults. Only providers
// that publish a default chat model answer here; codex does not, which is why
// the sidecar model override exists.
func t3ManifestDefaultModel(baseDir, provider string) string {
	raw, err := os.ReadFile(filepath.Join(baseDir, "userdata", "model-manifest.json"))
	if err != nil {
		return ""
	}
	var document struct {
		Manifest struct {
			Providers map[string]struct {
				Defaults struct {
					Chat string `json:"chat"`
				} `json:"defaults"`
			} `json:"providers"`
		} `json:"manifest"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return ""
	}
	return strings.TrimSpace(document.Manifest.Providers[provider].Defaults.Chat)
}

func t3SessionAddress(sessionID string) (string, bool) {
	sessionID = strings.TrimSpace(sessionID)
	threadID := strings.TrimPrefix(sessionID, t3SessionPrefix)
	return threadID, threadID != sessionID && threadID != ""
}

// IsT3Session reports whether a stored session id belongs to this transport.
func IsT3Session(sessionID string) bool {
	_, ok := t3SessionAddress(sessionID)
	return ok
}

type t3Started struct {
	ThreadID  string `json:"threadId"`
	ProjectID string `json:"projectId"`
	RunID     string `json:"runId"`
}

type t3ShowMessage struct {
	Role  string `json:"role"`
	RunID string `json:"runId"`
	Text  string `json:"text"`
}

type t3Show struct {
	Thread struct {
		ThreadID string `json:"threadId"`
		Title    string `json:"title"`
		Status   string `json:"status"`
	} `json:"thread"`
	Messages []t3ShowMessage `json:"messages"`
}

func t3State(show t3Show) string {
	return strings.TrimSpace(show.Thread.Status)
}

// t3ObservedTurn reports whether the thread already produced a run. The
// initial user message is not a turn; only an agent message is.
func t3ObservedTurn(show t3Show) bool {
	for _, message := range show.Messages {
		if message.Role != "" && message.Role != "user" {
			return true
		}
	}
	return false
}

type t3ErrorKind int

const (
	// t3ErrUnavailable means no T3 server answered. Nothing was created, so
	// the dispatcher may fall through to the next transport.
	t3ErrUnavailable t3ErrorKind = iota
	// t3ErrConfig means T3 answered but rejected the request (missing
	// project, bad provider/model). Falling back would open a second writer.
	t3ErrConfig
)

type t3Error struct {
	kind      t3ErrorKind
	uncertain bool
	err       error
}

func (e *t3Error) Error() string { return e.err.Error() }
func (e *t3Error) Unwrap() error { return e.err }

func isT3Unavailable(err error) bool {
	var target *t3Error
	return errors.As(err, &target) && target.kind == t3ErrUnavailable
}

func t3NoServer(output string) bool {
	return strings.Contains(output, "No running T3 Code server")
}

func t3ProjectExists(output string) bool {
	return strings.Contains(output, "ProjectAlreadyExistsError") || strings.Contains(output, "already exists for")
}

func t3Failure(output string, err error) string {
	text := strings.TrimSpace(output)
	if text == "" && err != nil {
		text = err.Error()
	}
	return tail(text, 800)
}

// decodeT3JSON tolerates the CLI's log lines around the JSON body and its
// pretty-printed output.
func decodeT3JSON(raw []byte, target any) error {
	if err := json.Unmarshal(raw, target); err == nil {
		return nil
	}
	start := bytes.IndexByte(raw, '{')
	end := bytes.LastIndexByte(raw, '}')
	if start < 0 || end <= start {
		return fmt.Errorf("输出中没有 JSON 对象：%s", tail(string(raw), 200))
	}
	return json.Unmarshal(raw[start:end+1], target)
}

// t3Run executes one CLI call. stdout carries the --json payload; combined
// keeps stderr for error classification and diagnostics.
func t3Run(ctx context.Context, plan *t3RuntimePlan, args ...string) ([]byte, string, error) {
	command := exec.CommandContext(ctx, plan.bin, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	combined := strings.TrimSpace(stdout.String() + "\n" + stderr.String())
	return stdout.Bytes(), combined, err
}

// t3ServerPort reads the port the running server published for baseDir. The
// server writes this file at startup, and a stale file is harmless because the
// caller TCP-probes the port before trusting it.
func t3ServerPort(baseDir string) int {
	raw, err := os.ReadFile(filepath.Join(baseDir, "userdata", "server-runtime.json"))
	if err != nil {
		return 0
	}
	var runtime struct {
		Port int `json:"port"`
	}
	if err := json.Unmarshal(raw, &runtime); err != nil {
		return 0
	}
	if runtime.Port <= 0 || runtime.Port > 65535 {
		return 0
	}
	return runtime.Port
}

// t3Available probes the server that owns baseDir. A pinned port and the one
// the server published are both cheap TCP checks; only a server too old to
// publish its port needs the CLI, whose `thread list` first resolves a project
// from the board's cwd and fails there even while the server is healthy.
func t3Available(ctx context.Context, plan *t3RuntimePlan) bool {
	port := plan.port
	if port == 0 {
		port = t3ServerPort(plan.baseDir)
	}
	if port > 0 {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), t3ProbeTimeout)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}
	probe, cancel := context.WithTimeout(ctx, t3CommandTimeout)
	defer cancel()
	_, combined, err := t3Run(probe, plan, "thread", "list", "--base-dir", plan.baseDir, "--json")
	if err != nil {
		// A server that answered "no project matches this cwd" is up; only a
		// missing server or an unreadable answer means the transport is down.
		return strings.Contains(combined, "No T3 Code project matches")
	}
	return !t3NoServer(combined)
}

// t3Create adds the project (idempotent) and starts one thread. The returned
// id is already transport-prefixed so it can be stored as a session id.
func t3Create(ctx context.Context, task model.Task, dir, runtime, actor, text, boardURL string, plan *t3RuntimePlan) (string, error) {
	probe, cancel := context.WithTimeout(ctx, t3CommandTimeout)
	available := t3Available(probe, plan)
	cancel()
	if !available {
		return "", &t3Error{kind: t3ErrUnavailable, err: errors.New("T3 服务不可达")}
	}
	// `project add` writes local state even when no server runs; a duplicate
	// project is the steady state and must not fail the dispatch.
	if _, combined, err := t3Run(ctx, plan, "project", "add", "--base-dir", plan.baseDir, dir); err != nil && !t3ProjectExists(combined) {
		return "", &t3Error{kind: t3ErrConfig, err: fmt.Errorf("T3 无法登记项目目录：%s", t3Failure(combined, err))}
	}
	title := strings.Join(strings.Fields(task.Title), " ")
	message := text + "\n\n" + t3DispatchMarker(task.ID) + "\n" + t3IdentityLine(actor) + t3BoardLine(boardURL)
	stdout, combined, err := t3Run(ctx, plan,
		"thread", "start",
		"--base-dir", plan.baseDir,
		"--title", title,
		"--provider", plan.provider,
		"--model", plan.model,
		"--json",
		dir, message)
	if err != nil {
		if t3NoServer(combined) {
			return "", &t3Error{kind: t3ErrUnavailable, err: fmt.Errorf("T3 服务不可达：%s", t3Failure(combined, err))}
		}
		return "", &t3Error{kind: t3ErrConfig, err: fmt.Errorf("T3 无法启动线程：%s", t3Failure(combined, err))}
	}
	var started t3Started
	if err := decodeT3JSON(stdout, &started); err != nil || strings.TrimSpace(started.ThreadID) == "" {
		return "", &t3Error{kind: t3ErrConfig, uncertain: true, err: fmt.Errorf("T3 已接受启动请求但未返回线程 ID，禁止另建会话：%s", t3Failure(combined, err))}
	}
	return t3SessionPrefix + strings.TrimSpace(started.ThreadID), nil
}

// t3DispatchMarker is the only execution-lock signal T3 can carry: the CLI
// cannot inject environment variables, so the id travels in the prompt.
func t3DispatchMarker(taskID string) string {
	return "TASKBOARD_DISPATCH_TASK_ID=" + taskID
}

// t3IdentityLine replaces TB_AGENT, which T3 also cannot inject.
func t3IdentityLine(actor string) string {
	return fmt.Sprintf("执行身份：%s；写看板时使用 --agent %s。", actor, actor)
}

// t3BoardLine carries TASKBOARD_ADDR the same way, because a T3 thread
// inherits the T3 server's environment, not the board's. Without this line a
// non-default board instance is invisible to the executing agent, which then
// reports the card as missing instead of writing the result back.
func t3BoardLine(boardURL string) string {
	address := strings.TrimSpace(boardURL)
	address = strings.TrimPrefix(address, "http://")
	address = strings.TrimPrefix(address, "https://")
	if address == "" {
		return ""
	}
	return fmt.Sprintf("\n看板地址：http://%s（TASKBOARD_ADDR=%s；写看板时用它定位实例）。", address, address)
}

func t3Get(ctx context.Context, plan *t3RuntimePlan, threadID string) (t3Show, error) {
	stdout, combined, err := t3Run(ctx, plan, "thread", "show", "--base-dir", plan.baseDir, "--json", threadID)
	if err != nil {
		return t3Show{}, fmt.Errorf("T3 无法读取线程 %s：%s", threadID, t3Failure(combined, err))
	}
	var show t3Show
	if err := decodeT3JSON(stdout, &show); err != nil {
		return t3Show{}, fmt.Errorf("T3 线程状态不是有效 JSON：%w", err)
	}
	return show, nil
}

func t3Send(ctx context.Context, plan *t3RuntimePlan, threadID, text string) error {
	_, combined, err := t3Run(ctx, plan, "thread", "send", "--base-dir", plan.baseDir, "--json", threadID, text)
	if err != nil {
		return fmt.Errorf("T3 无法发送到原线程：%s", t3Failure(combined, err))
	}
	return nil
}

// All four are variables so tests can compress the probe cadence.
var (
	t3ProbeInterval    = 2 * time.Second
	t3UnreachableGrace = 2 * time.Minute
	t3IdleGrace        = 6 * time.Second
	t3FlapProbes       = 3
)

type transportOutcome int

const (
	// transportFallthrough means this transport is not reachable and the
	// caller should try the next one. Nothing was created.
	transportFallthrough transportOutcome = iota
	// transportDispatched means the session is live and tracking has run.
	transportDispatched
	// transportStopped means the failure is final: no other session may be
	// opened for this card.
	transportStopped
)

// tryT3Dispatch carries one card from "T3 chosen" to a tracked thread. Nothing
// is written to the card on success; only outcomes a person must act on reach
// the comment stream.
func (d *Dispatcher) tryT3Dispatch(ctx context.Context, task model.Task, dir, runtime, actor, by, text, followupID string, plan *t3RuntimePlan) transportOutcome {
	run := &model.Execution{TaskID: task.ID, Runtime: runtime, Status: "submitting", FollowupID: followupID, Detail: "正在请求 T3 打开线程"}
	if err := d.store.SaveExecution(*run); err != nil {
		d.logDispatch("dispatch.channel_failed", task.ID, "t3", runtime, by, "intent_save_failed", err.Error())
		return transportFallthrough
	}
	// A fresh dispatch gets the plain task text: the hc review pipeline only
	// exists inside Herdr. A channel switch carries the comment text as is.
	if followupID == "" {
		text = buildTaskTextForRuntime(task, runtime)
	}
	sessionID, t3Err := t3Create(ctx, task, dir, runtime, actor, text, d.boardURL, plan)
	if t3Err == nil {
		run.SessionID, run.Status, run.Detail = sessionID, "running", t3InitialTurnPendingDetail
		if err := d.store.SaveExecution(*run); err != nil {
			d.setFollowup(followupID, "unknown")
			d.logDispatch("dispatch.uncertain", task.ID, "t3", runtime, by, "receipt_save_failed", err.Error())
			d.comment(task.ID, actor, "已在 T3 创建线程，但看板没能记下这次执行。不会另建会话，请先在 T3 中核对。")
			return transportStopped
		}
		d.setFollowup(followupID, "sent")
		if followupID == "" {
			d.syncRunningTaskColumn(task.ID, actor)
		}
		d.logDispatch("dispatch.handed_off", task.ID, "t3", runtime, by, "", sessionID)
		d.trackT3(ctx, run, plan)
		return transportDispatched
	}
	var failure *t3Error
	if errors.As(t3Err, &failure) && failure.kind == t3ErrUnavailable {
		run.Status, run.Detail = "failed", t3Err.Error()
		_ = d.store.SaveExecution(*run)
		d.logDispatch("dispatch.channel_failed", task.ID, "t3", runtime, by, "unavailable", t3Err.Error())
		return transportFallthrough
	}
	if errors.As(t3Err, &failure) && failure.uncertain {
		run.Status, run.Detail = "unknown", "T3 创建线程的回执不确定；禁止再次创建，请先在 T3 中核对："+t3Err.Error()
		d.setFollowup(followupID, "unknown")
		d.logDispatch("dispatch.uncertain", task.ID, "t3", runtime, by, "launch_uncertain", t3Err.Error())
	} else {
		run.Status, run.Detail = "failed", t3Err.Error()
		d.setFollowup(followupID, "failed")
		d.logDispatch("dispatch.failed", task.ID, "t3", runtime, by, "config", t3Err.Error())
	}
	_ = d.store.SaveExecution(*run)
	d.comment(task.ID, actor, fmt.Sprintf("没能交给 T3：%v。", t3Err))
	return transportStopped
}

// trackT3 keeps one execution record in step with its T3 thread. A finished
// turn only becomes `returned`: the execution Agent writes the task result
// and moves the card, the program never does.
func (d *Dispatcher) trackT3(ctx context.Context, run *model.Execution, plan *t3RuntimePlan) {
	lastStatus := run.Status
	save := func() {
		_ = d.store.SaveExecution(*run)
		d.recordTrackedStatus(run, "t3", &lastStatus)
	}
	threadID, ok := t3SessionAddress(run.SessionID)
	if !ok {
		run.Status, run.Detail = "failed", "T3 线程标识无效"
		save()
		return
	}
	ticker := time.NewTicker(t3ProbeInterval)
	defer ticker.Stop()
	var unreachableSince, idleSince time.Time
	misses := 0
	seenTurn := false
	for {
		show, err := t3Get(ctx, plan, threadID)
		if err != nil {
			idleSince = time.Time{}
			if ctx.Err() != nil {
				// Service shutdown keeps the thread for the next start.
				return
			}
			if unreachableSince.IsZero() {
				unreachableSince = time.Now()
			}
			if time.Since(unreachableSince) >= t3UnreachableGrace {
				run.Status = "released"
				run.Detail = fmt.Sprintf("T3 线程连续 %s 不可达；已释放写锁，可按人类指令重新派发或评论跟进", t3UnreachableGrace)
				save()
				return
			}
			misses++
			if misses >= t3FlapProbes {
				run.Status = "disconnected"
				run.Detail = fmt.Sprintf("T3 线程连续 %d 次不可达；宽限期内继续探测，超过 %s 释放写锁", misses, t3UnreachableGrace)
				save()
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			continue
		}
		unreachableSince = time.Time{}
		misses = 0
		switch t3State(show) {
		case "preparing", "starting", "running":
			idleSince = time.Time{}
			seenTurn = true
			run.Status, run.Detail = "running", ""
		case "waiting":
			idleSince = time.Time{}
			seenTurn = true
			run.Status, run.Detail = "waiting_approval", "T3 agent 等待人工处理"
			d.parkProjectCardForApproval(run.TaskID)
		case "idle":
			if !seenTurn && !t3ObservedTurn(show) {
				idleSince = time.Time{}
				run.Status, run.Detail = "running", t3InitialTurnPendingDetail
				save()
				break
			}
			seenTurn = true
			if idleSince.IsZero() {
				idleSince = time.Now()
			}
			if time.Since(idleSince) >= t3IdleGrace {
				run.Status, run.Detail = "returned", "T3 agent turn 已结束；任务状态由执行 Agent 回写"
				save()
				return
			}
			run.Status, run.Detail = "running", "T3 线程暂时 idle，等待状态稳定"
		case "completed":
			run.Status, run.Detail = "returned", "T3 agent turn 已结束；任务状态由执行 Agent 回写"
			save()
			return
		case "failed", "cancelled", "interrupted", "rolled_back":
			run.Status, run.Detail = "failed", "T3 线程状态："+t3State(show)
			save()
			return
		default:
			seenTurn = true
			run.Status, run.Detail = "running", "T3 线程状态："+t3State(show)
		}
		save()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *Dispatcher) resumeT3Tracking(run model.Execution, plan *t3RuntimePlan) {
	ctx, cancel := context.WithCancel(context.Background())
	ex := &execution{cancel: cancel, done: make(chan struct{})}
	d.inflight[run.TaskID] = ex
	go func() {
		defer close(ex.done)
		defer func() {
			d.mu.Lock()
			delete(d.inflight, run.TaskID)
			d.mu.Unlock()
		}()
		d.trackT3(ctx, &run, plan)
		d.finishProjectCard(run.TaskID)
	}()
}

// sendT3Followup delivers one saved comment to the original thread. A failed
// send is unknown, never retried automatically.
func (d *Dispatcher) sendT3Followup(ctx context.Context, taskID, actor string, run *model.Execution, text string) {
	plan, err := t3Plan(d.runtimeConfigPath, run.Channel())
	if err != nil || plan == nil {
		detail := "T3 transport 不可用，评论未投递；保留原线程，禁止自动重投"
		if err != nil {
			detail += "：" + err.Error()
		}
		run.Status, run.Detail = "unknown", detail
		_ = d.store.SaveExecution(*run)
		d.setFollowup(run.FollowupID, "unknown")
		d.logDispatch("dispatch.channel_failed", taskID, "t3", run.Runtime, actor, "followup_unavailable", detail)
		d.comment(taskID, actor, "评论没能送达原 T3 会话；不会自动重投。")
		return
	}
	threadID, _ := t3SessionAddress(run.SessionID)
	if err := t3Send(ctx, plan, threadID, text); err != nil {
		run.Status, run.Detail = "unknown", "T3 thread send 未确认送达；保留原线程，禁止自动重投："+err.Error()
		_ = d.store.SaveExecution(*run)
		d.setFollowup(run.FollowupID, "unknown")
		d.logDispatch("dispatch.uncertain", taskID, "t3", run.Runtime, actor, "followup_unsent", run.Detail)
		d.comment(taskID, actor, "评论投递结果不确定，不会自动重投。请先在 T3 中原会话核对。")
		return
	}
	run.Status, run.Detail = "running", "评论已发送到原 T3 线程"
	_ = d.store.SaveExecution(*run)
	d.setFollowup(run.FollowupID, "sent")
	d.trackT3(ctx, run, plan)
}
