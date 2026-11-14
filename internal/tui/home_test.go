package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
)

func TestHomePanelsLoadIndependently(t *testing.T) {
	a := testApp(t, "home")
	hv, ok := a.top().(*homeView)
	if !ok {
		t.Fatalf("home initial view, top = %T", a.top())
	}
	if len(hv.panels) != 6 {
		t.Fatalf("panels = %d", len(hv.panels))
	}
	// Each panel got its own DQL.
	for _, p := range hv.panels {
		if p.dql == "" {
			t.Errorf("panel %q has no query", p.def.Title)
		}
	}
}

// TestHomeSelectionAndYank: home rows are first-class selections — y copies
// the row's identity and rows with a Smartscape id yield an entity for the
// global actions (pin, x/X, o).
func TestHomeSelectionAndYank(t *testing.T) {
	a := testApp(t, "home")
	h := a.top().(*homeView)
	h.Update(dataMsg{owner: panelOwner{v: h, idx: 0}, seq: h.seq,
		records: []map[string]any{{"display_id": "P-77", "name": "cpu saturation"}}})
	h.Update(dataMsg{owner: panelOwner{v: h, idx: 1}, seq: h.seq,
		records: []map[string]any{{"dt.smartscape.service": "SERVICE-0000000000000001", "svc": "checkout"}}})

	h.focus, h.cursor = 0, 0
	if text, _, ok := h.YankText(); !ok || text != "P-77" {
		t.Errorf("problems panel yank = %q, %v", text, ok)
	}
	if _, e := h.Selection(); e != nil {
		t.Error("summarized problem rows carry no entity")
	}

	h.focus, h.cursor = 1, 0
	rec, e := h.Selection()
	if rec == nil || e == nil || e.ID != "SERVICE-0000000000000001" || e.Type != "SERVICE" {
		t.Fatalf("services panel selection = %+v", e)
	}
	// The global x now works from home: it walks the selected service.
	press(a, key("x"))
	nv, ok := a.top().(*navView)
	if !ok || nv.mode != navWalk || nv.root.ID != "SERVICE-0000000000000001" {
		t.Fatalf("x on a home service row should open the walk, top = %T", a.top())
	}
}

// Finding #4: a stale in-flight home-panel result (older seq) must not
// overwrite a newer refresh.
func TestHomePanelDropsStaleResults(t *testing.T) {
	a := testApp(t, "home")
	hv := a.top().(*homeView)
	staleSeq := hv.seq

	// A new refresh bumps the generation.
	deliver(a, hv.Refresh())
	if hv.seq == staleSeq {
		t.Fatal("Refresh should bump seq")
	}

	// A late result tagged with the old seq is ignored.
	hv.Update(dataMsg{owner: panelOwner{v: hv, idx: 1}, seq: staleSeq,
		records: []map[string]any{{"svc": "ghost"}}})
	if len(hv.panels[1].records) != 0 {
		t.Errorf("stale result should be dropped, got %d records", len(hv.panels[1].records))
	}

	// A current-seq result is accepted.
	hv.Update(dataMsg{owner: panelOwner{v: hv, idx: 1}, seq: hv.seq,
		records: []map[string]any{{"svc": "real"}}})
	if len(hv.panels[1].records) != 1 {
		t.Errorf("current result should be accepted, got %d", len(hv.panels[1].records))
	}
}

// homeOf returns the home view of an app started on it.
func homeOf(t *testing.T, a *app) *homeView {
	t.Helper()
	h, ok := a.top().(*homeView)
	if !ok {
		t.Fatalf("top = %T, want *homeView", a.top())
	}
	return h
}

// feedPanel delivers records (or an error) to one panel at the refresh
// generation it last fired — fixed-window panels keep theirs across `t`.
func feedPanel(h *homeView, idx int, err error, recs ...map[string]any) {
	h.Update(dataMsg{owner: panelOwner{v: h, idx: idx}, seq: h.panels[idx].seq, records: recs, err: err})
}

