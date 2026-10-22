package catalog

import "testing"

// TestDashboardLensesAndEcho: the dashboards view registers the all/mine
// owner lenses and echoes the matching `dtctl get dashboards [--mine]` per
// active lens, so 'c' copies the command equivalent to what's on screen.
func TestDashboardLensesAndEcho(t *testing.T) {
	spec := Lookup("dashboards")
	if spec == nil {
		t.Fatal("dashboards view not registered")
	}
	for _, alias := range []string{"dashboard", "dash"} {
		if Lookup(alias) != spec {
			t.Errorf("alias %q does not resolve to dashboards", alias)
		}
	}
	want := []string{"all", "mine"}
	if len(spec.Lenses) != len(want) {
		t.Fatalf("unexpected dashboard lenses: %+v", spec.Lenses)
	}
	for i, name := range want {
		if spec.Lenses[i].Name != name {
			t.Errorf("dashboard lens %d = %q, want %q", i, spec.Lenses[i].Name, name)
		}
	}
	// The named indices the source keys off must line up with the strip order.
	if DashLensAll != 0 || DashLensMine != 1 {
		t.Fatalf("lens indices drifted from the strip: all=%d mine=%d", DashLensAll, DashLensMine)
	}
	if got := spec.Echo(Scope{Lens: DashLensAll}, ""); got != "dtctl get dashboards" {
		t.Errorf("all-lens echo = %q", got)
	}
	if got := spec.Echo(Scope{Lens: DashLensMine}, ""); got != "dtctl get dashboards --mine" {
		t.Errorf("mine-lens echo = %q", got)
	}
}

// TestDashboardUserIDs: only dashboard records with a UUID-shaped owner yield
// an id to resolve — non-dashboards and non-UUID owners are skipped so the
// inspector never issues (or errors on) an IAM lookup where it can't apply.
func TestDashboardUserIDs(t *testing.T) {
	uuid := "11111111-2222-3333-4444-555555555555"
	if ids := DashboardUserIDs(map[string]any{"type": "dashboard", "owner": uuid}); len(ids) != 1 || ids[0] != uuid {
		t.Errorf("dashboard owner uuid not returned: %+v", ids)
	}
	if ids := DashboardUserIDs(map[string]any{"type": "dashboard", "owner": "monitoring-team"}); ids != nil {
		t.Errorf("non-uuid owner must be skipped: %+v", ids)
	}
	if ids := DashboardUserIDs(map[string]any{"type": "host", "owner": uuid}); ids != nil {
		t.Errorf("non-dashboard record must be skipped: %+v", ids)
	}
	if !IsDashboardUserField("owner") || IsDashboardUserField("id") {
		t.Error("owner must be a user field and id must not")
	}
}
