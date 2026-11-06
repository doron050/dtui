package tui

import "testing"

// sessionRecord mirrors a user.sessions record: frontend.name and
// dt.smartscape.frontend as ARRAYS, ns-string duration, string counts, and
// end_reason. Synthetic browser-monitor sessions (ChromeForTesting, Linux,
// "synthetic_execution_finished") dominate busy tenants next to real users.
func sessionRecord(id, start, app, dur, views, actions, reqs, errs, browser, version, os, geo, end string) map[string]any {
	rec := map[string]any{
		"dt.rum.session.id":          id,
		"event.id":                   id,
		"start_time":                 start,
		"frontend.name":              []any{app},
		"dt.smartscape.frontend":     []any{"FRONTEND-0000000000000F51"},
		"duration":                   dur,
		"view_summary_count":         views,
		"user_action_count":          actions,
		"request_count":              reqs,
		"error.count":                errs,
		"event_count":                "192",
		"browser.name":               browser,
		"browser.version":            version,
		"os.name":                    os,
		"device.type":                "desktop",
		"end_reason":                 end,
		"characteristics.has_replay": false,
	}
	if geo != "" {
		rec["geo.country.iso_code"] = geo
	}
	return rec
}

func sessionRows() []map[string]any {
	return []map[string]any{
		sessionRecord("7340215598812744003", "2026-07-15T11:49:07.002000000Z", "easytravel-web", "5247000000", "0", "1", "139", "29",
			"ChromeForTesting", "157.0.8103.38", "Linux", "", "synthetic_execution_finished"),
		sessionRecord("7340215598812744004", "2026-07-15T11:49:04.356000000Z", "easytravel-web", "2172000000", "1", "1", "42", "0",
			"ChromeForTesting", "157.0.8103.38", "Linux", "", "synthetic_execution_finished"),
		sessionRecord("7340215598812744001", "2026-07-15T11:31:44.120000000Z", "storefront", "742900000000", "12", "31", "388", "2",
			"Chrome", "141.0.7390.122", "Windows", "AT", "timeout"),
		sessionRecord("7340215598812744002", "2026-07-15T11:12:09.501000000Z", "storefront", "38100000000", "1", "2", "17", "0",
			"Safari", "18.6", "iOS", "US", "visibility_change"),
	}
}

func TestSnapshotSessionsTable(t *testing.T) {
	pinSnapshotClock(t)
	a := testApp(t, "sessions")
	seedRows(t, a, sessionRows())
	assertSnapshots(t, "sessions", a)
}

// TestSnapshotSessionTimeline pins the RUM session waterfall: the live event
// order of a hard page load — page_summary, its navigation (doc request with
// a trace), view_summary, the hard_navigation user action — followed by XHR
// requests inside the action and a failed one.
func TestSnapshotSessionTimeline(t *testing.T) {
	pinSnapshotClock(t)
	a := testApp(t, "sessions")
	seedRows(t, a, sessionRows()[2:3])
	press(a, key("enter"))
	tl, ok := a.top().(*timelineView)
	if !ok {
		t.Fatalf("top = %T, want *timelineView", a.top())
	}
	ev := func(classifier, start, dur string, extra map[string]any) map[string]any {
		rec := map[string]any{
			"characteristics.classifier": classifier, "start_time": start, "duration": dur,
			"dt.rum.session.id": "7340215598812744001", "frontend.name": "storefront",
			"view.name": "/checkout", "view.detected_name": "/checkout", "page.name": "/checkout",
		}
		for k, v := range extra {
			rec[k] = v
		}
		return rec
	}
	tl.Update(dataMsg{owner: tl, seq: tl.seq, records: []map[string]any{
		ev("page_summary", "2026-07-15T11:31:45.976000000Z", "2841000000", map[string]any{"navigation.type": "hard"}),
		ev("navigation", "2026-07-15T11:31:45.976000000Z", "1410600000", map[string]any{
			"url.path": "/checkout", "http.response.status_code": "200", "request.type": "doc",
			"trace.id": "0000000000000000000000000000f001",
		}),
		ev("view_summary", "2026-07-15T11:31:45.976000000Z", "2840000000", nil),
		ev("user_action", "2026-07-15T11:31:45.976000000Z", "1809000000", map[string]any{
			"user_action.name": "Loading of page /checkout", "user_action.type": "hard_navigation",
		}),
		ev("request", "2026-07-15T11:31:46.227000000Z", "127000000", map[string]any{
			"http.request.method": "GET", "url.path": "/api/cart", "http.response.status_code": "200",
			"request.type": "fetch_xhr", "trace.id": "0000000000000000000000000000f002",
		}),
		ev("request", "2026-07-15T11:31:46.422000000Z", "133100000", map[string]any{
			"http.request.method": "POST", "url.path": "/api/checkout/quote", "http.response.status_code": "200",
			"request.type": "fetch_xhr",
		}),
		ev("request", "2026-07-15T11:31:46.741000000Z", "117900000", map[string]any{
			"http.request.method": "POST", "url.path": "/api/checkout/submit", "http.response.status_code": "503",
			"request.type": "fetch_xhr", "trace.id": "0000000000000000000000000000f003",
		}),
		ev("error", "2026-07-15T11:31:46.861000000Z", "0", map[string]any{
			"error.display_name": "Error: checkout submit failed (503)",
		}),
	}})
	assertSnapshots(t, "session-timeline", a)
}
