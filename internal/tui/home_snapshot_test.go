package tui

import "testing"

// TestSnapshotHome pins the launch screen: the six-panel grid with every
// panel populated. Records mirror each panel's summarize aliases (problems by
// display_id, failing services by dt.smartscape.service, the K8s warning
// timeseries, frontend errors, the vulnerability rollup, attack findings);
// Grail returns long counts as strings, so the fixtures do too.
func TestSnapshotHome(t *testing.T) {
	pinSnapshotClock(t)
	a := testApp(t, "home")
	seedHomeSnapshot(a.top().(*homeView))
	assertSnapshots(t, "home", a)
}

// TestSnapshotHomeConfigured pins the configured layouts a .dtui.yaml home
// block can produce: two uneven columns with pinned and global windows, one
// full-width column, and a rejected layout under its banner.
func TestSnapshotHomeConfigured(t *testing.T) {
	pinSnapshotClock(t)
	for name, c := range map[string]struct {
		panels []HomePanelConfig
		notice string
	}{
		"columns": {panels: []HomePanelConfig{
			{ID: "vulnerabilities"},
			{ID: "problems", Right: true, Timeframe: "7d"},
			{ID: "failing-services", Timeframe: "global"},
			{ID: "attacks", Right: true, Timeframe: "30d"},
			{ID: "k8s-warnings"},
		}},
		"single-column": {panels: []HomePanelConfig{
			{ID: "problems", Timeframe: "global"},
			{ID: "frontend-errors", Timeframe: "7d"},
		}},
		"rejected": {notice: `home.panels[1]: unknown panel "vulns" (line 5)`},
	} {
		t.Run(name, func(t *testing.T) {
			a, h := homeTestApp(t, c.panels, c.notice)
			seedHomeSnapshot(h)
			assertSnapshots(t, "home-"+name, a)
		})
	}
}

// seedHomeSnapshot fills every panel the view shows with its fixture rows.
func seedHomeSnapshot(h *homeView) {
	for i, p := range h.panels {
		h.Update(dataMsg{owner: panelOwner{v: h, idx: i}, seq: h.seq, records: homeSnapshotRecords[p.def.ID]})
	}
}

// homeSnapshotRecords are the fixture rows per curated panel id.
var homeSnapshotRecords = map[string][]map[string]any{
	"problems": {
		{"display_id": "P-260714", "status": "ACTIVE", "name": "CPU saturation",
			"start": "2026-07-15T11:41:00.000000000Z", "affected": []any{"ip-10-0-2-41"}},
		{"display_id": "P-260709", "status": "ACTIVE", "name": "Failure rate increase",
			"start": "2026-07-15T10:05:00.000000000Z", "affected": []any{"OrderController", "storefront-web"}},
	},
	"failing-services": {
		{"dt.smartscape.service": "SERVICE-0000000000000A01", "failed": "128", "svc": "OrderController", "ns": "shop"},
		{"dt.smartscape.service": "SERVICE-0000000000000A02", "failed": "7", "svc": "storefront-web"},
	},
	"k8s-warnings": {
		{"k8s.pod.name": "checkout-7d9f8b6c5d-x2k4q", "k8s.event.reason": "BackOff",
			"events": []any{0.0, 2.0, 4.0, 3.0, 6.0}, "interval": "3600000000000"},
		{"k8s.pod.name": "cart-5c6d7e8f9a-p9r8s", "k8s.event.reason": "FailedScheduling",
			"events": []any{1.0, 0.0, 0.0, 1.0, 0.0}, "interval": "3600000000000"},
	},
	"frontend-errors": {
		{"error.display_name": "TypeError: Cannot read properties of undefined (reading 'id')", "count": "42", "app": "storefront"},
		{"error.display_name": "NetworkError when attempting to fetch resource.", "count": "9", "app": "storefront"},
	},
	"vulnerabilities": {
		{"vulnerability.id": "1000000000000000001", "display_id": "S-101", "level": "CRITICAL", "score": 9.8,
			"title": "Remote code execution in log4j-core", "status": "OPEN", "muted": "NOT_MUTED"},
		{"vulnerability.id": "1000000000000000002", "display_id": "S-102", "level": "HIGH", "score": 7.5,
			"title": "Denial of service in netty-codec-http2", "status": "OPEN", "muted": "NOT_MUTED"},
	},
	"attacks": {
		{"timestamp": "2026-07-15T11:52:10.000000000Z", "finding.id": "F-0001", "finding.type": "SQL_INJECTION",
			"finding.action": "Blocked", "k8s.workload.name": "checkout",
			"dt.smartscape_source.id": "K8S_DEPLOYMENT-0000000000000C01", "dt.smartscape_source.type": "K8S_DEPLOYMENT"},
		{"timestamp": "2026-07-15T11:20:44.000000000Z", "finding.id": "F-0002", "finding.type": "CMD_INJECTION",
			"finding.action": "Monitored", "k8s.workload.name": "cart"},
	},
}