// seedEveryPanel gives each panel one or two rows so the cursor can tour them.
func seedEveryPanel(h *homeView) {
	feedPanel(h, 0, nil,
		map[string]any{"display_id": "P-1", "name": "cpu saturation"},
		map[string]any{"display_id": "P-2", "name": "memory leak"})
	feedPanel(h, 1, nil, map[string]any{"dt.smartscape.service": "SERVICE-0000000000000001", "svc": "checkout", "failed": "12"})
	feedPanel(h, 2, nil, map[string]any{"k8s.pod.name": "cart-0", "k8s.event.reason": "BackOff", "events": []any{1.0, 2.0}})
	feedPanel(h, 3, nil, map[string]any{"error.display_name": "TypeError", "app": "shop", "count": "3"})
	feedPanel(h, 4, nil,
		map[string]any{"display_id": "S-1", "level": "CRITICAL", "score": 9.8, "title": "log4shell"},
		map[string]any{"display_id": "S-2", "level": "LOW", "score": 2.0, "title": "minor"})
	feedPanel(h, 5, nil, map[string]any{"finding.id": "F-1", "finding.type": "SQL_INJECTION", "finding.action": "Blocked", "k8s.workload.name": "orders"})
}

func TestHomeViewPanelStates(t *testing.T) {
	a := testApp(t, "home")
	h := homeOf(t, a)

	out := ansi.Strip(h.View(120, 40))
	if got := strings.Count(out, "loading…"); got != len(h.panels) {
		t.Errorf("every panel should show its spinner before data arrives, got %d:\n%s", got, out)
	}
	if !h.Busy() {
		t.Error("home must report busy while panels load")
	}

	feedPanel(h, 0, errors.New("query failed: boom"))
	feedPanel(h, 1, nil)
	feedPanel(h, 2, nil, map[string]any{"k8s.pod.name": "cart-0", "k8s.event.reason": "BackOff", "events": []any{2.0, 3.0}})

	out = ansi.Strip(h.View(120, 40))
	for _, want := range []string{
		"✗ query failed: boom",          // per-panel error, the others unaffected
		"✓ nothing to report",           // empty result is explicit, not blank
		"BackOff",                       // a loaded row
		"5×",                            // series summed into an occurrence count
		"kubernetes warnings · 24h · 1", // curated window and row count in the title
	} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "active problems · 24h ·") {
		t.Error("an errored panel must not advertise a row count")
	}
	if !h.Busy() {
		t.Error("panels 3-5 are still loading")
	}
	for i := 3; i < len(h.panels); i++ {
		feedPanel(h, i, nil)
	}
	if h.Busy() {
		t.Error("all panels loaded, home must not be busy")
	}
}

func TestHomeViewLayoutColumns(t *testing.T) {
	a := testApp(t, "home")
	h := homeOf(t, a)

	sameLine := func(out, x, y string) bool {
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, x) && strings.Contains(l, y) {
				return true
			}
		}
		return false
	}
	wide := ansi.Strip(h.View(120, 40))
	if !sameLine(wide, "active problems", "failing services") {
		t.Errorf("wide terminals pair panels side by side:\n%s", wide)
	}
	narrow := ansi.Strip(h.View(80, 40))
	if sameLine(narrow, "active problems", "failing services") {
		t.Errorf("narrow terminals stack panels in one column:\n%s", narrow)
	}
	for _, p := range h.panels {
		if !strings.Contains(narrow, p.def.Title) {
			t.Errorf("narrow layout lost panel %q", p.def.Title)
		}
	}
}

