package tui

import (
	"fmt"
	"strings"
	"testing"

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

// TestHomeWindowBadges: a panel whose window differs from the header's wears
// a ◷ pill, one matching it a dim label, and the header counts the
// differing panels.
func TestHomeWindowBadges(t *testing.T) {
	a, hv := homeTestApp(t, []HomePanelConfig{
		{ID: "vulnerabilities", Timeframe: "7d"},
		{ID: "failing-services"},
	}, "")
	out := ansi.Strip(hv.View(120, 30))
	if !strings.Contains(strings.Join(strings.Fields(out), " "), "open vulnerabilities ◷ 7d") {
		t.Errorf("differing panel should carry a ◷ 7d pill:\n%s", out)
	}
	if !strings.Contains(out, "failing services · 2h") {
		t.Errorf("global-following panel should show its window:\n%s", out)
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

func TestHomeColumnsSplit(t *testing.T) {
	left := []HomePanelConfig{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	mixed := []HomePanelConfig{{ID: "a"}, {ID: "b", Right: true}, {ID: "c"}}
	for name, c := range map[string]struct {
		panels []HomePanelConfig
		wide   bool
		want   string
	}{
		"left only":    {left, true, "[[0 1 2]]"},
		"mixed wide":   {mixed, true, "[[0 2] [1]]"},
		"mixed narrow": {mixed, false, "[[0 1 2]]"},
		"default":      {DefaultHomePanels(), true, "[[0 2 4] [1 3 5]]"},
	} {
		if got := fmt.Sprint(HomeColumns(c.panels, c.wide)); got != c.want {
			t.Errorf("%s: columns = %s, want %s", name, got, c.want)
		}
	}
}

// TestHomeColumnsRender: right-column panels render beside the left ones,
// and without any right panel every panel spans the full width.
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
		{ID: "attacks", Right: true},
		{ID: "vulnerabilities"},
	}, "")
	view := ansi.Strip(hv.View(140, 30))
	if top := lineWith(view, "active problems"); !strings.Contains(top, "attack detections") {
		t.Errorf("attacks should sit right of problems:\n%s", view)
	}
	if strings.Contains(lineWith(view, "open vulnerabilities"), "attack detections") {
		t.Errorf("vulnerabilities should sit under problems, not beside attacks:\n%s", view)
	}

	_, hv = homeTestApp(t, []HomePanelConfig{{ID: "problems"}, {ID: "attacks"}}, "")
	view = ansi.Strip(hv.View(140, 30))
	if top := strings.TrimRight(lineWith(view, "active problems"), " "); strings.Count(top, "╭") != 1 || len([]rune(top)) < 130 {
		t.Errorf("left-only panels should span the full width: %q", top)
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

// TestHomeColumnsTabOrder: tab follows entry order, not screen columns.
func TestHomeColumnsTabOrder(t *testing.T) {
	a, hv := homeTestApp(t, []HomePanelConfig{
		{ID: "problems"},
		{ID: "attacks", Right: true},
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
