package tui

import "testing"

// Frontends mirror the smartscapeNodes FRONTEND record: frontend.type
// (web/mobile/custom), both ID eras (id_classic is APPLICATION- /
// MOBILE_APPLICATION- / CUSTOM_APPLICATION-), RUM instrumentation flags, and
// lifetime. The request/error enrichment is sparse like the live series —
// mostly null buckets with bursts — and absent for frontends with no traffic.
func TestSnapshotFrontendsTable(t *testing.T) {
	pinSnapshotClock(t)

	fe := func(id, classic, name, typ, start string, req, errs []any) map[string]any {
		rec := map[string]any{
			"id": id, "id_classic": classic, "name": name, "frontend.name": name, "type": "FRONTEND",
			"frontend.type": typ, "dt.rum.instrumentation.rum.enabled": true, "dt.security_context": []any{},
			"lifetime": map[string]any{"start": start, "end": "2026-07-15T11:38:00.000000000Z"},
		}
		if req != nil {
			rec["__enrich.req"], rec["__enrich.err"] = req, errs
		}
		return rec
	}
	rows := []map[string]any{
		fe("FRONTEND-0000000000000F51", "APPLICATION-0000000000000F51", "easytravel-web", "web", "2026-04-29T20:56:00.000000000Z",
			[]any{14.0, 139.0, 18.0, 1.0, nil, nil, nil, 1.0, nil, nil, nil, 75.0, 56.0, 84.0, 123.0, nil, nil, 30.0, 72.0, 75.0, 209.0, 6.0, nil, nil, nil},
			[]any{nil, 4.0, nil, nil, nil, nil, nil, nil, nil, nil, nil, 2.0, nil, 2.0, 2.0, nil, nil, nil, 3.0, 2.0, 4.0, nil, nil, nil, nil}),
		fe("FRONTEND-0000000000000F52", "MOBILE_APPLICATION-0000000000000F52", "easytravel-android", "mobile", "2026-04-29T20:56:00.000000000Z",
			[]any{nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 74.0, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil},
			[]any{nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 13.0, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil}),
		fe("FRONTEND-0000000000000F53", "CUSTOM_APPLICATION-0000000000000F53", "kiosk-terminal", "custom", "2026-07-15T11:32:00.000000000Z", nil, nil),
		fe("FRONTEND-0000000000000F54", "APPLICATION-0000000000000F54", "storefront", "web", "2026-05-07T11:31:00.000000000Z", nil, nil),
	}

	a := testApp(t, "frontends")
	seedRows(t, a, rows)
	assertSnapshots(t, "frontends", a)
}
