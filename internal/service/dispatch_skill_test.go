package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"taskboard-native/internal/client"
	"taskboard-native/internal/model"
)

// The nonce exists only in the original card's comments, never in the dispatched
// prompt. Producing it proves the worker read the right board on its actual port.
func TestLiveDispatchSkillHandoff(t *testing.T) {
	if os.Getenv("TASKBOARD_TEST_SKILL_HANDOFF") != "1" {
		t.Skip("opt-in real DSH skill handoff")
	}
	srv := start(t)
	raw, err := os.ReadFile(os.Getenv("TASKBOARD_LIVE_RUNTIME_CONFIG"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srv.DBPath+".runtimes.json", raw, 0600); err != nil {
		t.Fatal(err)
	}
	c := client.New(&client.Discover{Addr: srv.Addr}, "skill-handoff-test")
	workspace := t.TempDir()
	proof := filepath.Join(workspace, "skill-proof.txt")
	p, err := c.CreateProjectWithPath("技能交接隔离验收", workspace)
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("按派发说明加载 manage-taskboard-native，读取原任务的全部评论。找到评论中的验证口令，将口令本身原样写入 %s。不要修改其他文件，按技能自行认领、执行并回写原卡结果与状态，GET 确认成功。先在原卡用【执行结果】评论回写变更、验证证据及 SKILL_HANDOFF_OK，再移到评审并 GET 确认。最终回复说明技能加载方式。", proof)
	task, err := c.CreateTask(p.ID, p.Columns[0].ID, "技能与原卡读取验收", body, "")
	if err != nil {
		t.Fatal(err)
	}
	nonce := fmt.Sprintf("SKILL_READ_%d", time.Now().UnixNano())
	status, response := request(t, srv, "POST", "/api/tasks/"+task.ID+"/comments", "skill-handoff-test", map[string]string{"text": "验证口令：" + nonce})
	if status != 201 {
		t.Fatalf("comment: %d %s", status, response)
	}
	if err := c.Dispatch(task.ID, "dsh"); err != nil {
		t.Fatal(err)
	}
	cur := awaitDesktopTask(t, c, task.ID, 120*time.Second, func(cur model.Task) bool {
		return cur.Execution != nil && cur.Execution.Status == "returned" && cur.ColumnID == p.Columns[2].ID
	})
	data, err := os.ReadFile(proof)
	if err != nil || strings.TrimSpace(string(data)) != nonce {
		t.Fatalf("worker did not read original comments: %q %v", data, err)
	}
	if !strings.Contains(fmt.Sprint(cur.Comments), "SKILL_HANDOFF_OK") {
		t.Fatal("missing skill handoff result")
	}
	if cur.UpdatedBy != "DSH" {
		t.Fatalf("review was not attributed to executing Agent: %s", cur.UpdatedBy)
	}
	found := false
	for _, comment := range cur.Comments {
		if strings.HasPrefix(comment.Text, "【执行结果】") && comment.Author == "DSH" && strings.Contains(comment.Text, "SKILL_HANDOFF_OK") {
			found = true
		}
	}
	if !found {
		t.Fatal("missing Agent-authored result comment")
	}
	t.Logf("board=%s task=%s session=%s proof=%s", srv.URL, task.ID, cur.Execution.SessionID, nonce)
	t.Logf("result=%s", cur.Comments[len(cur.Comments)-1].Text)
}
