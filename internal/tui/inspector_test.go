package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
)

// TestInspectorResolvesDashboardOwner: opening a dashboard's detail view
// resolves the owner UUID to an IAM display name and appends it next to the
// raw id — the list/preview keep the bare UUID (resolution is detail-only).
func TestInspectorResolvesDashboardOwner(t *testing.T) {
	uuid := "11111111-2222-3333-4444-555555555555"
	rec := map[string]any{"type": "dashboard", "name": "Fleet", "owner": uuid}
	ds := &dataSource{sources: map[string]Source{
		"resolve-users": func(_ context.Context, _ catalog.Scope, arg string) ([]map[string]any, error) {
			if arg != uuid {
				t.Errorf("resolve arg = %q, want the owner uuid", arg)
			}
			return []map[string]any{{"id": uuid, "name": "Ada Lovelace (ada@x.io)"}}, nil
		},
	}}
	v := newInspectorView(ds, "Fleet", rec)

	cmd := v.resolveOwners()
	if cmd == nil {
		t.Fatal("expected an IAM resolution command for the dashboard owner")
	}
	v.Update(cmd())

	if v.userNames[uuid] != "Ada Lovelace (ada@x.io)" {
		t.Fatalf("owner UUID not resolved: %+v", v.userNames)
	}
	got := v.withUserName(renderString("owner", uuid, 80))
	if !strings.Contains(got.lines[0], "Ada Lovelace") {
		t.Errorf("resolved name not appended to owner value: %q", got.lines[0])
	}
	// The append is scoped to user fields, not any value sharing the UUID.
	if !catalog.IsDashboardUserField("owner") || catalog.IsDashboardUserField("id") {
		t.Errorf("owner must be a user field and id must not")
	}
}

// A non-dashboard record must not trigger an IAM lookup or its error banner —
// resolveOwners fires only where DashboardUserIDs finds dashboard user ids,
// even when the record carries a UUID-shaped owner field.
func TestInspectorSkipsOwnerResolutionForNonDashboard(t *testing.T) {
	uuid := "11111111-2222-3333-4444-555555555555"
	ds := &dataSource{sources: map[string]Source{}}
	for _, rec := range []map[string]any{
		{"type": "host", "owner": "monitoring-team"},
		{"type": "host", "owner": uuid}, // UUID owner, but not a dashboard
		{"owner": uuid},                 // no type at all
	} {
		v := newInspectorView(ds, "host", rec)
		if cmd := v.resolveOwners(); cmd != nil {
			t.Fatalf("resolveOwners must be a no-op for non-dashboard record %+v", rec)
		}
	}
}

// A transient IAM failure must not latch resolution off for the view: the
// request flags are cleared on error so a refresh re-issues the lookup.
func TestInspectorRetriesOwnerAfterError(t *testing.T) {
	uuid := "11111111-2222-3333-4444-555555555555"
	rec := map[string]any{"type": "dashboard", "owner": uuid}
	calls := 0
	ds := &dataSource{sources: map[string]Source{
		"resolve-users": func(_ context.Context, _ catalog.Scope, _ string) ([]map[string]any, error) {
			calls++
			if calls == 1 {
				return nil, errors.New("HTTP 403")
			}
			return []map[string]any{{"id": uuid, "name": "Ada"}}, nil
		},
	}}
	v := newInspectorView(ds, "d", rec)

	v.Update(v.resolveOwners()()) // first attempt fails
	if v.userNames[uuid] != "" {
		t.Fatalf("owner must stay unresolved after the error: %+v", v.userNames)
	}
	// The latch was released, so a refresh re-issues rather than no-op'ing.
	retry := v.resolveOwners()
	if retry == nil {
		t.Fatal("expected a retry command after a failed resolution")
	}
	v.Update(retry())
	if v.userNames[uuid] != "Ada" {
		t.Fatalf("retry did not resolve the owner: %+v", v.userNames)
	}
}
