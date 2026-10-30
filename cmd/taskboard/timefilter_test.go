package main

import (
	"testing"
	"time"

	"taskboard-native/internal/model"
)

func TestTimeFilterHits(t *testing.T) {
	now := time.Now()
	loc := now.Location()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	yesterday := start.AddDate(0, 0, -1)
	tenDaysAgo := start.AddDate(0, 0, -10)

	mk := func(ts time.Time) model.Task { return model.Task{UpdatedAt: ts} }
	b := &board{}

	// 全部时间 matches everything, including the zero time.
	b.timeFilter = ""
	if !b.timeFilterHits(mk(time.Time{})) {
		t.Error("empty filter should match any task")
	}

	// 今天: only the current calendar day.
	b.timeFilter = "today"
	if !b.timeFilterHits(mk(start.Add(2 * time.Hour))) {
		t.Error("today +2h should match today")
	}
	if b.timeFilterHits(mk(yesterday.Add(23 * time.Hour))) {
		t.Error("yesterday should not match today")
	}
	if b.timeFilterHits(mk(start.AddDate(0, 0, 1))) {
		t.Error("tomorrow should not match today")
	}

	// 昨天: the day before today only.
	b.timeFilter = "yesterday"
	if !b.timeFilterHits(mk(yesterday.Add(2 * time.Hour))) {
		t.Error("yesterday +2h should match yesterday")
	}
	if b.timeFilterHits(mk(start.Add(2 * time.Hour))) {
		t.Error("today should not match yesterday")
	}

	// 近7天: today inclusive, back 6 days.
	b.timeFilter = "7d"
	if !b.timeFilterHits(mk(start.AddDate(0, 0, -5))) {
		t.Error("5 days ago should match 7d")
	}
	if b.timeFilterHits(mk(tenDaysAgo)) {
		t.Error("10 days ago should not match 7d")
	}

	// 近30天: today inclusive, back 29 days.
	b.timeFilter = "30d"
	if !b.timeFilterHits(mk(tenDaysAgo)) {
		t.Error("10 days ago should match 30d")
	}
	if b.timeFilterHits(mk(start.AddDate(0, 0, -40))) {
		t.Error("40 days ago should not match 30d")
	}
}

func TestTasksInHonorsTimeFilter(t *testing.T) {
	now := time.Now()
	loc := now.Location()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	colID := "col-1"
	b := &board{
		tasks: []model.Task{
			{ID: "t-today", ColumnID: colID, Title: "今天任务", UpdatedAt: start.Add(1 * time.Hour)},
			{ID: "t-yesterday", ColumnID: colID, Title: "昨天任务", UpdatedAt: start.AddDate(0, 0, -1).Add(1 * time.Hour)},
			{ID: "t-other", ColumnID: "col-2", Title: "其他列", UpdatedAt: start.Add(1 * time.Hour)},
		},
	}
	b.timeFilter = "today"
	got := b.tasksIn(colID)
	if len(got) != 1 || got[0].ID != "t-today" {
		t.Fatalf("today filter: expected only t-today, got %d tasks", len(got))
	}
	b.timeFilter = ""
	if len(b.tasksIn(colID)) != 2 {
		t.Fatalf("empty filter: expected 2 tasks in col-1")
	}
}
