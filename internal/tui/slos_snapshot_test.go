package tui

import "testing"

// SLOs are API-backed (Spec.API "slos"): rows arrive in the shape sloSource
// adapts from `dtctl get slos` — name, description, tags (live SLOs carry
// up to ten), target + window from criteria[0], modified — merged with the
// live evaluation's status, sli, and errorBudget. seedRows feeds the table
// exactly where the Source result lands.
func TestSnapshotSLOsTable(t *testing.T) {
	pinSnapshotClock(t)

	slo := func(id, name, status string, sli, target, budget float64, window string, tags []any, modified string) map[string]any {
		return map[string]any{
			"id": id, "name": name, "status": status, "sli": sli, "target": target, "errorBudget": budget,
			"window": window, "tags": tags, "modified": modified, "version": "vu9U3hXa3q0AAAABAB",
		}
	}
	rows := []map[string]any{
		slo("00000000-0000-4000-8000-000000000b01", "checkout availability", "SUCCESS", 99.97, 99.9, 70.0, "7d",
			[]any{"team:checkout", "tier:1", "env:prod", "slo-type:availability", "owner:sre", "region:eu", "service:checkout",
				"journey:purchase", "reviewed:2026-q2", "pagerduty:checkout"}, "2026-07-01T10:41:51Z"),
		slo("00000000-0000-4000-8000-000000000b02", "payments p95 latency < 400 ms", "FAILURE", 99.12, 99.95, -1660.0, "1d",
			[]any{"team:payments", "tier:1"}, "2026-07-14T09:26:53Z"),
		slo("00000000-0000-4000-8000-000000000b03", "search error rate", "WARNING", 99.53, 99.5, 6.0, "28d",
			[]any{"team:search"}, "2026-06-30T05:15:04Z"),
	}

	a := testApp(t, "slos")
	seedRows(t, a, rows)
	assertSnapshots(t, "slos", a)
}