func TestHomeKeyNavigation(t *testing.T) {
	a := testApp(t, "home")
	h := homeOf(t, a)
	seedEveryPanel(h)
	last := len(h.panels) - 1

	press(a, key("tab"))
	if h.focus != 1 || h.cursor != 0 {
		t.Fatalf("tab → focus %d cursor %d", h.focus, h.cursor)
	}
	press(a, key("shift+tab"))
	press(a, key("shift+tab"))
	if h.focus != last {
		t.Fatalf("shift+tab must wrap to the last panel, focus = %d", h.focus)
	}
	press(a, key("tab"))
	if h.focus != 0 {
		t.Fatalf("tab must wrap to the first panel, focus = %d", h.focus)
	}

	// j walks the rows, then rolls into the next panel.
	press(a, key("j"))
	if h.focus != 0 || h.cursor != 1 {
		t.Fatalf("j within panel → focus %d cursor %d", h.focus, h.cursor)
	}
	press(a, key("j"))
	if h.focus != 1 || h.cursor != 0 {
		t.Fatalf("j past the last row → focus %d cursor %d", h.focus, h.cursor)
	}
	// k at the top rolls back onto the previous panel's last row.
	press(a, key("k"))
	if h.focus != 0 || h.cursor != 1 {
		t.Fatalf("k above the first row → focus %d cursor %d", h.focus, h.cursor)
	}
	press(a, key("k"))
	press(a, key("k"))
	if h.focus != last || h.cursor != len(h.panels[last].records)-1 {
		t.Fatalf("k from the first panel wraps to the last row of the last panel → focus %d cursor %d", h.focus, h.cursor)
	}

	// An empty panel holds the cursor on its placeholder line until j moves on.
	feedPanel(h, 1, nil)
	h.focus, h.cursor = 1, 0
	press(a, key("enter"))
	if len(a.stack) != 1 {
		t.Fatalf("enter on an empty panel must not navigate, top = %T", a.top())
	}
	press(a, key("j"))
	if h.focus != 2 {
		t.Fatalf("j from an empty panel moves to the next one, focus = %d", h.focus)
	}

	// The focused panel's query drives ctrl-q and the command echo.
	if h.DQL() != h.panels[2].dql || !strings.Contains(h.DQL(), "dt.kubernetes.events") {
		t.Errorf("DQL should follow focus:\n%s", h.DQL())
	}
	if !strings.Contains(h.Echo(), "dt.kubernetes.events") {
		t.Errorf("echo should carry the focused query: %s", h.Echo())
	}
}

// Each panel's enter jumps into the full view. The 24h panels must carry at
// least a 24h window, so the jump doesn't land on "no rows match"; a longer
// global window is kept.
func TestHomeDrillDowns(t *testing.T) {
	day := "24h"
	cases := []struct {
		name   string
		panel  int
		row    int
		tfKey  string // timeframe picker digit pressed first; "" keeps the default
		view   string
		tf     string
		filter string
		lens   int
	}{
		{name: "problem keeps 24h", panel: 0, row: 1, view: "problems", tf: day, filter: "P-2"},
		{name: "problem keeps longer window", panel: 0, tfKey: "4", view: "problems", tf: "7d", filter: "P-1"},
		{name: "k8s warning scopes the pod", panel: 2, view: "pods", tf: day},
		{name: "frontend error opens errors lens", panel: 3, view: "userevents", tf: day, filter: "TypeError", lens: 1},
		{name: "vulnerability carries its 24h window", panel: 4, view: "vulnerabilities", tf: day, filter: "S-1"},
		{name: "attack keeps 24h", panel: 5, view: "attacks", tf: day, filter: "F-1"},
		{name: "attack keeps longer window", panel: 5, tfKey: "4", view: "attacks", tf: "7d", filter: "F-1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := testApp(t, "home")
			if c.tfKey != "" {
				press(a, key("t"))
				press(a, key(c.tfKey))
			}
			h := homeOf(t, a)
			seedEveryPanel(h)
			h.focus, h.cursor = c.panel, c.row

			press(a, key("enter"))
			tv, ok := a.top().(*tableView)
			if !ok {
				t.Fatalf("top = %T, want *tableView", a.top())
			}
			if tv.spec.Name != c.view {
				t.Errorf("view = %s, want %s", tv.spec.Name, c.view)
			}
			if tv.scope.Timeframe.Label != c.tf {
				t.Errorf("timeframe = %s, want %s", tv.scope.Timeframe.Label, c.tf)
			}
			if tv.filter != c.filter {
				t.Errorf("filter = %q, want %q", tv.filter, c.filter)
			}
			if tv.scope.Lens != c.lens {
				t.Errorf("lens = %d, want %d", tv.scope.Lens, c.lens)
			}
		})
	}

	t.Run("k8s warning pod goes into the query scope", func(t *testing.T) {
		a := testApp(t, "home")
		h := homeOf(t, a)
		seedEveryPanel(h)
		h.focus, h.cursor = 2, 0
		press(a, key("enter"))
		tv := a.top().(*tableView)
		if e := tv.scope.Entity; e == nil || e.Name != "cart-0" || e.Type != "K8S_POD" {
			t.Fatalf("pods scope entity = %+v", tv.scope.Entity)
		}
	})

	t.Run("failing service opens its detail page", func(t *testing.T) {
		a := testApp(t, "home")
		h := homeOf(t, a)
		seedEveryPanel(h)
		h.focus, h.cursor = 1, 0
		press(a, key("enter"))
		dv, ok := a.top().(*detailView)
		if !ok {
			t.Fatalf("top = %T, want *detailView", a.top())
		}
		if dv.entity.ID != "SERVICE-0000000000000001" || dv.entity.Name != "checkout" || dv.entity.Type != "SERVICE" {
			t.Errorf("detail entity = %+v", dv.entity)
		}
	})

	t.Run("failing service without an id falls back to the services list", func(t *testing.T) {
		a := testApp(t, "home")
		h := homeOf(t, a)
		feedPanel(h, 1, nil, map[string]any{"svc": "orphan", "failed": "1"})
		h.focus, h.cursor = 1, 0
		if _, e := h.Selection(); e != nil {
			t.Errorf("a row without a smartscape id yields no entity, got %+v", e)
		}
		press(a, key("enter"))
		tv, ok := a.top().(*tableView)
		if !ok || tv.spec.Name != "services" || tv.scope.Timeframe.Label != catalog.DefaultTimeframe.Label {
			t.Fatalf("top = %T (%s), timeframe %s", a.top(), a.top().Crumb(), tv.scope.Timeframe.Label)
		}
	})
}

