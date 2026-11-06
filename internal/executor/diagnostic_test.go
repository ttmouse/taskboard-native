package executor

import (
	"fmt"
	"strings"
	"testing"
)

func TestDispatchFailureRecordsHerdrCauseAndKeepsColumn(t *testing.T) {
	installFakeRuntime(t, RuntimePi, `printf 'extension warning\n' >&2
printf '请求失败：需要 GitHub 授权并 star 仓库后使用 EAC 模型\n'
for i in 1 2 3 4 5 6 7 8 9 10; do printf '%0100d\n' 0; done
printf 'trace end\n'
exit 1`)
	st := newTestStore(t)
	p, task := projectWithWorkspace(t, st)
	installFakeHerdr(t, p.WorkspacePath+"/missing", "done")
	d := NewDispatcher(st)
	defer d.Close()
	if err := d.Dispatch(task.ID, RuntimePi, "test"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "failure diagnosis", func() (bool, string) {
		cur, err := st.Task(task.ID)
		if err != nil {
			return false, err.Error()
		}
		for _, comment := range cur.Comments {
			if !strings.Contains(comment.Text, "没能交给 Herdr") {
				continue
			}
			if !strings.Contains(comment.Text, "无法在 Herdr 创建项目 space") {
				t.Errorf("diagnosis missing Herdr cause: %s", comment.Text)
			}
			if cur.ColumnID != p.Columns[0].ID {
				t.Error("program must leave the failed task column unchanged")
			}
			return true, ""
		}
		return false, fmt.Sprint(cur.Comments)
	})
}
