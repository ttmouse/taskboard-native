// Command taskboard-mcp is the taskboard MCP server: newline-delimited
// JSON-RPC 2.0 over stdio. Every tool call is an HTTP request to the local
// taskboard service, so the service stays the only writer.
package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"taskboard-native/internal/model"
	"taskboard-native/internal/service"
)

// protocolVersion is the MCP revision this server implements.
const protocolVersion = "2025-06-18"

func main() {
	if err := newServer().serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "taskboard-mcp: %v\n", err)
		os.Exit(1)
	}
}

// newServer builds the stdio server from the environment: TASKBOARD_ADDR
// locates the service, TASKBOARD_AGENT is the identity on writes.
func newServer() *server {
	agent := os.Getenv("TASKBOARD_AGENT")
	if agent == "" {
		agent = "mcp"
	}
	base := service.DefaultListenAddr()
	if !strings.HasPrefix(base, "http") {
		base = "http://" + base
	}
	return &server{
		agent: agent,
		base:  base,
		http:  &http.Client{Timeout: 20 * time.Second},
	}
}

type server struct {
	agent string
	base  string
	http  *http.Client
}

// --- JSON-RPC wire types ---

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// serve reads one JSON-RPC message per line until EOF.
func (s *server) serve(in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		resp, ok := s.handleLine(line)
		if !ok {
			continue // notification: no response
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// handleLine dispatches one message. ok=false means it was a notification.
func (s *server) handleLine(line []byte) (response, bool) {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return response{JSONRPC: "2.0", Error: &rpcError{Code: codeParseError, Message: "parse error: " + err.Error()}}, true
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		return response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: codeInvalidRequest, Message: "invalid request"}}, true
	}
	// Notifications (no id) are acknowledged only by doing the work, except
	// notifications/initialized which needs none.
	if len(req.ID) == 0 || string(req.ID) == "null" {
		return response{}, false
	}
	result, rpcErr := s.dispatch(req)
	if rpcErr != nil {
		return response{JSONRPC: "2.0", ID: req.ID, Error: rpcErr}, true
	}
	return response{JSONRPC: "2.0", ID: req.ID, Result: result}, true
}

func (s *server) dispatch(req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{"listChanged": false},
			},
			"serverInfo": map[string]any{
				"name":    "taskboard-native",
				"version": "0.1.0",
			},
			"instructions": "本机看板：用 list_projects 找到项目，用 list_tasks 按 projectId 查看卡片，" +
				"可用 list_tasks(summary=true) 先读卡片摘要，再用 get_task(id) 读取正文和评论。" +
				"create_project / create_task / update_task / move_task / add_comment / archive_task / " +
				"add_relation / remove_relation / release_execution / followup_comment 可写入；" +
				"卡片更新、移动、归档与关系变更需要当前 version；未知参数会被拒绝。",
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": toolDefinitions()}, nil
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, &rpcError{Code: codeInvalidParams, Message: "invalid params: " + err.Error()}
		}
		text, err := s.callTool(params.Name, params.Arguments)
		if err != nil {
			return map[string]any{
				"content": []map[string]any{{"type": "text", "text": err.Error()}},
				"isError": true,
			}, nil
		}
		content := []map[string]any{{"type": "text", "text": text}}
		// get_task returns the card as JSON; any images embedded in its body
		// or comments are referenced by /api/images/{hash}. Attach them as
		// native image blocks so a multimodal model can actually see them,
		// with no extra instruction in the task prompt.
		if params.Name == "get_task" {
			content = append(content, s.taskImageBlocks(text)...)
		}
		return map[string]any{
			"content": content,
			"isError": false,
		}, nil
	case "resources/list":
		return map[string]any{"resources": []any{}}, nil
	case "prompts/list":
		return map[string]any{"prompts": []any{}}, nil
	default:
		return nil, &rpcError{Code: codeMethodNotFound, Message: "method not found: " + req.Method}
	}
}

