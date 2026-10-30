package executor

import (
	"context"
	"fmt"

	"taskboard-native/internal/model"
)

func (b *desktopBridge) requireIdle(ctx context.Context, session string) error {
	var state struct {
		Status string `json:"status"`
	}
	if err := b.call(ctx, "session.getState", desktopSessionParams(session), &state); err != nil {
		return err
	}
	if state.Status != "idle" {
		return fmt.Errorf("原会话当前为 %s，请等待当前回合结束再跟进", state.Status)
	}
	return nil
}

func (d *Dispatcher) continueDesktop(ctx context.Context, b *desktopBridge, task model.Task, run *model.Execution, text, actor string) {
	params := desktopSessionParams(run.SessionID)
	// Current bridge requires a platform ID even for legacy external IDs.
	if _, ok := params["sessionId"]; !ok {
		params["sessionId"] = run.SessionID
	}
	params["clientMessageId"] = "taskboard-comment-" + run.FollowupID
	params["content"] = text
	var reply struct {
		Accepted bool   `json:"accepted"`
		OK       *bool  `json:"ok"`
		Message  string `json:"message"`
	}
	err := b.call(ctx, "session.startTurn", params, &reply)
	if err != nil || !reply.Accepted {
		if err == nil && reply.OK != nil && !*reply.OK {
			d.store.SetFollowupStatus(run.FollowupID, "failed")
			d.desktopFailure(task, run, actor, fmt.Errorf("原会话拒绝跟进：%s", reply.Message))
			return
		}
		d.store.SetFollowupStatus(run.FollowupID, "unknown")
		d.desktopState(run, "unknown", fmt.Sprintf("评论投递回执不确定：%v %s", err, reply.Message))
		d.comment(task.ID, actor, "评论已保存，但原会话是否接收不确定；请核对桌面会话，不自动重发。")
		return
	}
	d.store.SetFollowupStatus(run.FollowupID, "sent")
	if err := d.desktopState(run, "running", ""); err != nil {
		d.comment(task.ID, actor, "评论已接收，但记录状态失败："+err.Error())
		return
	}
	d.comment(task.ID, actor, "已交回原 DSH 会话跟进评论 "+run.FollowupID+"。")
	d.trackDesktop(ctx, b, task, run, actor)
}
