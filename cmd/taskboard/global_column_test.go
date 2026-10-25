package main

import (
	"testing"

	"taskboard-native/internal/model"
)

func TestMapColumnsInGlobalView(t *testing.T) {
	b := &board{
		cur: curGlobal,
		projects: []model.Project{
			{ID: "local", Name: "local", Columns: []model.Column{
				{ID: "loc-todo", Name: "待办"},
				{ID: "loc-done", Name: "完成"},
			}},
			{ID: "native", Name: "taskboard-native", Columns: []model.Column{
				{ID: "nat-todo", Name: "待办"},
				{ID: "nat-done", Name: "完成"},
			}},
		},
		columns: []model.Column{
			{ID: "loc-todo", Name: "待办"},
			{ID: "loc-done", Name: "完成"},
		},
	}
	if got := b.mapToProjectColumnID("loc-done", "native"); got != "nat-done" {
		t.Fatalf("mapToProjectColumnID = %q, want nat-done", got)
	}
	if got := b.mapToBoardColumnID("nat-done", "native"); got != "loc-done" {
		t.Fatalf("mapToBoardColumnID = %q, want loc-done", got)
	}
	b.cur = "native"
	if got := b.mapToProjectColumnID("nat-done", "native"); got != "nat-done" {
		t.Fatalf("single-project passthrough = %q", got)
	}
}