func toolDefinitions() []map[string]any {
	object := func(props map[string]any, required ...string) map[string]any {
		schema := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			schema["required"] = required
		}
		// Every schema is closed: unknown arguments are rejected by the API.
		schema["additionalProperties"] = false
		return schema
	}
	str := func(desc string) map[string]any {
		return map[string]any{"type": "string", "description": desc}
	}
	num := func(desc string) map[string]any {
		return map[string]any{"type": "integer", "description": desc}
	}
	boolean := func(desc string) map[string]any {
		return map[string]any{"type": "boolean", "description": desc}
	}
	return []map[string]any{
		{
			"name":        "list_projects",
			"description": "列出所有看板项目及其列；配置了工作目录的项目会包含 workspacePath（项目 id 用于后续按项目作用域查询）。",
			"inputSchema": object(map[string]any{}),
		},
		{
			"name":        "create_project",
			"description": "创建看板项目。可选 workspacePath 必须是本机已存在的绝对目录；服务负责校验，MCP 不会创建目录或执行任务。",
			"inputSchema": object(map[string]any{
				"name":          str("项目名称，必填"),
				"workspacePath": str("可选的本机绝对目录路径，目录必须已存在"),
			}, "name"),
		},
		{
			"name":        "list_tasks",
			"description": "按项目列出卡片（projectId 必填，作用域限定）。默认只返回未归档卡片；可使用 summary=true 先读摘要，再用 get_task(id) 读取单卡正文和评论。",
			"inputSchema": object(map[string]any{
				"projectId":       str("项目 id，必填"),
				"includeArchived": boolean("是否包含已归档卡片，默认 false"),
				"columnId":        str("按项目内的列 id 精确筛选"),
				"assignee":        str("按负责人精确筛选"),
				"limit":           map[string]any{"type": "integer", "minimum": 1, "maximum": 200, "description": "最多返回条数，范围 1..200"},
				"offset":          map[string]any{"type": "integer", "minimum": 0, "description": "跳过条数；仅提供 limit 时可使用"},
				"summary":         boolean("只返回 id、项目/列、标题、版本和更新时间等摘要字段"),
			}, "projectId"),
		},
		{
			"name":        "get_task",
			"description": "读取单张卡片完整详情，包括正文和评论。通常先用 list_tasks(summary=true) 找到目标 id，再调用此工具。",
			"inputSchema": object(map[string]any{
				"id": str("卡片 id，必填"),
			}, "id"),
		},
		{
			"name":        "create_task",
			"description": "在项目里新建卡片，默认追加到第一列或指定列的末尾。带 dispatchRuntime 则同一调用建卡并派发给该执行渠道。",
			"inputSchema": object(map[string]any{
				"projectId":       str("项目 id，必填"),
				"title":           str("卡片标题，必填"),
				"columnId":        str("目标列 id 或列名，省略则用项目第一列"),
				"description":     str("卡片正文"),
				"assignee":        str("负责人"),
				"dispatchRuntime": str("执行渠道 id（dsh|codex|pi|claude）；省略则只建卡不派发"),
			}, "projectId", "title"),
		},
		{
			"name":        "update_task",
			"description": "修改卡片字段（乐观并发：必须带当前 version，冲突时返回任务详情）。description/assignee 传空串可清空；title 不可空。",
			"inputSchema": object(map[string]any{
				"id":          str("卡片 id，必填"),
				"version":     num("卡片当前 version，必填"),
				"title":       str("新标题；不可为空串"),
				"description": str("新正文；空串清空"),
				"assignee":    str("新负责人；空串清空"),
			}, "id", "version"),
		},
		{
			"name":        "move_task",
			"description": "把卡片移动到目标列（列名或列 id），可指定放在某张卡之后或列首。",
			"inputSchema": object(map[string]any{
				"id":       str("卡片 id，必填"),
				"version":  num("卡片当前 version，必填"),
				"columnId": str("目标列 id 或列名，必填"),
				"afterId":  str("放在该卡片之后；省略表示列末"),
				"atTop":    boolean("true 时放到列首"),
			}, "id", "version", "columnId"),
		},
		{
			"name":        "add_comment",
			"description": "给卡片追加一条评论。mention 指定由哪个执行渠道跟进这条评论（dsh|codex|pi|claude），留空表示不指定。parentCommentId 回复同一卡片上的某条评论。",
			"inputSchema": object(map[string]any{
				"taskId":          str("卡片 id，必填"),
				"text":            str("评论正文，必填"),
				"mention":         str("可选：@ 给一个执行渠道，派发器会把这条评论交给它跟进"),
				"parentCommentId": str("可选：回复的父评论 id，必须属于同一卡片"),
			}, "taskId", "text"),
		},
		{
			"name":        "list_comments",
			"description": "列出卡片评论（按时间升序）。",
			"inputSchema": object(map[string]any{
				"taskId": str("卡片 id，必填"),
			}, "taskId"),
		},
		{
			"name":        "archive_task",
			"description": "归档卡片（软删除，乐观并发：必须带当前 version）。",
			"inputSchema": object(map[string]any{
				"id":      str("卡片 id，必填"),
				"version": num("卡片当前 version，必填"),
			}, "id", "version"),
		},
		{
			"name":        "add_relation",
			"description": "给卡片添加关系：parent（relatedId 为父卡）、blocks、blocked_by、related。需当前 version。",
			"inputSchema": object(map[string]any{
				"taskId":    str("卡片 id，必填"),
				"version":   num("卡片当前 version，必填"),
				"type":      str("关系类型：parent|blocks|blocked_by|related，必填"),
				"relatedId": str("关联卡片 id，必填"),
			}, "taskId", "version", "type", "relatedId"),
		},
		{
			"name":        "remove_relation",
			"description": "移除卡片关系。类型同 add_relation，需当前 version。",
			"inputSchema": object(map[string]any{
				"taskId":    str("卡片 id，必填"),
				"version":   num("卡片当前 version，必填"),
				"type":      str("关系类型：parent|blocks|blocked_by|related，必填"),
				"relatedId": str("关联卡片 id，必填"),
			}, "taskId", "version", "type", "relatedId"),
		},
		{
			"name":        "release_execution",
			"description": "把卡片执行记录收敛为 released，释放写锁，以便重新派发或评论跟进。",
			"inputSchema": object(map[string]any{
				"taskId": str("卡片 id，必填"),
			}, "taskId"),
		},
		{
			"name":        "followup_comment",
			"description": "把已保存评论交回原会话跟进（对应 HTTP /continue）。",
			"inputSchema": object(map[string]any{
				"taskId":    str("卡片 id，必填"),
				"commentId": str("评论 id，必填"),
			}, "taskId", "commentId"),
		},
	}
}

