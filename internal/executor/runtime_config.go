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
	var connections map[string]runtimeConnection
	if err := json.Unmarshal(raw, &connections); err != nil {
		return nil, fmt.Errorf("看板渠道配置不是有效 JSON：%w", err)
	}
	connection, exists := connections[runtime]
	if !exists {
		return args, nil
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
