package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The command bar with no matching view: cycling and enter must report the
// typo instead of crashing on an empty match list.
func TestCommandBarNoMatchReportsUnknownView(t *testing.T) {
	a := testApp(t, "hosts")
	press(a, key(":"))
	typeText(a, "zq")
	if out := ansi.Strip(a.overlay.View(120, 40)); !strings.Contains(out, "no matching view") {
		t.Fatalf("palette should say nothing matches:\n%s", out)
	}
	press(a, key("tab"))
	press(a, key("shift+tab"))
	press(a, key("enter"))
	if !a.statusErr || !strings.Contains(a.status, `unknown view "zq"`) {
		t.Fatalf("status = %q (err=%v), want unknown view", a.status, a.statusErr)
	}
	if tv := a.top().(*tableView); tv.spec.Name != "hosts" || len(a.stack) != 1 {
		t.Fatalf("a typo must leave the stack alone, top = %s depth %d", tv.spec.Name, len(a.stack))
	}
}

// '.' on a different entity moves the pin; '.' on the pinned one unpins.
func TestPinMovesToNewSelectionAndTogglesOff(t *testing.T) {
	a := testApp(t, "hosts")
	other := hostRow()
	other["id"], other["name"] = "HOST-1111222233334444", "web-02.example.invalid"
	seedRows(t, a, []map[string]any{hostRow(), other})

	press(a, key("."))
	first := a.pin
	if first == nil {
		t.Fatal("'.' did not pin the selection")
	}
	press(a, key("j"))
	press(a, key("."))
	if a.pin == nil || a.pin.ID == first.ID {
		t.Fatalf("'.' on another entity must move the pin, pin = %+v", a.pin)
	}
	press(a, key("."))
	if a.pin != nil {
		t.Fatalf("'.' on the pinned entity must unpin, pin = %+v", a.pin)
	}
}

// A detail page's digits address its tab bar: 1 is the first tab (not the
// global bookmark), and a digit past the last tab teaches instead of jumping.
func TestDetailDigitsAddressTabs(t *testing.T) {
	a := testApp(t, "hosts")
	seedRows(t, a, []map[string]any{hostRow()})
	press(a, key("enter"))
	dv := a.top().(*detailView)
	if len(dv.tabs) < 2 || len(dv.tabs) >= 9 || dv.lensedInner() != nil {
		t.Fatalf("fixture assumption: host detail has 2..8 tabs, first unlensed (got %d)", len(dv.tabs))
	}

	press(a, key("2"))
	if dv.active != 1 {
		t.Fatalf("2 should open the second tab, active = %d", dv.active)
	}
	press(a, key("1"))
	if a.top() != dv || dv.active != 0 {
		t.Fatalf("1 should return to the first tab, top = %T active = %d", a.top(), dv.active)
	}
	press(a, key("9"))
	if a.top() != dv || dv.active != 0 || !strings.Contains(a.status, "digits pick tabs") {
		t.Fatalf("9 past the last tab should teach, top = %T active = %d status %q", a.top(), dv.active, a.status)
	}
}

// On a lensed tab a digit past the last lens keeps the lens and teaches.
func TestDetailDigitPastLastLensTeaches(t *testing.T) {
	a := testApp(t, "pods")
	seedRows(t, a, []map[string]any{podRow("a", "ns1", "Running", 0)})
	press(a, key("enter"))
	dv := a.top().(*detailView)
	press(a, key("v"))
	ev := dv.lensedInner()
	if ev == nil {
		t.Fatal("events tab should show a lens strip")
	}
	n := len(ev.spec.Lenses)
	if n >= 9 {
		t.Fatalf("fixture assumption: events has < 9 lenses, got %d", n)
	}
	press(a, key(string(rune('1'+n-1))))
	if ev.scope.Lens != n-1 {
		t.Fatalf("digit %d should pick the last lens, lens = %d", n, ev.scope.Lens)
	}
	press(a, key(string(rune('1'+n))))
	if ev.scope.Lens != n-1 || !strings.Contains(a.status, "digits pick lenses") {
		t.Fatalf("digit past the last lens should teach, lens = %d status %q", ev.scope.Lens, a.status)
	}
}

// NavigateMsg's trace pseudo-target takes the id from ID, else the first facet.
func TestNavigateTraceIDFallsBackToFacet(t *testing.T) {
	trace := strings.Repeat("cd", 16)
	for name, msg := range map[string]NavigateMsg{
		"id":    {Target: "trace", ID: trace, Facets: []NavFacet{{Field: "trace.id", Value: "ignored"}}},
		"facet": {Target: "trace", Facets: []NavFacet{{Field: "trace.id", Value: " " + trace + " "}}},
	} {
		t.Run(name, func(t *testing.T) {
			a := testApp(t, "home")
			_, cmd := a.Update(msg)
			deliver(a, cmd)
			wv, ok := a.top().(*waterfallView)
			if !ok || wv.traceID != trace {
				t.Fatalf("top = %T, want waterfall for %s", a.top(), trace)
			}
		})
	}

	a := testApp(t, "home")
	_, cmd := a.Update(NavigateMsg{Target: "trace"})
	deliver(a, cmd)
	if !a.statusErr || !strings.Contains(a.status, "needs a trace id") {
		t.Fatalf("trace without id should error, status = %q", a.status)
	}
}

// The semantic dictionary lands in the shared cache; a failed fetch leaves
// it empty rather than half-filled.
func TestDictionaryResultFillsSharedCache(t *testing.T) {
	a := testApp(t, "hosts")
	a.Update(dataMsg{owner: dictOwner{}, records: []map[string]any{
		{"name": "acme.order.id", "description": "Order id", "unit": "1"},
	}})
	if doc, ok := a.ds.fieldDoc("acme.order.id"); !ok || doc.Description != "Order id" {
		t.Fatalf("dictionary not cached: %+v %v", doc, ok)
	}
}

// The detail header's pulse counts distinct active problems with the right
// plural.
func TestDetailPulsePluralizes(t *testing.T) {
	for _, tc := range []struct {
		ids  []string
		want string
	}{
		{[]string{"P-1"}, "⚠ 1 active problem "},
		{[]string{"P-1", "P-2"}, "⚠ 2 active problems"},
	} {
		a := testApp(t, "hosts")
		seedRows(t, a, []map[string]any{hostRow()})
		press(a, key("enter"))
		dv := a.top().(*detailView)
		var recs []map[string]any
		for _, id := range tc.ids {
			r := richProblemRow()
			r["display_id"] = id
			recs = append(recs, r)
		}
		a.Update(dataMsg{owner: pulseOwner{v: dv}, seq: dv.pulseSeq, records: recs})
		if head := ansi.Strip(dv.View(140, 40)); !strings.Contains(head, tc.want) {
			t.Errorf("pulse for %v should read %q:\n%s", tc.ids, tc.want, head)
		}
	}
}
