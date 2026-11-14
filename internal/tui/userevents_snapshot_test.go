package tui

import "testing"

// User events mirror user.events records: characteristics.classifier as the
// discriminator (no event.type), ns-string durations, string status codes,
// view.name/page.name context, and the scalar dt.smartscape.frontend.
func TestSnapshotUserEventsTable(t *testing.T) {
	pinSnapshotClock(t)

	ev := func(classifier, start, dur string, extra map[string]any) map[string]any {
		rec := map[string]any{
			"characteristics.classifier": classifier, "start_time": start, "duration": dur,
			"dt.rum.session.id": "7340215598812744001", "frontend.name": "storefront", "frontend.type": "web",
			"dt.smartscape.frontend": "FRONTEND-0000000000000F54",
			"view.name":              "/checkout", "view.detected_name": "/checkout", "page.name": "/checkout",
			"browser.name": "Headless Chrome", "geo.country.iso_code": "AT",
		}
		for k, v := range extra {
			rec[k] = v
		}
		return rec
	}
	rows := []map[string]any{
		ev("request", "2026-07-15T11:59:38.740999936Z", "117900000", map[string]any{
			"http.request.method": "POST", "url.path": "/api/checkout/submit", "http.response.status_code": "200",
			"request.type": "fetch_xhr", "url.full": "https://shop.example.invalid/api/checkout/submit",
		}),
		ev("request", "2026-07-15T11:59:38.421999872Z", "133100000", map[string]any{
			"http.request.method": "POST", "url.path": "/api/checkout/quote", "http.response.status_code": "200",
			"request.type": "fetch_xhr",
		}),
		ev("error", "2026-07-15T11:59:37.902000000Z", "0", map[string]any{
			"error.display_name": "TypeError: Cannot read properties of undefined (reading 'items')",
		}),
		ev("user_action", "2026-07-15T11:59:37.101000000Z", "1809000000", map[string]any{
			"user_action.name": "click on \"Place order\"", "user_action.type": "click",
		}),
		ev("view_summary", "2026-07-15T11:59:30.976000000Z", "2840000000", map[string]any{
			"view.name": "/cart", "view.detected_name": "/cart", "page.name": "/cart",
		}),
	}

	a := testApp(t, "userevents")
	seedRows(t, a, rows)
	assertSnapshots(t, "userevents", a)
}
