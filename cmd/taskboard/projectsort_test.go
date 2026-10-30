package main

import (
	"testing"
	"time"

	"taskboard-native/internal/model"
)

func TestSortedProjects(t *testing.T) {
	base := time.Date(2026, 10, 28, 12, 0, 0, 0, time.Local)
	mk := func(id string, created time.Time, active time.Time) model.Project {
		return model.Project{ID: id, Name: id, CreatedAt: created, ActiveAt: active}
	}
	b := &board{
		projects: []model.Project{
			mk("old-active", base.AddDate(0, 0, -30), base.AddDate(0, 0, -30)), // 老但无近期活动
			mk("recent", base.AddDate(0, 0, -2), base.Add(-1*time.Hour)),       // 最近活跃
			mk("newest", base.AddDate(0, 0, -1), base.Add(-2*time.Hour)),       // 最新创建
		},
	}

	// 默认：保持服务端顺序。
	b.projectSort = ""
	got := b.sortedProjects()
	wantOrder := []string{"old-active", "recent", "newest"}
	for i, id := range wantOrder {
		if got[i].ID != id {
			t.Fatalf("default order: got %s at %d, want %s", got[i].ID, i, id)
		}
	}

	// 按更新时间：最近活跃在前。
	b.projectSort = "updated"
	got = b.sortedProjects()
	wantOrder = []string{"recent", "newest", "old-active"}
	for i, id := range wantOrder {
		if got[i].ID != id {
			t.Fatalf("updated order: got %s at %d, want %s", got[i].ID, i, id)
		}
	}

	// 按创建时间：最新创建在前。
	b.projectSort = "created"
	got = b.sortedProjects()
	wantOrder = []string{"newest", "recent", "old-active"}
	for i, id := range wantOrder {
		if got[i].ID != id {
			t.Fatalf("created order: got %s at %d, want %s", got[i].ID, i, id)
		}
	}

	// 原切片不被修改。
	if b.projects[0].ID != "old-active" || len(b.projects) != 3 {
		t.Fatal("sortedProjects must not mutate b.projects")
	}
}
