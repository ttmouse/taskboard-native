package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"taskboard-native/internal/client"
	"taskboard-native/internal/model"
	"testing"
	"time"
)

func TestContinueHTTPRequiresExplicitRequestAndOriginalSession(t *testing.T) {
	srv := start(t)
	c := client.New(&client.Discover{Addr: srv.Addr}, "user")
	p, err := c.CreateProjectWithPath("followup", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	task, err := c.CreateTask(p.ID, p.Columns[0].ID, "followup", "", "")
	if err != nil {
		t.Fatal(err)
	}
	cm, err := c.AddComment(task.ID, "ordinary")
	if err != nil {
		t.Fatal(err)
	}
	current, _ := c.Task(task.ID)
	if current.Execution != nil {
		t.Fatal("ordinary comment dispatched work")
	}
	route := "/api/tasks/" + task.ID + "/comments/" + cm.ID + "/continue"
	status, raw := request(t, srv, "POST", route, "user", nil)
	if status != 400 || !strings.Contains(string(raw), "原会话") {
		t.Fatalf("%d %s", status, raw)
	}
	status, _ = request(t, srv, "POST", "/api/tasks/"+task.ID+"/comments/foreign/continue", "user", nil)
	if status != 404 {
		t.Fatal(status)
	}
}

func TestLiveCommentContinuation(t *testing.T) {
	runtime := os.Getenv("TASKBOARD_LIVE_FOLLOWUP")
	if runtime == "" {
		t.Skip("opt-in real model continuation")
	}
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(root, "board.db")
	config, err := os.ReadFile(os.Getenv("TASKBOARD_LIVE_RUNTIME_CONFIG"))
	if err != nil {
		t.Fatal(err)
	}
	profile := db + ".runtimes.json"
	if err := os.WriteFile(profile, config, 0600); err != nil {
		t.Fatal(err)
	}
	srv, err := Start("127.0.0.1:0", db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { srv.Close() }()
	// Desktop bridges do not inject process environment; the isolated workspace
	// names its own board, as a real project's local instructions would.
	rules := fmt.Sprintf("# 隔离验收工作区\n唯一看板地址：%s。所有看板 HTTP 请求都使用这个地址，不能连接默认端口。\n请读取 /Users/douba/.agents/skills/manage-taskboard-native/SKILL.md。tb 不在 PATH 时直接使用 curl HTTP API。使用自身渠道身份 X-Agent，按技能完成原卡回写。\n", srv.URL)
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte(rules), 0600); err != nil {
		t.Fatal(err)
	}
	c := client.New(&client.Discover{Addr: srv.Addr}, "followup-acceptance")
	p, err := c.CreateProjectWithPath("原会话跟进验收", workspace)
	if err != nil {
		t.Fatal(err)
	}
	marker := fmt.Sprintf("FOLLOWUP_%d", time.Now().UnixNano())
	artifact := filepath.Join(workspace, "proof.txt")
	body := fmt.Sprintf("这是隔离验收。禁止创建子 Agent。按 manage-taskboard-native 认领、执行、验证和回写。只创建 %s，内容为 FIRST。将口令 %s 记在本次对话中，第一轮不要将口令写入任何文件或结果评论。写原卡结果评论【第一轮完成】，移到评审，GET 确认。", artifact, marker)
	task, err := c.CreateTask(p.ID, p.Columns[0].ID, "原会话两轮验收", body, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Dispatch(task.ID, runtime); err != nil {
		t.Fatal(err)
	}
	current := awaitDesktopTask(t, c, task.ID, 300*time.Second, func(cur model.Task) bool {
		return cur.Execution != nil && cur.Execution.Status == "returned" && cur.ColumnID == p.Columns[2].ID
	})
	original := current.Execution.SessionID
	first, err := os.ReadFile(artifact)
	if err != nil || strings.TrimSpace(string(first)) != "FIRST" {
		t.Fatalf("first=%q err=%v", first, err)
	}
	t.Logf("%s initial session=%s", runtime, original)
	// Remove the secret from the card; the followup must retain prior dialogue.
	description := "第一轮已完成，后续按新评论执行。"
	if _, err := c.Update(task.ID, current.Version, nil, &description, nil); err != nil {
		t.Fatal(err)
	}
	addr := srv.Addr
	srv.Close()
	srv, err = Start(addr, db)
	if err != nil {
		t.Fatal(err)
	}
	c = client.New(&client.Discover{Addr: srv.Addr}, "followup-acceptance")
	cm, err := c.AddComment(task.ID, "这是第二轮明确授权。在原对话里找第一轮让你记住的 FOLLOWUP_ 开头口令，将 proof.txt 内容改成该完整口令。不要从其他会话猜口令。只改这个文件。以自身身份把原卡移到进行中，验证后写【跟进验收通过】结果评论，再移回评审并 GET 确认。")
	if err != nil {
		t.Fatal(err)
	}
	// A preflight configuration failure must retain the saved comment and permit retry.
	if runtime != "dsh" {
		if err := os.WriteFile(profile, []byte("{"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := c.ContinueComment(task.ID, cm.ID); err == nil {
			t.Fatal("invalid config was accepted")
		}
		if err := os.WriteFile(profile, config, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.ContinueComment(task.ID, cm.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.ContinueComment(task.ID, cm.ID); err != nil {
		t.Fatal(err)
	}
	current = awaitDesktopTask(t, c, task.ID, 300*time.Second, func(cur model.Task) bool {
		if cur.Execution == nil || cur.Execution.Status != "returned" || cur.Execution.FollowupID != cm.ID || cur.ColumnID != p.Columns[2].ID {
			return false
		}
		for _, item := range cur.Comments {
			if strings.Contains(item.Text, "跟进验收通过") && item.ID != cm.ID {
				return true
			}
		}
		return false
	})
	got, err := os.ReadFile(artifact)
	if err != nil || strings.TrimSpace(string(got)) != marker || current.Execution.SessionID != original {
		t.Fatalf("followup artifact=%q err=%v session=%+v", got, err, current.Execution)
	}
	t.Logf("%s: real initial dispatch + saved comment + restart + retry + exact-session continuation + result and review verified; session=%s", runtime, original)
}
