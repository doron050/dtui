package tui

import "testing"

// Services mirror what a live tenant returns: Smartscape id/name + lifetime,
// plus the __enrich.req / __enrich.fail series behind the spark columns.
func TestSnapshotServicesTable(t *testing.T) {
	pinSnapshotClock(t)

	rows := []map[string]any{
		{
			"id": "SERVICE-0000000000000A01", "name": "OrderController", "type": "SERVICE",
			"lifetime":                     map[string]any{"start": "2026-06-21T15:43:00.000000000Z", "end": "2026-07-15T06:11:00.000000000Z"},
			"dt.service_detection.version": "1",
			"__enrich.req":                 []any{1.0, 1.0, 2.0, 1.0, 1.0, 1.0, 3.0, 12.0},
			"__enrich.fail":                []any{0.0, 0.0, 0.0, 0.0, 0.0, 1.0, 0.0, 1.0},
		},
		{
			"id": "SERVICE-0000000000000A02", "name": "storefront-web", "type": "SERVICE",
			"lifetime":                     map[string]any{"start": "2026-06-21T15:43:00.000000000Z", "end": "2026-07-15T06:11:00.000000000Z"},
			"dt.service_detection.version": "1",
			"__enrich.req":                 []any{2.0, 3.0, 2.0, 4.0, 3.0, 5.0, 4.0, 71.0},
			"__enrich.fail":                []any{0.0, 1.0, 0.0, 0.0, 2.0, 0.0, 5.0, 5.0},
		},
	}

	a := testApp(t, "services")
	seedRows(t, a, rows)
	assertSnapshots(t, "services", a)
}
