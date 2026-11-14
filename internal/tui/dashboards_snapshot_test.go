package tui

import "testing"

// Dashboards are API-backed (Spec.API "dashboards"): rows arrive in the shape
// dashboardSource adapts from the document list — id, name, type, owner (a
// user UUID), version, shared as a string, tags, and the last-modified time.
func TestSnapshotDashboardsTable(t *testing.T) {
	pinSnapshotClock(t)

	dash := func(id, name, owner, shared, modified string, version float64, tags []any) map[string]any {
		return map[string]any{
			"id": id, "name": name, "type": "dashboard", "owner": owner, "shared": shared,
			"modified": modified, "version": version, "tags": tags,
		}
	}
	const owner = "00000000-0000-4000-8000-000000000c01"
	rows := []map[string]any{
		dash("00000000-0000-4000-8000-000000000d01", "Kubernetes cluster overview", owner, "true", "2026-07-15T09:04:29Z", 1481, []any{"k8s", "platform"}),
		dash("00000000-0000-4000-8000-000000000d02", "Checkout — golden signals", owner, "true", "2026-07-14T13:27:12Z", 1298, []any{"checkout"}),
		dash("00000000-0000-4000-8000-000000000d03", "OneAgent fleet health", "00000000-0000-4000-8000-000000000c02", "false", "2026-07-11T09:26:53Z", 162, nil),
		dash("00000000-0000-4000-8000-000000000d04", "Scratchpad", "00000000-0000-4000-8000-000000000c03", "false", "2026-06-25T08:06:31Z", 6, nil),
	}

	a := testApp(t, "dashboards")
	seedRows(t, a, rows)
	assertSnapshots(t, "dashboards", a)
}
