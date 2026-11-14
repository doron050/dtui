package tui

import "testing"

// TestSnapshotMetricsTable pins the metric explorer's catalog list. Rows
// mirror its `metrics | summarize` result: series counts as strings and the
// collectDistinct of source types, where null marks series with no source
// entity (billing, self-monitoring).
func TestSnapshotMetricsTable(t *testing.T) {
	pinSnapshotClock(t)

	rows := []map[string]any{
		{"metric.key": "dt.billing.full_stack_monitoring.usage", "series": "1", "types": []any{nil}},
		{"metric.key": "dt.host.cpu.usage", "series": "3", "types": []any{"HOST"}},
		{"metric.key": "dt.host.disk.used.percent", "series": "9", "types": []any{"DISK", "HOST"}},
		{"metric.key": "dt.host.memory.usage", "series": "3", "types": []any{"HOST"}},
		{"metric.key": "dt.process.cpu.usage", "series": "23", "types": []any{"PROCESS"}},
		{"metric.key": "dt.service.request.count", "series": "18", "types": []any{"SERVICE"}},
		{"metric.key": "dt.service.request.response_time", "series": "18", "types": []any{"SERVICE"}},
		{"metric.key": "dt.sfm.server.metrics.ingested", "series": "2", "types": []any{nil}},
		{"metric.key": "jvm.memory.used", "series": "12", "types": []any{"CONTAINER", "PROCESS"}},
	}
	a := testApp(t, "metrics")
	seedRows(t, a, rows)
	assertSnapshots(t, "metrics", a)
}
