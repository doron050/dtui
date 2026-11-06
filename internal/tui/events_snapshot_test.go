package tui

import "testing"

// eventRecord mirrors a `fetch events` Davis-event record from a OneAgent +
// Kubernetes tenant: DAVIS_EVENT kind, CLOSED status, classic dt.entity.*
// ids with their .name siblings, and the dt.smartscape_source.* pair. Like
// the live records, it carries neither event.severity nor
// dt_source_entity_name, so the SEV and SOURCE columns render blank.
func eventRecord(ts, typ, category, name, provider string, extra map[string]any) map[string]any {
	rec := map[string]any{
		"timestamp":                        ts,
		"event.start":                      ts,
		"event.end":                        ts,
		"event.id":                         "-4711000000000000100_" + ts[11:19],
		"event.kind":                       "DAVIS_EVENT",
		"event.type":                       typ,
		"event.category":                   category,
		"event.name":                       name,
		"event.provider":                   provider,
		"event.status":                     "CLOSED",
		"event.status_transition":          "CREATED",
		"dt.davis.impact_level":            "Infrastructure",
		"dt.davis.is_frequent_event":       false,
		"dt.davis.mute.status":             "NOT_MUTED",
		"maintenance.is_under_maintenance": false,
		"dt.entity.host":                   "HOST-0000000000000B11",
		"dt.entity.host.name":              "gke-demo-pool-1-a1b2c3d4-x9k2",
		"host.name":                        "gke-demo-pool-1-a1b2c3d4-x9k2",
		"dt.smartscape.host":               "HOST-0000000000000B11",
		"dt.smartscape_source.id":          "PROCESS-0000000000000C31",
		"dt.smartscape_source.type":        "PROCESS",
	}
	for k, v := range extra {
		rec[k] = v
	}
	return rec
}

func TestSnapshotEventsTable(t *testing.T) {
	pinSnapshotClock(t)

	restart := func(ts, proc string) map[string]any {
		return eventRecord(ts, "WARNING", "WARNING", "Process restart", "ONEAGENT", map[string]any{
			"event.evidence_type":                   "PROCESS_RESTART",
			"event.group_label":                     "Process restart",
			"process.name":                          proc,
			"dt.entity.process_group_instance.name": proc,
		})
	}
	rows := []map[string]any{
		restart("2026-07-15T11:59:12.911000000Z", "chromium-headless"),
		restart("2026-07-15T11:58:44.303000000Z", "node_exporter"),
		eventRecord("2026-07-15T11:57:02.118000000Z", "CUSTOM_INFO", "INFO", "Deployment spec change", "KUBERNETES_INFERRED_EVENT", map[string]any{
			"event.group_label":         "Deployment spec change",
			"k8s.cluster.name":          "demo-eks-useast1",
			"k8s.namespace.name":        "checkout",
			"k8s.workload.name":         "cart-api",
			"dt.smartscape_source.id":   "K8S_DEPLOYMENT-0000000000000C41",
			"dt.smartscape_source.type": "K8S_DEPLOYMENT",
		}),
		eventRecord("2026-07-15T11:54:30.000000000Z", "AVAILABILITY_EVENT", "AVAILABILITY", "HTTP monitor location unavailable", "SYNTHETIC", map[string]any{
			"dt.entity.http_check":      "HTTP_CHECK-0000000000000D51",
			"dt.entity.http_check.name": "storefront health",
			"dt.smartscape_source.id":   "HTTP_MONITOR-0000000000000D52",
			"dt.smartscape_source.type": "HTTP_MONITOR",
			"dt.entity.host":            nil,
			"dt.entity.host.name":       nil,
			"host.name":                 nil,
		}),
		restart("2026-07-15T11:51:10.201000000Z", "OneAgent log module"),
	}

	a := testApp(t, "events")
	seedRows(t, a, rows)
	assertSnapshots(t, "events", a)
}
