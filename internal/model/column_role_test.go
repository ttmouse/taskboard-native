package model

import "testing"

func TestValidColumnRole(t *testing.T) {
	cases := []struct {
		role string
		want bool
	}{
		{ColumnRoleIntake, true},
		{ColumnRoleDoing, true},
		{ColumnRoleReview, true},
		{ColumnRoleDone, true},
		{"", true},
		{"finished", false},
		{"完成", false},
		{"Done", false},
		{" intake", false},
	}
	for _, tc := range cases {
		if got := ValidColumnRole(tc.role); got != tc.want {
			t.Errorf("ValidColumnRole(%q) = %v, want %v", tc.role, got, tc.want)
		}
	}
}

func TestColumnByRole(t *testing.T) {
	renamed := []Column{
		{ID: "c1", Name: "收集箱", Role: ColumnRoleIntake},
		{ID: "c2", Name: "在做", Role: ColumnRoleDoing},
		{ID: "c3", Name: "验收中", Role: ColumnRoleReview},
		{ID: "c4", Name: "已上线", Role: ColumnRoleDone},
	}
	legacy := []Column{
		{ID: "l1", Name: "待办"},
		{ID: "l2", Name: "进行中"},
		{ID: "l3", Name: "评审"},
		{ID: "l4", Name: "完成"},
	}
	mixed := []Column{
		// A column still carrying the legacy name but no role, plus a
		// renamed column that does hold the role: the role wins.
		{ID: "m1", Name: "完成"},
		{ID: "m2", Name: "已上线", Role: ColumnRoleDone},
	}
	cases := []struct {
		name    string
		columns []Column
		role    string
		want    string
		wantOK  bool
	}{
		{"role on renamed board", renamed, ColumnRoleIntake, "c1", true},
		{"role done", renamed, ColumnRoleDone, "c4", true},
		{"renamed board has no legacy names", renamed, ColumnRoleReview, "c3", true},
		{"legacy name fallback", legacy, ColumnRoleIntake, "l1", true},
		{"legacy name fallback done", legacy, ColumnRoleDone, "l4", true},
		{"role beats legacy name", mixed, ColumnRoleDone, "m2", true},
		{"unknown role value", legacy, "finished", "", false},
		{"empty role never resolves", legacy, "", "", false},
		{"renamed board without roles", []Column{{ID: "x", Name: "已上线"}}, ColumnRoleDone, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ColumnByRole(tc.columns, tc.role)
			if ok != tc.wantOK || got.ID != tc.want {
				t.Fatalf("ColumnByRole(%q) = %+v, %v; want %q, %v", tc.role, got, ok, tc.want, tc.wantOK)
			}
		})
	}

	// The project method is the same resolver.
	p := Project{ID: "p", Columns: renamed}
	if c, ok := p.ColumnByRole(ColumnRoleReview); !ok || c.ID != "c3" {
		t.Fatalf("Project.ColumnByRole = %+v, %v", c, ok)
	}
}

func TestLegacyColumnName(t *testing.T) {
	cases := map[string]string{
		ColumnRoleIntake: "待办",
		ColumnRoleDoing:  "进行中",
		ColumnRoleReview: "评审",
		ColumnRoleDone:   "完成",
		"":               "",
		"finished":       "",
	}
	for role, want := range cases {
		if got := LegacyColumnName(role); got != want {
			t.Errorf("LegacyColumnName(%q) = %q, want %q", role, got, want)
		}
	}
}
