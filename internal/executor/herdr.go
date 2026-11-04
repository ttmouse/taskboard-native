package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"taskboard-native/internal/model"
)

type herdrCreated struct {
	Terminal string `json:"terminal"`
	Address  string `json:"address"`
}

type herdrSpace struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Folder string `json:"folder"`
	Active bool   `json:"active"`
}

type herdrSpaces struct {
	Spaces []herdrSpace `json:"spaces"`
}

type herdrLaunchError struct {
	err       error
	uncertain bool
}

// herdrErrorKind distinguishes Herdr failures that should fall back to CLI
// from those that should surface directly (Herder works fine, the project
// just isn't configured for it).
type herdrErrorKind int

const (
	// herdrErrUnavailable means Herdr binary or service cannot be reached at all.
	// The dispatcher should fall back to direct CLI execution.
	herdrErrUnavailable herdrErrorKind = iota
	// herdrErrConfig means Herdr is reachable but the project isn't wired up
	// (no matching space etc.). Fallback is NOT appropriate — Herdr IS
	// available, just not for this project.
	herdrErrConfig
)

type herdrError struct {
	kind herdrErrorKind
	err  error
}

func (e *herdrError) Error() string { return e.err.Error() }
func (e *herdrError) Unwrap() error { return e.err }

// isHerdrUnavailable reports whether err classifies as herdrErrUnavailable.
func isHerdrUnavailable(err error) bool {
	var herdrErr *herdrError
	if errors.As(err, &herdrErr) {
		return herdrErr.kind == herdrErrUnavailable
	}
	// Legacy: anything that wraps the original exec errors from ResolveHerdrApp
	// (e.g. from herdrCommand) also counts as unavailable.
	return strings.Contains(err.Error(), "找不到 herdr-app") || strings.Contains(err.Error(), "找不到 hc")
}

var errHerdrSpaceNotFound = errors.New("Herdr project space not found")

var herdrSpaceLocks sync.Map

// ResolveHerdrApp accounts for Finder-launched apps, whose PATH commonly omits
// the user's CLI install directories.
func ResolveHerdrApp() (string, error) {
	return resolveHerdrExecutable("herdr-app")
}

// ResolveHerdrCore accounts for Finder-launched apps, whose PATH commonly
// omits the directory containing Herdr Core's hc command.
func ResolveHerdrCore() (string, error) {
	return resolveHerdrExecutable("hc")
}

// herdrAvailable reports whether both herdr-app and hc are installed on this
// machine. The dispatcher uses it to decide whether a non-DSH runtime should
// route through Herdr or fall back to direct CLI execution. It is a variable
// so tests can simulate a machine without Herdr deterministically.
var herdrAvailable = func() bool {
	if _, err := ResolveHerdrApp(); err != nil {
		return false
	}
	if _, err := ResolveHerdrCore(); err != nil {
		return false
	}
	return true
}

func resolveHerdrExecutable(name string) (string, error) {
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	var paths []string
	if home, err := os.UserHomeDir(); err == nil {
		for _, dir := range []string{".local/bin", ".npm-global/bin", "go/bin"} {
			paths = append(paths, filepath.Join(home, dir, name))
		}
	}
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin"} {
		paths = append(paths, filepath.Join(dir, name))
	}
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path, nil
		}
	}
	return "", fmt.Errorf("找不到 %s 命令，请确认 Herdr 已安装", name)
}

func herdrCommand(ctx context.Context, args ...string) (*exec.Cmd, error) {
	bin, err := ResolveHerdrApp()
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, bin, args...), nil
}

func (e *herdrLaunchError) Error() string { return e.err.Error() }
func (e *herdrLaunchError) Unwrap() error { return e.err }

type herdrStatus struct {
	Address  string `json:"address"`
	State    string `json:"state"`
	ExitCode *int   `json:"exit_code"`
	Turn     int    `json:"turn"`
	Status   struct {
		State string `json:"state"`
	} `json:"status"`
}