// callTool runs one tool against the HTTP API and returns raw JSON text.
func (s *server) callTool(name string, raw json.RawMessage) (string, error) {
	var args map[string]json.RawMessage
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("arguments must be an object: %w", err)
		}
	}
	if args == nil {
		args = map[string]json.RawMessage{}
	}
	if err := rejectUnknownArgs(name, args); err != nil {
		return "", err
	}
	switch name {
	case "list_projects":
		return s.get("/api/projects")
	case "create_project":
		projectName, err := requiredString(args, "name")
		if err != nil {
			return "", err
		}
		body := map[string]any{"name": projectName}
		if hasArgument(args, "workspacePath") {
			workspacePath, err := optionalString(args, "workspacePath")
			if err != nil {
				return "", err
			}
			body["workspacePath"] = workspacePath
		}
		return s.send(http.MethodPost, "/api/projects", body, http.StatusCreated)
	case "list_tasks":
		projectID, err := requiredString(args, "projectId")
		if err != nil {
			return "", err
		}
		includeArchived, err := optionalBool(args, "includeArchived")
		if err != nil {
			return "", err
		}
		query := url.Values{"projectId": []string{projectID}}
		for _, field := range []string{"columnId", "assignee"} {
			if hasArgument(args, field) {
				value, err := optionalString(args, field)
				if err != nil {
					return "", err
				}
				query.Set(field, value)
			}
		}
		for _, field := range []string{"limit", "offset"} {
			if hasArgument(args, field) {
				value, err := requiredInt(args, field)
				if err != nil {
					return "", err
				}
				query.Set(field, fmt.Sprintf("%d", value))
			}
		}
		if hasArgument(args, "summary") {
			summary, err := optionalBool(args, "summary")
			if err != nil {
				return "", err
			}
			query.Set("summary", fmt.Sprintf("%t", summary))
		}
		if includeArchived {
			query.Set("includeArchived", "true")
		}
		return s.get("/api/tasks?" + query.Encode())
	case "get_task":
		id, err := requiredString(args, "id")
		if err != nil {
			return "", err
		}
		return s.get("/api/tasks/" + url.PathEscape(id))
	case "create_task":
		projectID, err := requiredString(args, "projectId")
		if err != nil {
			return "", err
		}
		title, err := requiredString(args, "title")
		if err != nil {
			return "", err
		}
		columnID, err := optionalString(args, "columnId")
		if err != nil {
			return "", err
		}
		if columnID != "" {
			columnID, err = s.resolveColumn(projectID, columnID)
			if err != nil {
				return "", err
			}
		}
		body := map[string]any{"projectId": projectID, "title": title, "columnId": columnID}
		if v, err := optionalString(args, "description"); err != nil {
			return "", err
		} else if v != "" {
			body["description"] = v
		}
		if v, err := optionalString(args, "assignee"); err != nil {
			return "", err
		} else if v != "" {
			body["assignee"] = v
		}
		if v, err := optionalString(args, "dispatchRuntime"); err != nil {
			return "", err
		} else if v != "" {
			body["dispatchRuntime"] = v
		}
		return s.send(http.MethodPost, "/api/tasks", body, http.StatusCreated)
	case "update_task":
		id, err := requiredString(args, "id")
		if err != nil {
			return "", err
		}
		version, err := requiredInt(args, "version")
		if err != nil {
			return "", err
		}
		body := map[string]any{"version": version}
		// Keys present (even as "") are forwarded so description/assignee can
		// be cleared; omitted keys leave the field unchanged. Empty title is
		// rejected by the service.
		for _, field := range []string{"title", "description", "assignee"} {
			if !hasArgument(args, field) {
				continue
			}
			v, err := optionalString(args, field)
			if err != nil {
				return "", err
			}
			body[field] = v
		}
		return s.send(http.MethodPatch, "/api/tasks/"+url.PathEscape(id), body, http.StatusOK)
	case "move_task":
		id, err := requiredString(args, "id")
		if err != nil {
			return "", err
		}
		version, err := requiredInt(args, "version")
		if err != nil {
			return "", err
		}
		want, err := requiredString(args, "columnId")
		if err != nil {
			return "", err
		}
		target, err := s.task(id)
		if err != nil {
			return "", err
		}
		columnID, err := s.resolveColumn(target.ProjectID, want)
		if err != nil {
			return "", err
		}
		body := map[string]any{"version": version, "columnId": columnID}
		if v, err := optionalString(args, "afterId"); err != nil {
			return "", err
		} else if v != "" {
			body["afterId"] = v
		}
		if v, err := optionalBool(args, "atTop"); err != nil {
			return "", err
		} else if v {
			body["atTop"] = true
		}
		return s.send(http.MethodPatch, "/api/tasks/"+url.PathEscape(id), body, http.StatusOK)
	case "add_comment":
		taskID, err := requiredString(args, "taskId")
		if err != nil {
			return "", err
		}
		text, err := requiredString(args, "text")
		if err != nil {
			return "", err
		}
		body := map[string]any{"text": text}
		if mention, err := optionalString(args, "mention"); err != nil {
			return "", err
		} else if mention != "" {
			body["mention"] = mention
		}
		if parentID, err := optionalString(args, "parentCommentId"); err != nil {
			return "", err
		} else if parentID != "" {
			body["parentCommentId"] = parentID
		}
		return s.send(http.MethodPost, "/api/tasks/"+url.PathEscape(taskID)+"/comments", body, http.StatusCreated)
	case "list_comments":
		taskID, err := requiredString(args, "taskId")
		if err != nil {
			return "", err
		}
		return s.get("/api/tasks/" + url.PathEscape(taskID) + "/comments")
	case "archive_task":
		id, err := requiredString(args, "id")
		if err != nil {
			return "", err
		}
		version, err := requiredInt(args, "version")
		if err != nil {
			return "", err
		}
		return s.send(http.MethodPost, "/api/tasks/"+url.PathEscape(id)+"/archive", map[string]any{"version": version}, http.StatusOK)
	case "add_relation", "remove_relation":
		taskID, err := requiredString(args, "taskId")
		if err != nil {
			return "", err
		}
		version, err := requiredInt(args, "version")
		if err != nil {
			return "", err
		}
		relType, err := requiredString(args, "type")
		if err != nil {
			return "", err
		}
		relatedID, err := requiredString(args, "relatedId")
		if err != nil {
			return "", err
		}
		switch relType {
		case "parent", "blocks", "blocked_by", "related":
			// ok
		default:
			return "", fmt.Errorf("type must be parent, blocks, blocked_by, or related")
		}
		method := http.MethodPost
		if name == "remove_relation" {
			method = http.MethodDelete
		}
		path := "/api/tasks/" + url.PathEscape(taskID) + "/relations/" + url.PathEscape(relType) + "/" + url.PathEscape(relatedID)
		return s.send(method, path, map[string]any{"version": version}, http.StatusOK)
	case "release_execution":
		taskID, err := requiredString(args, "taskId")
		if err != nil {
			return "", err
		}
		return s.do(http.MethodPost, "/api/tasks/"+url.PathEscape(taskID)+"/execution/release", nil, http.StatusOK)
	case "followup_comment":
		taskID, err := requiredString(args, "taskId")
		if err != nil {
			return "", err
		}
		commentID, err := requiredString(args, "commentId")
		if err != nil {
			return "", err
		}
		path := "/api/tasks/" + url.PathEscape(taskID) + "/comments/" + url.PathEscape(commentID) + "/continue"
		return s.do(http.MethodPost, path, nil, http.StatusAccepted)
	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

// --- HTTP helpers ---

func (s *server) get(path string) (string, error) {
	return s.do(http.MethodGet, path, nil, http.StatusOK)
}

func (s *server) send(method, path string, body any, want int) (string, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	return s.do(method, path, raw, want)
}

func (s *server) do(method, path string, body []byte, want int) (string, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, s.base+path, reader)
	if err != nil {
		return "", err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Agent", s.agent)
	resp, err := s.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("service unreachable at %s: %w", s.base, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != want {
		return "", apiError(resp.StatusCode, raw)
	}
	return strings.TrimSpace(string(raw)), nil
}

var taskImageRef = regexp.MustCompile(`/api/images/([a-f0-9]{64})`)

// taskImageBlocks parses a get_task JSON response and returns one MCP image
// content block per distinct /api/images/{hash} reference found in the body
// or comments. Failures are skipped: a missing image must not break reading
// the card.
func (s *server) taskImageBlocks(taskJSON string) []map[string]any {
	var task struct {
		Description string `json:"description"`
		Comments    []struct {
			Text string `json:"text"`
		} `json:"comments"`
	}
	if json.Unmarshal([]byte(taskJSON), &task) != nil {
		return nil
	}
	seen := map[string]bool{}
	var blocks []map[string]any
	gather := func(text string) {
		for _, m := range taskImageRef.FindAllStringSubmatch(text, -1) {
			hash := m[1]
			if seen[hash] {
				continue
			}
			seen[hash] = true
			data, mime := s.fetchImage(hash)
			if len(data) == 0 {
				continue
			}
			blocks = append(blocks, map[string]any{
				"type":     "image",
				"data":     base64.StdEncoding.EncodeToString(data),
				"mimeType": mime,
			})
		}
	}
	gather(task.Description)
	for _, c := range task.Comments {
		gather(c.Text)
	}
	return blocks
}

func (s *server) fetchImage(hash string) (data []byte, mime string) {
	resp, err := s.http.Get(s.base + "/api/images/" + hash)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return nil, ""
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 25<<20))
	if err != nil || len(raw) == 0 {
		return nil, ""
	}
	return raw, sniffMIME(raw)
}

