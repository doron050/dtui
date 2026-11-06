package tui

import "testing"

// attackRecord mirrors a security.events DETECTION_FINDING record — the
// envelope (event.*, finding.*, dt.security.risk.*, actor.ips as an array)
// shaped from live detection findings — carrying the Runtime Application
// Protection fields the attacks columns read: finding.action, the attacked
// workload, and the vulnerable code location.
func attackRecord(ts, typ, action, severity, ip, workload, process, location string) map[string]any {
	return map[string]any{
		"timestamp":                        ts,
		"event.kind":                       "SECURITY_EVENT",
		"event.type":                       "DETECTION_FINDING",
		"event.provider":                   "Dynatrace",
		"event.id":                         "00000000-0000-4000-8000-0000000000" + ts[17:19],
		"product.name":                     "Runtime Application Protection",
		"product.vendor":                   "Dynatrace",
		"finding.id":                       "00000000-0000-4000-9000-0000000000" + ts[17:19],
		"finding.type":                     typ,
		"finding.action":                   action,
		"finding.severity":                 severity,
		"finding.score":                    9.1,
		"finding.time.created":             ts,
		"dt.security.risk.level":           severity,
		"dt.security.risk.score":           9.1,
		"actor.ips":                        []any{ip},
		"k8s.workload.name":                workload,
		"k8s.namespace.name":               "checkout",
		"dt.process_group.detected_name":   process,
		"vulnerability.code_location.name": location,
		"dt.smartscape_source.id":          "PROCESS-0000000000000C61",
		"dt.smartscape_source.type":        "PROCESS",
	}
}

func TestSnapshotAttacksTable(t *testing.T) {
	pinSnapshotClock(t)

	rows := []map[string]any{
		attackRecord("2026-07-15T11:41:07.889000000Z", "SQL_INJECTION", "Blocked", "CRITICAL", "198.51.100.75", "orders-api", "SpringBoot orders-api com.example.orders.Application",
			"com.example.orders.db.OrderRepository.findByCustomer(String):112"),
		attackRecord("2026-07-15T11:36:06.230000000Z", "SQL_INJECTION", "Blocked", "CRITICAL", "198.51.100.75", "orders-api", "SpringBoot orders-api com.example.orders.Application",
			"com.example.orders.db.OrderRepository.findByCustomer(String):112"),
		attackRecord("2026-07-15T11:21:03.258000000Z", "CMD_INJECTION", "Audited", "HIGH", "203.0.113.215", "report-worker", "SpringBoot report-worker com.example.reports.Application",
			"com.example.reports.export.PdfRenderer.render(Path):57"),
		attackRecord("2026-07-15T11:16:08.185000000Z", "SSRF", "Blocked", "MEDIUM", "203.0.113.215", "",
			"SpringBoot proxy-gateway com.example.proxy.Application",
			"com.example.proxy.ProxyController.proxyUrl(String):89"),
	}

	a := testApp(t, "attacks")
	seedRows(t, a, rows)
	assertSnapshots(t, "attacks", a)
}