func herdrTerminalAddress(raw []byte) (string, error) {
	var result herdrCreated
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("Herdr did not return terminal details: %w", err)
	}
	address := strings.TrimSpace(result.Terminal)
	if address == "" {
		address = strings.TrimSpace(result.Address)
	}
	if address == "" {
		return "", fmt.Errorf("Herdr created the space but returned no terminal address")
	}
	return address, nil
}

const herdrSessionPrefix = "herdr:"

// HerdrInitialTurnPendingDetail marks the terminal-created window before the
// first agent turn exists. Cards use it to avoid offering a conversation link
// until there is a conversation to open.
const HerdrInitialTurnPendingDetail = "Herdr terminal 已创建，等待初始 turn 开始"

func herdrAddress(sessionID string) (string, bool) {
	address := strings.TrimPrefix(sessionID, herdrSessionPrefix)
	return address, address != sessionID && address != ""
}

func herdrState(status herdrStatus) string {
	state := strings.TrimSpace(status.Status.State)
	if state == "" {
		state = strings.TrimSpace(status.State)
	}
	return state
}

func herdrFolderContains(parent, folder string) bool {
	rel, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(folder))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

func selectHerdrSpace(matches []herdrSpace, projectName string) (string, error) {
	if len(matches) == 1 {
		return matches[0].ID, nil
	}
	var active []herdrSpace
	for _, space := range matches {
		if space.Active {
			active = append(active, space)
		}
	}
	if len(active) == 1 {
		return active[0].ID, nil
	}
	return "", fmt.Errorf("Herdr 中项目 %q 有多个匹配的 space，且无法唯一确定活动项", projectName)
}

func herdrSpaceForFolder(raw []byte, folder, projectName string) (string, error) {
	var result herdrSpaces
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("Herdr space 列表解析失败：%w", err)
	}
	var folderMatches []herdrSpace
	for _, space := range result.Spaces {
		if space.ID != "" && filepath.Clean(space.Folder) == filepath.Clean(folder) {
			folderMatches = append(folderMatches, space)
		}
	}
	if len(folderMatches) > 0 {
		return selectHerdrSpace(folderMatches, projectName)
	}
	var nameMatches []herdrSpace
	for _, space := range result.Spaces {
		if space.ID != "" && strings.TrimSpace(space.Name) == strings.TrimSpace(projectName) && herdrFolderContains(space.Folder, folder) {
			nameMatches = append(nameMatches, space)
		}
	}
	if len(nameMatches) > 0 {
		return selectHerdrSpace(nameMatches, projectName)
	}
	return "", fmt.Errorf("%w：Herdr 中没有与项目 %q 或工作目录 %q 匹配的 space", errHerdrSpaceNotFound, projectName, folder)
}

