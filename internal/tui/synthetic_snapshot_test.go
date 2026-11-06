package tui

import "testing"

// Synthetic monitors mirror the classic dt.entity.synthetic_test /
// dt.entity.http_check union: entity.name, id, monitor.type, tags (null or
// a string array), lifetime, and the availability enrichment (25 buckets of
// percentages, the newest still null).
func TestSnapshotSyntheticTable(t *testing.T) {
	pinSnapshotClock(t)

	avail := func(dip ...float64) []any {
		s := make([]any, 25)
		for i := range 24 {
			s[i] = 100.0
		}
		for i, d := range dip {
			s[10+i] = d
		}
		return s
	}
	mon := func(id, name, typ, start string, tags any, av []any) map[string]any {
		return map[string]any{
			"id": id, "entity.name": name, "monitor.type": typ, "tags": tags,
			"lifetime":       map[string]any{"start": start, "end": "2026-07-15T11:46:18.080000000Z"},
			"__enrich.avail": av,
		}
	}
	rows := []map[string]any{
		mon("HTTP_CHECK-0000000000000AA1", "api health /v1/status", "http", "2026-06-14T07:17:30.785000000Z", nil, avail()),
		mon("SYNTHETIC_TEST-0000000000000AA2", "checkout journey (Frankfurt)", "browser", "2025-02-05T01:29:43.723000000Z",
			[]any{"team:checkout"}, avail(100, 50, 0, 50, 100)),
		mon("HTTP_CHECK-0000000000000AA3", "login endpoint", "http", "2024-08-01T10:18:33.305000000Z", []any{"env:prod"}, avail()),
		mon("SYNTHETIC_TEST-0000000000000AA4", "search and book a trip", "browser", "2022-10-19T17:35:30.192000000Z",
			[]any{"env:prod", "team:travel"}, avail()),
	}

	a := testApp(t, "synthetic")
	seedRows(t, a, rows)
	assertSnapshots(t, "synthetic", a)
}
