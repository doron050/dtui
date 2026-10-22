package tui

import (
	"strings"
	"testing"

	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
)

func TestIntentLinks(t *testing.T) {
	env := "https://abc.apps.dynatrace.com"
	first := func(opts []linkOption) string {
		if len(opts) == 0 {
			return ""
		}
		return opts[0].URL
	}
	problem := first(linkOptionsFor(env, map[string]any{"event.kind": "DAVIS_PROBLEM", "event.id": "e-1"}, nil, "", ""))
	if !strings.Contains(problem, "/ui/intent/dynatrace.davis.problems/view-problem#") {
		t.Errorf("problem link = %s", problem)
	}
	trace := first(linkOptionsFor(env, nil, nil, "abc123", ""))
	if !strings.Contains(trace, "/ui/intent/dynatrace.distributedtracing/view-trace#") {
		t.Errorf("trace link = %s", trace)
	}
	pod := linkOptionsFor(env, nil, &catalog.Entity{ID: "K8S_POD-1", Type: "K8S_POD"}, "", "")
	if !strings.Contains(first(pod), "/ui/intent/dynatrace.kubernetes/view-entity-dt.smartscape.k8s_pod#") {
		t.Errorf("pod link = %s", first(pod))
	}
	// Typed entities offer their app plus the topology explorer.
	if len(pod) != 2 || !strings.Contains(pod[1].URL, "/ui/intent/dynatrace.smartscape/view_topology_in_context#") {
		t.Errorf("pod options = %+v, want app + topology", pod)
	}
	generic := first(linkOptionsFor(env, nil, &catalog.Entity{ID: "DISK-1", Type: "DISK"}, "", ""))
	if !strings.Contains(generic, "/ui/intent/dynatrace.smartscape/view_topology_in_context#") {
		t.Errorf("generic link = %s", generic)
	}
}

// TestOpenWithOptions covers the picker composition: record-carried URLs
// (the vulnerability page's designated link) and the query-as-notebook
// fallback both surface as targets.
func TestOpenWithOptions(t *testing.T) {
	env := "https://abc.apps.dynatrace.com"
	vuln := map[string]any{
		"display_id": "S-4",
		"url":        "https://abc.live.dynatrace.com/ui/security/problem/129",
	}
	opts := linkOptionsFor(env, vuln, nil, "", "fetch security.events")
	if len(opts) != 2 {
		t.Fatalf("options = %+v, want record url + notebook", opts)
	}
	if opts[0].URL != vuln["url"] {
		t.Errorf("first option = %+v, want the record's url field", opts[0])
	}
	if !strings.Contains(opts[1].URL, "/ui/intent/dynatrace.notebooks/view-query#") {
		t.Errorf("second option = %+v, want notebook query", opts[1])
	}
	// Nothing at all → no options.
	if got := linkOptionsFor(env, nil, nil, "", ""); len(got) != 0 {
		t.Errorf("empty selection produced options: %+v", got)
	}
}

// TestDashboardOpenLink covers the dashboards view: a dashboard document's
// id becomes a single Dashboards-app deep link, so 'o' opens it directly.
func TestDashboardOpenLink(t *testing.T) {
	env := "https://abc.apps.dynatrace.com"
	dash := map[string]any{"type": "dashboard", "id": "abc-123", "name": "Fleet"}
	opts := linkOptionsFor(env, dash, nil, "", "")
	if len(opts) != 1 {
		t.Fatalf("options = %+v, want a single dashboard link", opts)
	}
	if opts[0].URL != env+"/ui/apps/dynatrace.dashboards/dashboard/abc-123" {
		t.Errorf("dashboard link = %s", opts[0].URL)
	}
}

// TestOpenLinkHook: when Options.OpenURL is set (the wasm host — a browser tab
// can't shell out), openLink routes through it instead of the system browser.
func TestOpenLinkHook(t *testing.T) {
	var got string
	a, err := newApp(Options{
		ContextName: "test", SafetyLevel: "readonly", InitialView: "home",
		OpenURL: func(u string) error { got = u; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "https://abc.apps.dynatrace.com/ui/intent/x/y#z"
	if cmd := a.openLink(linkOption{Label: "trace", URL: want}); cmd == nil {
		t.Fatal("openLink returned nil cmd")
	}
	if got != want {
		t.Errorf("OpenURL hook got %q, want %q", got, want)
	}
}