// The k8s warnings query returns up to 100 series; the panel ranks them by
// total occurrences and keeps the top 8.
func TestHomeK8sWarningsRankedAndCapped(t *testing.T) {
	a := testApp(t, "home")
	h := homeOf(t, a)
	series := func() []map[string]any {
		var recs []map[string]any
		for i := 1; i <= 10; i++ {
			recs = append(recs, map[string]any{
				"k8s.pod.name":     fmt.Sprintf("pod-%02d", i),
				"k8s.event.reason": "BackOff",
				"events":           []any{float64(i), 1.0},
			})
		}
		return recs
	}
	feedPanel(h, 2, nil, series()...)

	got := h.panels[2].records
	if len(got) != 8 {
		t.Fatalf("records = %d, want 8", len(got))
	}
	if got[0]["k8s.pod.name"] != "pod-10" || got[7]["k8s.pod.name"] != "pod-03" {
		t.Errorf("ranking = first %v, last %v", got[0]["k8s.pod.name"], got[7]["k8s.pod.name"])
	}
	// Other panels keep the server order and size.
	feedPanel(h, 3, nil, series()...)
	if len(h.panels[3].records) != 10 || h.panels[3].records[0]["k8s.pod.name"] != "pod-01" {
		t.Error("only the k8s panel is re-ranked")
	}
}

// Results for another owner or a panel index out of range are ignored.
func TestHomeIgnoresForeignResults(t *testing.T) {
	a := testApp(t, "home")
	h := homeOf(t, a)
	other := newHomeView(h.ds, h.tf)

	h.Update(dataMsg{owner: panelOwner{v: other, idx: 0}, seq: h.seq,
		records: []map[string]any{{"display_id": "P-9"}}})
	h.Update(dataMsg{owner: panelOwner{v: h, idx: len(h.panels)}, seq: h.seq,
		records: []map[string]any{{"display_id": "P-9"}}})
	if len(h.panels[0].records) != 0 || !h.panels[0].loading {
		t.Error("a result routed to another home view must not land here")
	}
}

// Changing the global window refetches only the failing services panel,
// which follows it; the rest stay pinned to their 24h lookback.
func TestHomeTimeframeRefetches(t *testing.T) {
	a := testApp(t, "home")
	h := homeOf(t, a)
	for i := range h.panels {
		feedPanel(h, i, nil)
	}
	seq := h.seq

	press(a, key("t"))
	press(a, key("4")) // 7d
	if h.seq == seq {
		t.Fatal("a timeframe change must start a new refresh generation")
	}
	if !h.Busy() {
		t.Error("panels reload after a timeframe change")
	}
	if h.tf.Label != "7d" {
		t.Errorf("home tf = %s", h.tf.Label)
	}
	if !strings.Contains(h.panels[1].dql, "from:now() - 7d") {
		t.Errorf("failing services follows the global window:\n%s", h.panels[1].dql)
	}
	if h.panels[1].seq != h.seq {
		t.Error("the global-following panel refetches on t")
	}
	for _, i := range []int{0, 2, 3, 4, 5} {
		if h.panels[i].seq == h.seq {
			t.Errorf("fixed panel %q must not refetch on t", h.panels[i].def.Title)
		}
		if !strings.Contains(h.panels[i].dql, "now() - 24h") {
			t.Errorf("panel %q keeps its 24h lookback:\n%s", h.panels[i].def.Title, h.panels[i].dql)
		}
	}
}