func herdrCreate(ctx context.Context, task model.Task, projectName, dir, runtime, boardURL, actor, text string) (string, error) {
	lockValue, _ := herdrSpaceLocks.LoadOrStore(filepath.Clean(dir), &sync.Mutex{})
	spaceLock := lockValue.(*sync.Mutex)
	spaceLock.Lock()
	defer spaceLock.Unlock()

	// Phase 1: initial herdr ls — a failure here means Herdr truly is
	// unavailable (binary missing or service unreachable), so we classify
	// these as Unavailable and let the dispatcher fall back to CLI.
	command, err := herdrCommand(ctx, "ls")
	if err != nil {
		return "", &herdrError{kind: herdrErrUnavailable, err: fmt.Errorf("无法读取 Herdr space 列表：%w", err)}
	}
	spacesRaw, err := command.Output()
	if err != nil {
		return "", &herdrError{kind: herdrErrUnavailable, err: fmt.Errorf("无法读取 Herdr space 列表：%w", err)}
	}
	spaceID, err := herdrSpaceForFolder(spacesRaw, dir, projectName)
	if errors.Is(err, errHerdrSpaceNotFound) {
		// Phase 2: create the space — herdr ls already worked, so any failure
		// here means Herdr IS reachable but doesn't support this command or
		// rejects it for project-specific reasons. We call that Config.
		createArgs := []string{"do", "space.new", "--name", projectName, "--folder", dir}
		command, commandErr := herdrCommand(ctx, createArgs...)
		if commandErr != nil {
			return "", &herdrError{kind: herdrErrUnavailable, err: fmt.Errorf("无法在 Herdr 创建项目 space：%w", commandErr)}
		}
		out, createErr := command.CombinedOutput()
		if createErr != nil {
			return "", &herdrError{kind: herdrErrConfig, err: fmt.Errorf("无法在 Herdr 创建项目 space：%s", strings.TrimSpace(string(out)))}
		}
		command, err = herdrCommand(ctx, "ls")
		if err != nil {
			return "", &herdrError{kind: herdrErrUnavailable, err: fmt.Errorf("Herdr 已请求创建项目 space，但无法重新读取 space 列表：%w", err)}
		}
		spacesRaw, err = command.Output()
		if err != nil {
			return "", &herdrError{kind: herdrErrConfig, err: fmt.Errorf("Herdr 已请求创建项目 space，但无法重新读取 space 列表：%w", err)}
		}
		spaceID, err = herdrSpaceForFolder(spacesRaw, dir, projectName)
		if err != nil {
			return "", &herdrError{kind: herdrErrConfig, err: fmt.Errorf("Herdr 已请求创建项目 space，但无法确认它：%w", err)}
		}
	}
	if err != nil {
		return "", &herdrError{kind: herdrErrConfig, err: err}
	}
	argv := []string{"env", "TASKBOARD_ADDR=" + strings.TrimPrefix(boardURL, "http://"), "TB_AGENT=" + actor, "TASKBOARD_DISPATCH_TASK_ID=" + task.ID, runtime}
	switch runtime {
	case RuntimeCodex:
		argv = append(argv, "--no-daemon", "-c", "sandbox_workspace_write.network_access=true")
	case RuntimeClaude:
		argv = append(argv, "--permission-mode", "acceptEdits", "--allowedTools", "Bash")
	}
	encoded, err := json.Marshal(argv)
	if err != nil {
		return "", err
	}
	args := []string{"do", "tab.new", "--name", task.Title, "--space", spaceID, "--folder", dir, "--argv", string(encoded), "--task", text}
	command, err = herdrCommand(ctx, args...)
	if err != nil {
		return "", &herdrLaunchError{err: err, uncertain: false}
	}
	out, err := command.CombinedOutput()
	if err != nil {
		return "", &herdrLaunchError{err: fmt.Errorf("Herdr could not create the agent tab: %s", strings.TrimSpace(string(out))), uncertain: true}
	}
	address, err := herdrTerminalAddress(out)
	if err != nil {
		return "", &herdrLaunchError{err: err, uncertain: true}
	}
	return herdrSessionPrefix + address, nil
}

func herdrGet(ctx context.Context, address string) (herdrStatus, error) {
	command, err := herdrCoreCommand(ctx, "get", address, "--json")
	if err != nil {
		return herdrStatus{}, err
	}
	out, err := command.Output()
	if err != nil {
		return herdrStatus{}, err
	}
	var result herdrStatus
	if err := json.Unmarshal(out, &result); err != nil {
		return herdrStatus{}, err
	}
	return result, nil
}

func herdrSend(ctx context.Context, address, text string) error {
	command, err := herdrCoreCommand(ctx, "send", address, text)
	if err != nil {
		return err
	}
	out, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Herdr could not reach the original terminal: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func herdrCoreCommand(ctx context.Context, args ...string) (*exec.Cmd, error) {
	bin, err := ResolveHerdrCore()
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, bin, args...), nil
}

// herdrProbeInterval and herdrUnreachableGrace bound how long a tracked Herdr
// terminal may stay unreachable before its record converges to released and
// gives up the card's write lock. Without that convergence a terminal that is
// gone for good would leave the record occupying the lock forever: the card
// could be neither followed up nor dispatched again. All three are variables so
// tests can shorten them.
var (
	herdrProbeInterval    = 2 * time.Second
	herdrUnreachableGrace = 2 * time.Minute
	herdrIdleGrace        = 6 * time.Second
	// herdrFlapProbes is how many consecutive failed probes (about 6s at the
	// default interval) it takes before the badge says 连接异常. A single miss
	// is a routing blip, not evidence that the session is gone.
	herdrFlapProbes = 3
)

