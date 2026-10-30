package client_test

import (
	"path/filepath"
	"testing"

	"taskboard-native/internal/client"
	"taskboard-native/internal/service"
)

// TestAgentIdentityFollowsDisplayName walks the chain the 显示名 setting
// depends on, end to end over the real service: the client signs requests
// with X-Agent, the service records it as the task's last writer, and that
// is the name a card's provenance line shows. SetAgent is what the settings
// field calls on every edit.
func TestAgentIdentityFollowsDisplayName(t *testing.T) {
	srv, err := service.Start("127.0.0.1:0", filepath.Join(t.TempDir(), "board.db"))
	if err != nil {
		t.Fatalf("start service: %v", err)
	}
	t.Cleanup(func() { srv.Close() })

	c := client.New(&client.Discover{Addr: srv.Addr}, "gui")
	// A newly opened service starts projectless; create the fixture this
	// scenario writes into.
	if _, err := c.CreateProject("local"); err != nil {
		t.Fatalf("fixture project: %v", err)
	}
	projects, err := c.Projects()
	if err != nil || len(projects) == 0 {
		t.Fatalf("projects: %v", err)
	}
	p := projects[0]
	if len(p.Columns) == 0 {
		t.Fatal("project has no columns")
	}
	col := p.Columns[0].ID

	unset, err := c.CreateTask(p.ID, col, "未设置显示名", "", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if unset.UpdatedBy != "gui" {
		t.Fatalf("unset display name wrote as %q, want gui", unset.UpdatedBy)
	}

	c.SetAgent("豆爸")
	named, err := c.CreateTask(p.ID, col, "已设置显示名", "", "")
	if err != nil {
		t.Fatalf("create after SetAgent: %v", err)
	}
	if named.UpdatedBy != "豆爸" {
		t.Fatalf("display name wrote as %q, want 豆爸", named.UpdatedBy)
	}

	// The window resolves the fallback; an empty name must not erase the
	// identity the client already carries.
	c.SetAgent("")
	kept, err := c.CreateTask(p.ID, col, "清空显示名后", "", "")
	if err != nil {
		t.Fatalf("create after empty SetAgent: %v", err)
	}
	if kept.UpdatedBy != "豆爸" {
		t.Fatalf("empty SetAgent changed the identity to %q, want 豆爸", kept.UpdatedBy)
	}
}