func TestHomeRowClasses(t *testing.T) {
	a := testApp(t, "home")
	h := homeOf(t, a)

	for _, c := range []struct{ level, class string }{
		{"CRITICAL", "error"}, {"HIGH", "warn"}, {"MEDIUM", ""}, {"LOW", "dim"}, {"", "dim"},
	} {
		if _, got := h.panels[4].def.Line(map[string]any{"level": c.level}); got != c.class {
			t.Errorf("vulnerability %q → class %q, want %q", c.level, got, c.class)
		}
	}
	if _, got := h.panels[5].def.Line(map[string]any{"finding.action": "Blocked"}); got != "ok" {
		t.Errorf("blocked attack → %q, want ok", got)
	}
	if _, got := h.panels[5].def.Line(map[string]any{"finding.action": "Monitored"}); got != "warn" {
		t.Errorf("monitored attack → %q, want warn", got)
	}
	text, _ := h.panels[0].def.Line(map[string]any{"display_id": "P-1", "name": "cpu saturation",
		"affected": []any{"checkout", "cart", "orders"}})
	if !strings.Contains(text, "checkout +2") {
		t.Errorf("problem line should name the first affected entity and count the rest: %q", text)
	}
	text, _ = h.panels[1].def.Line(map[string]any{"svc": "checkout", "ns": "shop", "failed": "4"})
	if !strings.Contains(text, "checkout (shop)") {
		t.Errorf("failing service line should carry its namespace: %q", text)
	}
}

func homeTestApp(t *testing.T, panels []HomePanelConfig, notice string) (*app, *homeView) {
	t.Helper()
	a := testAppSeeded(t, Options{ContextName: "test", SafetyLevel: "readonly", InitialView: "home",
		HomePanels: panels, HomeNotice: notice})
	return a, a.top().(*homeView)
}

// TestHomeConfiguredLayout: a workspace layout picks, orders and windows the
// panels; each one's DQL carries its own window.
func TestHomeConfiguredLayout(t *testing.T) {
	_, hv := homeTestApp(t, []HomePanelConfig{
		{ID: "vulnerabilities", Timeframe: "7d"},
		{ID: "problems"},
		{ID: "failing-services", Timeframe: "global"},
	}, "")
	if len(hv.panels) != 3 {
		t.Fatalf("panels = %d, want 3", len(hv.panels))
	}
	for i, want := range []struct{ id, from string }{
		{"vulnerabilities", "from:now() - 7d"},
		{"problems", "from:now() - 24h"},
		{"failing-services", "from:now() - 2h"},
	} {
		p := hv.panels[i]
		if p.def.ID != want.id || !strings.Contains(p.dql, want.from) {
			t.Errorf("panel %d = %s, dql %q; want %s with %q", i, p.def.ID, p.dql, want.id, want.from)
		}
	}
}

// TestHomeUnresolvedConfigFallsBack: a layout whose ids all slipped past
// validation still lands on the default home instead of an empty grid.
func TestHomeUnresolvedConfigFallsBack(t *testing.T) {
	a, h := homeTestApp(t, []HomePanelConfig{{ID: "nope"}}, "")
	if len(h.panels) != len(catalog.HomePanels) {
		t.Fatalf("panels = %d, want the %d defaults", len(h.panels), len(catalog.HomePanels))
	}
	_ = h.View(120, 40)
	press(a, key("tab"))
	if h.focus != 1 {
		t.Errorf("tab focus = %d, want 1", h.focus)
	}
}

