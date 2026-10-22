package catalog

import "regexp"

// Dashboards backed by the document API (type=="dashboard") rather than DQL.
// Records are flattened maps produced by the dashboards source wired in
// tui.Options.Sources; columns name the flattened keys. Dashboards are
// documents, not Smartscape nodes, so the view carries no drill scope — the
// 'o' open action turns the row's id into a Dashboards-app deep link (see
// internal/tui/links.go). owner is an opaque user id, not a display name.

// Dashboard lenses are server-side owner slices, not DQL filters (this is an
// API view, so Lens.Filter stays empty). The dashboards source keys off the
// active Scope.Lens index — DashLensMine sends the document API an owner==me
// filter, DashLensAll leaves it off and lists everything the token can access
// (owned + shared-with-me). These map 1:1 onto `dtctl get dashboards [--mine]`,
// which the Echo mirrors.
const (
	DashLensAll  = 0 // no owner filter — every accessible dashboard (default)
	DashLensMine = 1 // owner == current user
)

var dashboardsSpec = &Spec{
	Name:    "dashboards",
	Aliases: []string{"dashboard", "dash"},
	Kind:    KindEntity,
	Desc:    "Dashboards (document API) — 'o' opens the selected one",
	API:     "dashboards",
	Echo: func(s Scope, _ string) string {
		if s.Lens == DashLensMine {
			return "dtctl get dashboards --mine"
		}
		return "dtctl get dashboards"
	},
	Lenses: []Lens{
		{Name: "all", Desc: "every dashboard you can access (owned + shared)"},
		{Name: "mine", Desc: "dashboards you own"},
	},
	Columns: []Column{
		{Title: "NAME", Field: "name"},
		{Title: "MODIFIED", Width: 8, Right: true, Value: func(rec map[string]any) string {
			return Age(Str(rec, "modified"))
		}, Sort: func(rec map[string]any) any { return Str(rec, "modified") }},
		{Title: "SHARED", Width: 6, Field: "shared", Class: func(val string) string {
			if val == "true" {
				return "ok"
			}
			return "dim"
		}},
		{Title: "TAGS", Width: 30, Value: func(rec map[string]any) string { return FormatValue(rec["tags"]) }},
	},
	Drills: map[string]string{},
}

// dashboardUserFields are the record keys whose values are IAM user UUIDs.
// Only "owner" is populated by dashboardSource today; the slice is the single
// source of truth the detail view resolves and appends names to.
var dashboardUserFields = []string{"owner"}

// userUUIDRe matches an IAM user UUID (canonical 8-4-4-4-12 hex form). Document
// owners are user UUIDs, and IAM ListUsers filters by UUID, so a non-UUID owner
// (an email or team string) could never resolve — filtering it out here avoids
// a pointless IAM round-trip.
var userUUIDRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// DashboardUserIDs returns the IAM user UUIDs a dashboard record carries in its
// owner field, for the detail view to resolve into display names (the
// list/preview keep the raw owner id). It is scoped to dashboard records
// (type=="dashboard") so the inspector only issues the IAM lookup — and only
// surfaces its errors — where dashboards are in view, and only for values that
// are actually UUIDs.
func DashboardUserIDs(rec map[string]any) []string {
	if Str(rec, "type") != "dashboard" {
		return nil
	}
	var ids []string
	for _, k := range dashboardUserFields {
		if v := Str(rec, k); userUUIDRe.MatchString(v) {
			ids = append(ids, v)
		}
	}
	return ids
}

// IsDashboardUserField reports whether a record key holds an IAM user UUID that
// DashboardUserIDs resolves, so the detail view appends the resolved name only
// to those fields (never to an unrelated field that happens to share the UUID).
func IsDashboardUserField(key string) bool {
	for _, k := range dashboardUserFields {
		if k == key {
			return true
		}
	}
	return false
}
