package tui

import "testing"

// logRecord mirrors a `fetch logs` record from a OneAgent-fed Kubernetes
// tenant: loglevel + status, the k8s.* / dt.process_group.* origin fields
// logSource picks from, log.source, and the dt.smartscape_source.* pair
// signalSourceEntity reads.
func logRecord(ts, level, content string, extra map[string]any) map[string]any {
	rec := map[string]any{
		"timestamp":                      ts,
		"loglevel":                       level,
		"status":                         level,
		"content":                        content,
		"event.type":                     "LOG",
		"host.name":                      "ip-10-0-1-17",
		"k8s.cluster.name":               "demo-cluster",
		"k8s.node.name":                  "ip-10-0-1-17",
		"k8s.namespace.name":             "orders",
		"k8s.pod.name":                   "orders-api-7c9d8f6b5-q4wzt",
		"k8s.container.name":             "orders-api",
		"k8s.deployment.name":            "orders-api",
		"container.image.name":           "registry.example.invalid/orders-api",
		"dt.process_group.detected_name": "SpringBoot orders-api com.example.orders.Application orders-api-*",
		"log.source":                     "Container Output",
		"log.iostream":                   "stdout",
		"process.technology":             []any{"JAVA", "APACHE_TOMCAT"},
		"dt.smartscape_source.id":        "CONTAINER-0000000000000D01",
		"dt.smartscape_source.type":      "CONTAINER",
		"dt.smartscape.host":             "HOST-0000000000000B01",
		"dt.smartscape.process":          "PROCESS-0000000000000C05",
	}
	for k, v := range extra {
		if v == nil {
			delete(rec, k)
			continue
		}
		rec[k] = v
	}
	return rec
}

func TestSnapshotLogsTable(t *testing.T) {
	pinSnapshotClock(t)

	// A host-level syslog line: no k8s/container origin, loglevel NONE.
	syslog := map[string]any{
		"log.source": "/var/log/syslog", "k8s.namespace.name": nil, "k8s.pod.name": nil,
		"k8s.container.name": nil, "k8s.deployment.name": nil, "container.image.name": nil,
		"log.iostream": nil, "process.technology": nil, "dt.process_group.detected_name": "Linux System",
		"dt.smartscape_source.id": "PROCESS-0000000000000C02", "dt.smartscape_source.type": "PROCESS",
		"dt.smartscape.process": "PROCESS-0000000000000C02",
	}
	rows := []map[string]any{
		logRecord("2026-07-15T11:59:56.364000000Z", "INFO",
			"2026-07-15 11:59:56.364  INFO 1 --- [nio-8080-exec-9] c.e.orders.web.OrderController           : GET /api/orders 200 (12 ms)", nil),
		logRecord("2026-07-15T11:59:52.819000000Z", "INFO",
			"2026-07-15 11:59:52.819  INFO 1 --- [-QueueProcessor] c.e.orders.telemetry.SpanReporter        : flush succeeded again", nil),
		logRecord("2026-07-15T11:59:51.819000000Z", "WARN",
			"2026-07-15 11:59:51.819  WARN 1 --- [-QueueProcessor] c.e.orders.telemetry.SpanReporter        : flush failed! Repeated errors of this command will not be logged", nil),
		logRecord("2026-07-15T11:59:46.822000000Z", "ERROR",
			"2026-07-15 11:59:46.822 ERROR 1 --- [nio-8080-exec-3] c.e.orders.db.OrderRepository            : connection to db:5432 refused", nil),
		logRecord("2026-07-15T11:59:01.000000000Z", "NONE",
			"Jul 15 11:59:01 ip-10-0-1-17 CRON[54766]: (ubuntu) CMD (/usr/local/bin/rotate-reports.sh >> /var/log/rotate.log 2>&1)", syslog),
		logRecord("2026-07-15T11:58:59.103000000Z", "INFO",
			"[INFO] 10.42.0.7:53001 - 4711 \"A IN orders-db.orders.svc.cluster.local. udp 52 false 512\" NOERROR qr,aa,rd 106 0.000123s",
			map[string]any{
				"k8s.namespace.name": "kube-system", "k8s.pod.name": "coredns-5d78c9869d-abcde",
				"k8s.container.name": "coredns", "k8s.deployment.name": "coredns",
				"dt.process_group.detected_name": "CoreDNS coredns-*",
				"dt.smartscape_source.id":        "CONTAINER-0000000000000D02",
			}),
	}

	a := testApp(t, "logs")
	seedRows(t, a, rows)
	assertSnapshots(t, "logs", a)
}