// TestHomeWindowBadges: a panel the workspace pinned away from the header's
// window wears a ◷ pill; a curated default or global-following window is a
// dim label, and the header counts only the pinned panels.
func TestHomeWindowBadges(t *testing.T) {
	a, hv := homeTestApp(t, []HomePanelConfig{
		{ID: "vulnerabilities", Timeframe: "7d"},
		{ID: "failing-services"},
		{ID: "k8s-warnings"},
	}, "")
	out := ansi.Strip(hv.View(120, 30))
	if !strings.Contains(strings.Join(strings.Fields(out), " "), "open vulnerabilities ◷ 7d") {
		t.Errorf("differing panel should carry a ◷ 7d pill:\n%s", out)
	}
	if !strings.Contains(out, "failing services · 2h") {
		t.Errorf("global-following panel should show its window:\n%s", out)
	}
	if !strings.Contains(out, "kubernetes warnings · 24h") {
		t.Errorf("a curated default window is named dim, not flagged:\n%s", out)
	}
	if head := ansi.Strip(a.View()); !strings.Contains(head, "last 2h · ◷ 1 panel differ") {
		t.Errorf("header should count differing panels:\n%s", head)
	}
}

// TestHomeTimeframeChangeSkipsFixedPanels: t re-runs only the panels that
// follow the global window and says which ones stay fixed.
func TestHomeTimeframeChangeSkipsFixedPanels(t *testing.T) {
	a, hv := homeTestApp(t, []HomePanelConfig{
		{ID: "problems"},
		{ID: "failing-services"},
	}, "")
	problemsDQL := hv.panels[0].dql
	tf, _ := catalog.ParseTimeframe("6h")
	deliver(a, a.setTimeframe(tf))
	if hv.panels[0].dql != problemsDQL {
		t.Errorf("fixed problems panel re-ran: %q", hv.panels[0].dql)
	}
	if !strings.Contains(hv.panels[1].dql, "from:now() - 6h") {
		t.Errorf("global panel should follow t: %q", hv.panels[1].dql)
	}
	if !strings.Contains(a.status, "not following t: problems 24h") {
		t.Errorf("status should name the fixed panels, got %q", a.status)
	}
}

// TestHomeRejectedLayoutBanner: a rejected layout shows the default panels
// under a persistent banner naming the reason.
func TestHomeRejectedLayoutBanner(t *testing.T) {
	_, hv := homeTestApp(t, nil, `home.panels[1]: unknown panel "vulns"`)
	if len(hv.panels) != len(catalog.HomePanels) {
		t.Fatalf("rejected layout should fall back to the default panels, got %d", len(hv.panels))
	}
	out := ansi.Strip(hv.View(160, 30))
	for _, want := range []string{"✗ home config ignored", `unknown panel "vulns"`, "dtui workspace validate"} {
		if !strings.Contains(out, want) {
			t.Errorf("banner misses %q:\n%s", want, out)
		}
	}
	_, crafted := homeTestApp(t, nil, "home.panels[0]: \x1b[5mblink")
	if out := crafted.View(160, 30); strings.Contains(out, "\x1b[5m") || !strings.Contains(out, "␛[5mblink") {
		t.Errorf("banner must show the notice's control characters as glyphs:\n%q", out)
	}
	if _, clean := homeTestApp(t, nil, ""); strings.Contains(ansi.Strip(clean.View(160, 30)), "home config ignored") {
		t.Error("no banner without a notice")
	}
}

// TestHomeJumpCarriesPanelWindow: enter opens the full view over the
// panel's window, widened to the global one — the selected row must be in
// the jumped-to list. The header then names that window.
func TestHomeJumpCarriesPanelWindow(t *testing.T) {
	a, hv := homeTestApp(t, []HomePanelConfig{{ID: "vulnerabilities", Timeframe: "7d"}}, "")
	hv.Update(dataMsg{owner: panelOwner{v: hv, idx: 0}, seq: hv.panels[0].seq,
		records: []map[string]any{{"display_id": "S-1", "title": "synthetic"}}})
	press(a, key("enter"))
	tv, ok := a.top().(*tableView)
	if !ok || tv.spec.Name != "vulnerabilities" {
		t.Fatalf("enter should open vulnerabilities, top = %T", a.top())
	}
	if tv.scope.Timeframe.Label != "7d" {
		t.Errorf("jump window = %q, want the panel's 7d", tv.scope.Timeframe.Label)
	}
	if head := strings.Join(strings.Fields(ansi.Strip(a.View())), " "); !strings.Contains(head, "◷ last 7d (t: 2h)") {
		t.Errorf("header should name the view's own window:\n%s", head)
	}
}

