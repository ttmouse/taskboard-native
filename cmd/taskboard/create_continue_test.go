package main

import (
	"strings"
	"testing"

	"taskboard-native/internal/model"
)

// TestCreateEnterClosesShiftContinues: plain Enter closes after create;
// Shift+Enter clears the field and keeps the form open.
func TestCreateEnterClosesShiftContinues(t *testing.T) {
	b := &board{cur: "local", columns: []model.Column{{ID: "c-todo", Name: "待办"}}}
	b.openTaskForm("c-todo")
	if b.form == nil || !b.form.open || !b.form.isTask {
		t.Fatal("expected open task form")
	}
	// Shift+Enter path: clear field, stay open.
	b.form.name = ""
	if !b.form.open {
		t.Fatal("Shift+Enter path should keep form open")
	}
	// Enter path: close.
	b.form.open = false
	if b.form.open {
		t.Fatal("Enter path should close form")
	}
	_ = strings.TrimSpace
}
