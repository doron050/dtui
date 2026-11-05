package tui

import "testing"

// TestSnapshotWaterfall pins the trace waterfall: tree guides, kind and
// service columns, the proportional bars on the trace's time axis, and a
// failed span. The spans keep the real parent/child shape: an entry span
// with the request verdict, a controller subroutine, and its outbound calls.
func TestSnapshotWaterfall(t *testing.T) {
	pinSnapshotClock(t)

	const tid = "0000000000000000000000000000e004"
	root := entrySpan(tid, "00000000000e0004", "POST", "/api/checkout", "storefront-web",
		"2026-07-15T11:59:39.500000000Z", "2026-07-15T11:59:41.950000000Z", "2450000000", "500", true)
	spans := []map[string]any{
		root,
		spanRecord(tid, "00000000000e0041", "00000000000e0004", "CheckoutController/checkout", "server", "storefront-web",
			"2026-07-15T11:59:39.502000000Z", "2026-07-15T11:59:41.940000000Z", "2438000000",
			map[string]any{"span.is_subroutine": true}),
		spanRecord(tid, "00000000000e0042", "00000000000e0041", "SELECT shop.cart", "client", "storefront-web",
			"2026-07-15T11:59:39.510000000Z", "2026-07-15T11:59:39.530000000Z", "20000000",
			map[string]any{"db.system": "postgresql", "db.query.text": "SELECT * FROM cart WHERE session_id = ?"}),
		spanRecord(tid, "00000000000e0043", "00000000000e0041", "POST /api/orders", "client", "storefront-web",
			"2026-07-15T11:59:39.540000000Z", "2026-07-15T11:59:39.890000000Z", "350000000",
			map[string]any{"http.request.method": "POST", "server.address": "orders"}),
		spanRecord(tid, "00000000000e0044", "00000000000e0043", "POST /api/orders", "server", "OrderController",
			"2026-07-15T11:59:39.548000000Z", "2026-07-15T11:59:39.882000000Z", "334000000",
			map[string]any{"request.is_root_span": false, "request.is_failed": false, "http.response.status_code": "201"}),
		spanRecord(tid, "00000000000e0045", "00000000000e0041", "POST /v1/charges", "client", "storefront-web",
			"2026-07-15T11:59:39.900000000Z", "2026-07-15T11:59:41.930000000Z", "2030000000",
			map[string]any{"http.request.method": "POST", "server.address": "payments.example.invalid",
				"http.response.status_code": "503", "span.status_code": "error"}),
	}

	a := testApp(t, "traces")
	seedRows(t, a, []map[string]any{root})
	press(a, key("enter"))
	wf := a.top().(*waterfallView)
	wf.Update(dataMsg{owner: wf, seq: wf.seq, records: spans})
	assertSnapshots(t, "waterfall", a)
}