func sniffMIME(data []byte) string {
	switch {
	case len(data) >= 4 && data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G':
		return "image/png"
	case len(data) >= 2 && data[0] == 0xff && data[1] == 0xd8:
		return "image/jpeg"
	case len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a"):
		return "image/gif"
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	}
	return "image/png"
}

// apiError renders an API error body, keeping the current task on conflicts
// so an agent can retry with the right version.
func apiError(status int, raw []byte) error {
	var body struct {
		Error string      `json:"error"`
		Task  *model.Task `json:"task"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || body.Error == "" {
		return fmt.Errorf("HTTP %d: %s", status, strings.TrimSpace(string(raw)))
	}
	if status == http.StatusConflict && body.Task != nil {
		cur, _ := json.Marshal(body.Task)
		return fmt.Errorf("version_conflict: task is at v%d: %s", body.Task.Version, cur)
	}
	return fmt.Errorf("HTTP %d: %s", status, body.Error)
}

func (s *server) task(id string) (model.Task, error) {
	req, err := http.NewRequest(http.MethodGet, s.base+"/api/tasks/"+url.PathEscape(id), nil)
	if err != nil {
		return model.Task{}, err
	}
	req.Header.Set("X-Agent", s.agent)
	resp, err := s.http.Do(req)
	if err != nil {
		return model.Task{}, fmt.Errorf("service unreachable at %s: %w", s.base, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return model.Task{}, apiError(resp.StatusCode, raw)
	}
	var task model.Task
	if err := json.Unmarshal(raw, &task); err != nil {
		return model.Task{}, err
	}
	return task, nil
}

// resolveColumn maps a column id or name in a project to its id.
func (s *server) resolveColumn(projectID, idOrName string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, s.base+"/api/projects/"+url.PathEscape(projectID)+"/board", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Agent", s.agent)
	resp, err := s.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("service unreachable at %s: %w", s.base, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return "", apiError(resp.StatusCode, raw)
	}
	var board service.Board
	if err := json.Unmarshal(raw, &board); err != nil {
		return "", err
	}
	for _, c := range board.Columns {
		if c.ID == idOrName || c.Name == idOrName {
			return c.ID, nil
		}
	}
	names := make([]string, 0, len(board.Columns))
	for _, c := range board.Columns {
		names = append(names, c.Name)
	}
	return "", fmt.Errorf("unknown column %q (available: %s)", idOrName, strings.Join(names, ", "))
}

// --- argument decoding ---

// toolAllowedArgs lists every known argument key per tool. callTool rejects
// any other key so hosts that skip schema validation still get a hard error.
var toolAllowedArgs = map[string][]string{
	"list_projects":     {},
	"create_project":    {"name", "workspacePath"},
	"list_tasks":        {"projectId", "includeArchived", "columnId", "assignee", "limit", "offset", "summary"},
	"get_task":          {"id"},
	"create_task":       {"projectId", "title", "columnId", "description", "assignee", "dispatchRuntime"},
	"update_task":       {"id", "version", "title", "description", "assignee"},
	"move_task":         {"id", "version", "columnId", "afterId", "atTop"},
	"add_comment":       {"taskId", "text", "mention", "parentCommentId"},
	"list_comments":     {"taskId"},
	"archive_task":      {"id", "version"},
	"add_relation":      {"taskId", "version", "type", "relatedId"},
	"remove_relation":   {"taskId", "version", "type", "relatedId"},
	"release_execution": {"taskId"},
	"followup_comment":  {"taskId", "commentId"},
}

func rejectUnknownArgs(name string, args map[string]json.RawMessage) error {
	allowed, ok := toolAllowedArgs[name]
	if !ok {
		return nil // unknown tool handled by the switch
	}
	known := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		known[key] = struct{}{}
	}
	var unknown []string
	for key := range args {
		if _, ok := known[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf("unknown argument(s): %s", strings.Join(unknown, ", "))
}

func requiredString(args map[string]json.RawMessage, key string) (string, error) {
	v, err := optionalString(args, key)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(v) == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return v, nil
}

func optionalString(args map[string]json.RawMessage, key string) (string, error) {
	raw, ok := args[key]
	if !ok || string(raw) == "null" {
		return "", nil
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", fmt.Errorf("%s must be a string", key)
	}
	return v, nil
}

func requiredInt(args map[string]json.RawMessage, key string) (int, error) {
	raw, ok := args[key]
	if !ok || string(raw) == "null" {
		return 0, fmt.Errorf("%s is required", key)
	}
	var v int
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return v, nil
}

func hasArgument(args map[string]json.RawMessage, key string) bool {
	raw, ok := args[key]
	return ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func optionalBool(args map[string]json.RawMessage, key string) (bool, error) {
	raw, ok := args[key]
	if !ok || string(raw) == "null" {
		return false, nil
	}
	var v bool
	if err := json.Unmarshal(raw, &v); err != nil {
		return false, fmt.Errorf("%s must be a boolean", key)
	}
	return v, nil
}
