package tui

import "testing"

// Detectors are API-backed (Spec.API "anomaly-detectors"): rows arrive in the
// shape detectorSource adapts from the anomaly-detection settings objects —
// title, enabled as a string, the short analyzer name, the event template's
// event type, source, and description.
func TestSnapshotDetectorsTable(t *testing.T) {
	pinSnapshotClock(t)

	det := func(id, title, enabled, analyzer, eventType, source, desc string) map[string]any {
		return map[string]any{
			"objectId": id, "title": title, "enabled": enabled, "analyzer": analyzer,
			"eventType": eventType, "source": source, "description": desc,
		}
	}
	rows := []map[string]any{
		det("vu9U3hXa3q0AAAABAC01", "Ingest volume above forecast", "true", "StaticThreshold", "CUSTOM_ALERT", "Custom",
			"Fires when hourly log ingest exceeds the forecast by more than 5 samples out of 5."),
		det("vu9U3hXa3q0AAAABAC02", "Kafka consumer lag", "true", "AutoAdaptiveThreshold", "CUSTOM_ALERT", "Clouds",
			"Consumer group lag for the orders topic, adaptive baseline."),
		det("vu9U3hXa3q0AAAABAC03", "Checkout p95 response time", "false", "SeasonalBaseline", "PERFORMANCE_EVENT", "Custom",
			"Disabled during the 2026 load-test window."),
	}

	a := testApp(t, "detectors")
	seedRows(t, a, rows)
	assertSnapshots(t, "detectors", a)
}
