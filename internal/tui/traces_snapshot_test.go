package tui

import "testing"

// spanRecord mirrors a `fetch spans` record from a OneAgent-instrumented Java
// service: OneAgent stamps dt.service.name (no OTel service.name), ns
// durations arrive as strings, and only the entry span carries the request
// verdict and http.* fields.
func spanRecord(traceID, spanID, parent, name, kind, svc, start, end, dur string, extra map[string]any) map[string]any {
	rec := map[string]any{
		"trace.id": traceID, "span.id": spanID, "span.name": name, "span.kind": kind,
		"dt.service.name":                svc,
		"dt.process_group.detected_name": "SpringBoot " + svc,
		"dt.smartscape.service":          "SERVICE-0000000000000A01",
		"dt.smartscape.process":          "PROCESS-0000000000000D01",
		"dt.smartscape.host":             "HOST-0000000000000B01",
		"start_time":                     start,
		"end_time":                       end,
		"duration":                       dur,
		"thread.name":                    "http-nio-8080-exec-4",
	}
	if svc == "OrderController" {
		rec["code.namespace"] = "com.example.shop.web.OrderController"
	}
	if parent != "" {
		rec["span.parent_id"] = parent
	}
	for k, v := range extra {
		rec[k] = v
	}
	return rec
}

func entrySpan(traceID, spanID, method, route, svc, start, end, dur, status string, failed bool) map[string]any {
	return spanRecord(traceID, spanID, "", method+" "+route, "server", svc, start, end, dur, map[string]any{
		"request.is_root_span":      true,
		"request.is_failed":         failed,
		"http.request.method":       method,
		"http.route":                route,
		"url.path":                  route,
		"http.response.status_code": status,
		"endpoint.name":             route,
	})
}

func TestSnapshotTracesTable(t *testing.T) {
	pinSnapshotClock(t)

	rows := []map[string]any{
		entrySpan("0000000000000000000000000000e001", "00000000000e0001", "POST", "/api/orders", "OrderController",
			"2026-07-15T11:59:58.120000000Z", "2026-07-15T11:59:58.461000000Z", "341000000", "201", false),
		entrySpan("0000000000000000000000000000e002", "00000000000e0002", "GET", "/api/orders/{orderId}", "OrderController",
			"2026-07-15T11:59:51.003000000Z", "2026-07-15T11:59:51.011000000Z", "8120000", "200", false),
		entrySpan("0000000000000000000000000000e003", "00000000000e0003", "DELETE", "/api/cart/{itemId}", "storefront-web",
			"2026-07-15T11:59:44.870000000Z", "2026-07-15T11:59:44.871000000Z", "871000", "200", false),
		entrySpan("0000000000000000000000000000e004", "00000000000e0004", "POST", "/api/checkout", "storefront-web",
			"2026-07-15T11:59:39.500000000Z", "2026-07-15T11:59:41.950000000Z", "2450000000", "500", true),
		entrySpan("0000000000000000000000000000e005", "00000000000e0005", "GET", "/actuator/health", "OrderController",
			"2026-07-15T11:59:30.000000000Z", "2026-07-15T11:59:30.002000000Z", "2140000", "200", false),
	}
	a := testApp(t, "traces")
	seedRows(t, a, rows)
	assertSnapshots(t, "traces", a)
}