// TestHomeColumnsSplit: order alone decides placement — panels alternate
// left and right; one panel or a narrow terminal stacks them.
func TestHomeColumnsSplit(t *testing.T) {
	for name, c := range map[string]struct {
		n    int
		wide bool
		want string
	}{
		"none":         {0, true, "[[]]"},
		"single":       {1, true, "[[0]]"},
		"odd wide":     {3, true, "[[0 2] [1]]"},
		"odd narrow":   {3, false, "[[0 1 2]]"},
		"default wide": {len(DefaultHomePanels()), true, "[[0 2 4] [1 3 5]]"},
	} {
		if got := fmt.Sprint(HomeColumns(c.n, c.wide)); got != c.want {
			t.Errorf("%s: columns = %s, want %s", name, got, c.want)
		}
	}
}

// TestHomeColumnsRender: the second panel renders beside the first, the
// third under the first, and a single panel spans the full width.
func TestHomeColumnsRender(t *testing.T) {
	lineWith := func(view, s string) string {
		for _, l := range strings.Split(view, "\n") {
			if strings.Contains(l, s) {
				return l
			}
		}
		return ""
	}
	_, hv := homeTestApp(t, []HomePanelConfig{
		{ID: "problems"},
		{ID: "attacks"},
		{ID: "vulnerabilities"},
	}, "")
	view := ansi.Strip(hv.View(140, 30))
	if top := lineWith(view, "active problems"); !strings.Contains(top, "attack detections") {
		t.Errorf("attacks should sit right of problems:\n%s", view)
	}
	if strings.Contains(lineWith(view, "open vulnerabilities"), "attack detections") {
		t.Errorf("vulnerabilities should sit under problems, not beside attacks:\n%s", view)
	}

	_, hv = homeTestApp(t, []HomePanelConfig{{ID: "problems"}}, "")
	view = ansi.Strip(hv.View(140, 30))
	if top := strings.TrimRight(lineWith(view, "active problems"), " "); strings.Count(top, "╭") != 1 || len([]rune(top)) < 130 {
		t.Errorf("a single panel should span the full width: %q", top)
	}
}

// TestHomePanelLineSafeText: record text in a panel line cannot drive the
// terminal — control characters render as visible glyphs.
func TestHomePanelLineSafeText(t *testing.T) {
	_, hv := homeTestApp(t, []HomePanelConfig{{ID: "problems"}}, "")
	hv.Update(dataMsg{owner: panelOwner{v: hv, idx: 0}, seq: hv.panels[0].seq,
		records: []map[string]any{{"display_id": "P-1", "status": "ACTIVE", "name": "evil\x1b[2Jname"}}})
	out := hv.View(120, 20)
	if strings.Contains(out, "\x1b[2J") || !strings.Contains(ansi.Strip(out), "evil␛[2Jname") {
		t.Errorf("control characters should render as glyphs:\n%q", ansi.Strip(out))
	}
}

// TestHomeColumnsTabOrder: tab follows entry order.
func TestHomeColumnsTabOrder(t *testing.T) {
	a, hv := homeTestApp(t, []HomePanelConfig{
		{ID: "problems"},
		{ID: "attacks"},
		{ID: "vulnerabilities"},
	}, "")
	var order []string
	for range hv.panels {
		order = append(order, hv.panels[hv.focus].def.ID)
		press(a, key("tab"))
	}
	if got := strings.Join(order, ","); got != "problems,attacks,vulnerabilities" {
		t.Errorf("tab order = %s", got)
	}
}

// TestHeaderNamesAbsoluteWindow: a view anchored to an absolute window (a
// trace's, a problem's) shows its range in the header, not "last trace".
func TestHeaderNamesAbsoluteWindow(t *testing.T) {
	a := testApp(t, "logs")
	tv, ok := a.top().(*tableView)
	if !ok {
		t.Fatalf("top = %T, want *tableView", a.top())
	}
	tv.scope.Timeframe = anchoredWindow(time.Date(2026, 7, 15, 11, 41, 0, 0, time.Local), 5*time.Minute)
	head := strings.Join(strings.Fields(ansi.Strip(a.View())), " ")
	if strings.Contains(head, "last trace") || !strings.Contains(head, "◷ 11:36 → 11:46 (t: 2h)") {
		t.Errorf("header should name the trace window's range:\n%s", head)
	}
}
