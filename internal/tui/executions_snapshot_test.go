package tui

import "testing"

// Executions mirror dt.synthetic.events records: monitor.name, event.type
// (browser_monitor_execution / browser_monitor_step_execution /
// http_monitor_execution), result.state + status message, step.name, and the
// ns-string result.statistics.duration.
func TestSnapshotExecutionsTable(t *testing.T) {
	pinSnapshotClock(t)

	ex := func(ts, monitor, typ, state, msg, step, dur string) map[string]any {
		rec := map[string]any{
			"timestamp": ts, "monitor.name": monitor, "event.type": typ, "event.kind": "SYNTHETIC_EVENT",
			"result.state": state, "result.status.message": msg, "result.status.category": state,
			"result.statistics.duration": dur, "dt.synthetic.monitor.id": "SYNTHETIC_TEST-0000000000000AA2",
			"result.location.type": "public", "execution.type": "standard",
		}
		if step != "" {
			rec["step.name"], rec["step.type"] = step, "javascript"
		}
		return rec
	}
	rows := []map[string]any{
		ex("2026-07-15T11:50:01.563000000Z", "checkout journey (Frankfurt)", "browser_monitor_step_execution", "SUCCESS", "Success", "Click on \"Checkout\"", "0"),
		ex("2026-07-15T11:50:01.102000000Z", "checkout journey (Frankfurt)", "browser_monitor_step_execution", "SUCCESS", "Success", "Navigate to /cart", "1410600000"),
		ex("2026-07-15T11:49:58.004000000Z", "checkout journey (Frankfurt)", "browser_monitor_execution", "SUCCESS", "Success", "", "9369000000"),
		ex("2026-07-15T11:48:18.051000000Z", "login endpoint", "http_monitor_execution", "FAIL", "HTTP 503 Service Unavailable", "", "1204000000"),
		ex("2026-07-15T11:47:17.956000000Z", "api health /v1/status", "http_monitor_execution", "SUCCESS", "Success", "", "87000000"),
	}

	a := testApp(t, "executions")
	seedRows(t, a, rows)
	assertSnapshots(t, "executions", a)
}
