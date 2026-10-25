package main

import (
	"testing"
	"time"

	"taskboard-native/internal/model"
)

func TestTasksInHonorsSearchQuery(t *testing.T) {
	b := &board{
		tasks: []model.Task{
			{ID: "1", ColumnID: "c-todo", Title: "边缘避让", Description: "motion", Position: 1, CreatedAt: time.Now()},
			{ID: "2", ColumnID: "c-todo", Title: "音效", Description: "sfx", Position: 2, CreatedAt: time.Now()},
			{ID: "3", ColumnID: "c-doing", Title: "搜索框", Description: "filter titles", Position: 1, CreatedAt: time.Now()},
		},
	}
	if n := len(b.tasksIn("c-todo")); n != 2 {
		t.Fatalf("no query: todo=%d want 2", n)
	}
	b.searchQuery = "边缘"
	got := b.tasksIn("c-todo")
	if len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("title match: %+v", got)
	}
	b.searchQuery = "filter"
	got = b.tasksIn("c-doing")
	if len(got) != 1 || got[0].ID != "3" {
		t.Fatalf("body match: %+v", got)
	}
	b.searchQuery = "没有这个"
	if n := len(b.tasksIn("c-todo")); n != 0 {
		t.Fatalf("miss: %d", n)
	}
}