func (d *Dispatcher) trackHerdr(ctx context.Context, run *model.Execution) {
	ticker := time.NewTicker(herdrProbeInterval)
	defer ticker.Stop()
	var unreachableSince time.Time
	var idleSince time.Time
	misses := 0
	for {
		address, ok := herdrAddress(run.SessionID)
		if !ok {
			run.Status, run.Detail = "failed", "Herdr terminal 标识无效"
			_ = d.store.SaveExecution(*run)
			return
		}
		status, err := herdrGet(ctx, address)
		if err != nil {
			idleSince = time.Time{}
			if ctx.Err() != nil {
				// Service shutdown: the contract keeps the original terminal and
				// resumes tracking next start, so nothing converges here.
				return
			}
			if unreachableSince.IsZero() {
				unreachableSince = time.Now()
			}
			if time.Since(unreachableSince) >= herdrUnreachableGrace {
				run.Status = "released"
				run.Detail = fmt.Sprintf("Herdr terminal 连续 %s 不可达；已释放写锁，可按人类指令重新派发或评论跟进", herdrUnreachableGrace)
				_ = d.store.SaveExecution(*run)
				return
			}
			misses++
			if misses >= herdrFlapProbes {
				run.Status = "disconnected"
				run.Detail = fmt.Sprintf("Herdr terminal 连续 %d 次不可达；宽限期内继续探测，超过 %s 释放写锁", misses, herdrUnreachableGrace)
				_ = d.store.SaveExecution(*run)
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
		switch herdrState(status) {
		case "working":
			idleSince = time.Time{}
			run.Status, run.Detail = "running", ""
		case "blocked":
			idleSince = time.Time{}
			run.Status, run.Detail = "waiting_approval", "Herdr agent 等待人工处理"
			d.parkProjectCardForApproval(run.TaskID)
		case "done":
			idleSince = time.Time{}
			run.Status, run.Detail = "returned", "Herdr agent turn 已结束；任务状态由执行 Agent 回写"
			_ = d.store.SaveExecution(*run)
			return
		case "idle":
			// A newly created Herdr terminal reports idle before the agent's
			// first turn starts. That is not evidence of completion; keep the
			// reservation live until a turn was observed or an exit was reported.
			if status.Turn == 0 && status.ExitCode == nil {
				idleSince = time.Time{}
				run.Status, run.Detail = "running", HerdrInitialTurnPendingDetail
				_ = d.store.SaveExecution(*run)
				break
			}
			if idleSince.IsZero() {
				idleSince = time.Now()
			}
			if time.Since(idleSince) >= herdrIdleGrace {
				run.Status, run.Detail = "returned", "Herdr agent turn 已结束；任务状态由执行 Agent 回写"
				_ = d.store.SaveExecution(*run)
				return
			}
			run.Status, run.Detail = "running", "Herdr turn 暂时 idle，等待状态稳定"
		default:
			idleSince = time.Time{}
			if status.ExitCode != nil {
				run.Status = "returned"
				run.Detail = "Herdr terminal 已结束；任务状态由执行 Agent 回写"
				if *status.ExitCode != 0 {
					run.Status = "failed"
					run.Detail = fmt.Sprintf("Herdr agent exited with status %d", *status.ExitCode)
				}
				_ = d.store.SaveExecution(*run)
				return
			}
		}
		_ = d.store.SaveExecution(*run)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *Dispatcher) sendHerdrFollowup(ctx context.Context, taskID, actor string, run *model.Execution, text string) {
	address, _ := herdrAddress(run.SessionID)
	if err := herdrSend(ctx, address, text); err != nil {
		run.Status, run.Detail = "unknown", "hc send 未确认送达；保留原 terminal，禁止自动重投："+err.Error()
		_ = d.store.SaveExecution(*run)
		d.setFollowup(run.FollowupID, "unknown")
		d.comment(taskID, actor, run.Detail)
		return
	}
	run.Status, run.Detail = "running", "评论已发送到原 Herdr terminal"
	_ = d.store.SaveExecution(*run)
	d.setFollowup(run.FollowupID, "sent")
	d.trackHerdr(ctx, run)
}
