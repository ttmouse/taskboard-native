package executor

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type runtimeConnection struct {
	Provider        string `json:"provider,omitempty"`
	BaseURL         string `json:"baseUrl"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
}

// t3Config is the optional T3 transport section of the same sidecar file.
// Provider and Model are keyed by the board's runtime id (`codex`, `claude`),
// so one section serves both channels without a second config file.
type t3Config struct {
	Enabled  *bool             `json:"enabled,omitempty"`
	Bin      string            `json:"bin,omitempty"`
	BaseDir  string            `json:"baseDir,omitempty"`
	Port     int               `json:"port,omitempty"`
	Prefer   string            `json:"prefer,omitempty"`
	Provider map[string]string `json:"provider,omitempty"`
	Model    map[string]string `json:"model,omitempty"`
}

// loadT3Config reads the `t3` section. A missing file or section is the
// default state, not an error: T3 then runs on its built-in defaults.
func loadT3Config(path string) (t3Config, error) {
	if path == "" {
		return t3Config{}, nil
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return t3Config{}, nil
	}
	if err != nil {
		return t3Config{}, fmt.Errorf("读取看板渠道配置：%w", err)
	}
	var document struct {
		T3 *t3Config `json:"t3"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return t3Config{}, fmt.Errorf("看板渠道配置不是有效 JSON：%w", err)
	}
	if document.T3 == nil {
		return t3Config{}, nil
	}
	config := *document.T3
	switch config.Prefer {
	case "", "herdr", "t3":
	default:
		return t3Config{}, fmt.Errorf("t3.prefer 只能是 herdr 或 t3，收到 %q", config.Prefer)
	}
	if config.Port < 0 || config.Port > 65535 {
		return t3Config{}, fmt.Errorf("t3.port 必须在 0-65535 之间，收到 %d", config.Port)
	}
	return config, nil
}

// runtimeArgs reads only the board's optional connection profile. No CLI
// configuration or login file is copied, edited, or replaced.
func runtimeArgs(dir, text, runtime string, configPaths ...string) ([]string, error) {
	definition, ok := runtimeDefinitionFor(runtime)
	if !ok {
		return nil, fmt.Errorf("未知执行渠道：%s", runtime)
	}
	if runtime == RuntimeDSH {
		return nil, fmt.Errorf("DSH 任务通过桌面桥接派发，不支持启动独立 CLI")
	}
	args := definition.args(dir, text)
	if (runtime != RuntimeCodex && runtime != RuntimePi) || len(configPaths) == 0 || configPaths[0] == "" {
		return args, nil
	}
	raw, err := os.ReadFile(configPaths[0])
	if os.IsNotExist(err) {
		return args, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取看板渠道配置：%w", err)
	}
	// Decode only this runtime's section: the file also carries the `t3`
	// transport section, whose shape differs.
	var connections map[string]json.RawMessage
	if err := json.Unmarshal(raw, &connections); err != nil {
		return nil, fmt.Errorf("看板渠道配置不是有效 JSON：%w", err)
	}
	rawConnection, exists := connections[runtime]
	if !exists {
		return args, nil
	}
	var connection runtimeConnection
	if err := json.Unmarshal(rawConnection, &connection); err != nil {
		return nil, fmt.Errorf("看板渠道配置中 %s 段无效：%w", runtime, err)
	}
	if runtime == RuntimePi {
		if strings.TrimSpace(connection.Model) == "" {
			return nil, fmt.Errorf("Pi 独立配置需要非空 model")
		}
		result := append([]string{}, args[:len(args)-2]...)
		if connection.Provider != "" {
			result = append(result, "--provider", connection.Provider)
		}
		result = append(result, "--model", connection.Model)
		return append(result, "--", text), nil
	}
	address, err := url.Parse(connection.BaseURL)
	if err != nil || address.Host == "" || (address.Scheme != "http" && address.Scheme != "https") || address.User != nil || address.RawQuery != "" || address.Fragment != "" || strings.TrimSpace(connection.Model) == "" {
		return nil, fmt.Errorf("Codex 独立连接需要有效的 http(s) baseUrl 和非空 model")
	}
	// A provider scoped to this invocation bypasses stale global endpoints.
	// Magpie's local loopback endpoint accepts its documented public bearer.
	settings := []string{
		`model_provider="taskboard"`,
		"model=" + strconv.Quote(connection.Model),
		`model_providers.taskboard.name="Taskboard"`,
		"model_providers.taskboard.base_url=" + strconv.Quote(connection.BaseURL),
		`model_providers.taskboard.wire_api="responses"`,
		`model_providers.taskboard.supports_websockets=false`,
		`model_providers.taskboard.experimental_bearer_token="magpie"`,
	}
	if connection.ReasoningEffort != "" {
		settings = append(settings, "model_reasoning_effort="+strconv.Quote(connection.ReasoningEffort))
	}
	result := append([]string{}, args[:len(args)-2]...)
	result = append(result, "--ignore-user-config")
	for _, setting := range settings {
		result = append(result, "-c", setting)
	}
	return append(result, "--", text), nil
}
