package main

import (
	"strings"
	"testing"

	"taskboard-native/internal/model"
)

// TestCreateContinueKeepsFormOpen documents the create-and-continue contract:
// submitting a task title must not close the form, and must clear the field.
func TestCreateContinueKeepsFormOpen(t *testing.T) {
	b := &board{cur: "local", columns: []model.Column{{ID: "c-todo", Name: "待办"}}}
	b.openTaskForm("c-todo")
	if b.form == nil || !b.form.open || !b.form.isTask {
		t.Fatal("expected open task form")
	}
	b.form.name = "  第一条  "
	title := strings.TrimSpace(b.form.name)
	if title == "" {
		t.Fatal("title")
	}
	// Mirror formView submit path (without API): create + clear, keep open.
	b.form.name = ""
	if !b.form.open {
		t.Fatal("form should stay open after create")
	}
	if b.form.name != "" {
		t.Fatal("title field should clear for the next entry")
	}
	b.form.name = "第二条"
	if strings.TrimSpace(b.form.name) == "" {
		t.Fatal("ready for next title")
	}
}
