package tui

import (
	"strings"
	"testing"

	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
)

// discoverRows simulates a pass-1 dim discovery result: records whose field
// names are the dims available on the service metrics. Excludes bookkeeping
// fields that ServiceDimExclude strips.
func discoverRows() []map[string]any {
	return []map[string]any{
		{
			"dt.smartscape.service":       "SERVICE-AAA",
			"metric.key":                  "dt.service.request.count",
			"dt.metrics.source":           "openpipeline:spans",
			"dt.system.monitoring_source": "fullstack_container",
			"failed":                      false,
			"dt.service.name":             "cargo-api",
			"service.name":                "cargo-api.ns",
			"primary_tags.app":            "demo-app",
			"primary_tags.env":            "demo",
			"k8s.namespace.name":          "demo-app",
			"k8s.workload.kind":           "deployment",
		},
	}
}

// serviceRows simulates a pass-2 collapsed roster result.
func serviceRows() []map[string]any {
	return []map[string]any{
		{
			"id":                           "SERVICE-AAA",
			"name":                         "cargo-api",
			"dt.service.name":              "cargo-api",
			"service.name":                 "cargo-api.ns",
			"primary_tags.app":             "demo-app",
			"primary_tags.env":             "demo",
			"dt.service_detection.version": "2",
			"lifetime": map[string]any{
				"start": "2026-01-01T00:00:00Z",
				"end":   "2026-07-22T12:00:00Z",
			},
		},
	}
}

// deliverDiscover injects a discovery dataMsg for the top tableView.
func deliverDiscover(a *app, rows []map[string]any) {
	tv, ok := a.top().(*tableView)
	if !ok {
		return
	}
	tv.seq++
	a.Update(dataMsg{owner: discoverOwner{tv}, seq: tv.seq, records: rows})
}

func TestServicesRosterDiscoveryFlow(t *testing.T) {
	a := testApp(t, "services")
	tv, ok := a.top().(*tableView)
	if !ok {
		t.Fatal("top view is not a tableView")
	}

	// testApp runs Init() which triggers discovery; with the nil runFn the
	// result is empty → degraded (discoverDone=true, dims empty). Re-inject
	// a non-empty discovery result to verify the non-degraded path.
	tv.discoverDone = false
	tv.discoverSegSnap = ""
	deliverDiscover(a, discoverRows())

	if !tv.discoverDone {
		t.Error("discoverDone should be true after discovery")
	}
	if len(tv.metricDims) == 0 {
		t.Error("metricDims should be populated after non-empty discovery")
	}
	// k8s.workload.kind must be a candidate — it's not in ServiceDimExclude.
	found := false
	for _, d := range tv.metricDims {
		if d == "k8s.workload.kind" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("metricDims must include k8s.workload.kind, got %v", tv.metricDims)
	}
}

func TestServicesRosterPrimaryTagColumns(t *testing.T) {
	a := testApp(t, "services")
	a.ds.primaryTags = []string{"app", "env"}
	deliverDiscover(a, discoverRows())
	seedRows(t, a, serviceRows())

	tv := a.top().(*tableView)
	cols := tv.columns()
	titles := make([]string, len(cols))
	for i, c := range cols {
		titles[i] = c.Title
	}
	// APP and ENV columns must appear.
	for _, want := range []string{"APP", "ENV"} {
		found := false
		for _, t2 := range titles {
			if t2 == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("columns missing %q — got %v", want, titles)
		}
	}
}

func TestServicesRosterFacetCandidatesFromDims(t *testing.T) {
	a := testApp(t, "services")
	deliverDiscover(a, discoverRows())
	seedRows(t, a, serviceRows())

	tv := a.top().(*tableView)
	cands := tv.facetCandidates()
	if len(cands) == 0 {
		t.Fatal("facetCandidates should be non-empty after discovery")
	}
	// k8s.workload.kind is a non-by: dim — must be a candidate.
	found := false
	for _, c := range cands {
		if c == "k8s.workload.kind" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("facetCandidates missing k8s.workload.kind (non-by: dim), got %v", cands)
	}
	// service.name must also be a candidate.
	found2 := false
	for _, c := range cands {
		if c == "service.name" {
			found2 = true
			break
		}
	}
	if !found2 {
		t.Errorf("facetCandidates missing service.name, got %v", cands)
	}
}

func TestServicesRosterFacetInFilterBlock(t *testing.T) {
	a := testApp(t, "services")
	a.ds.primaryTags = []string{"app"}
	deliverDiscover(a, discoverRows())
	seedRows(t, a, serviceRows())

	tv := a.top().(*tableView)
	// Add a facet and verify it lands in filter:{} of the composed DQL.
	tv.facets = []catalog.Facet{{Field: "k8s.namespace.name", Value: "demo-app"}}
	dql := tv.composeDQL()

	if !strings.Contains(dql, `"demo-app"`) {
		t.Errorf("composeDQL: facet value not in DQL:\n%s", dql)
	}
	// Must be in filter:{...}, i.e. before | summarize.
	filterIdx := strings.Index(dql, "filter: {")
	summarizeIdx := strings.Index(dql, "| summarize")
	if filterIdx < 0 || summarizeIdx < 0 || filterIdx > summarizeIdx {
		t.Errorf("composeDQL: facet not embedded in filter:{} pre-summarize; filterIdx=%d summarizeIdx=%d\n%s",
			filterIdx, summarizeIdx, dql)
	}
	// Must NOT appear as a | filter stage.
	if strings.Contains(dql, "\n| filter") {
		t.Errorf("composeDQL: MetricRoster must not inject | filter pipe stages:\n%s", dql)
	}
}

func TestServicesRosterSearchInFilterBlock(t *testing.T) {
	a := testApp(t, "services")
	a.ds.primaryTags = []string{"app"}
	deliverDiscover(a, discoverRows())
	seedRows(t, a, serviceRows())

	tv := a.top().(*tableView)
	tv.searches = []string{"cargo"}
	dql := tv.composeDQL()

	// contains(toString(…)) must appear pre-summarize.
	if !strings.Contains(dql, "toString(") {
		t.Errorf("composeDQL: /- search must use toString wrap, got:\n%s", dql)
	}
	if !strings.Contains(dql, `"cargo"`) {
		t.Errorf("composeDQL: search term not in DQL:\n%s", dql)
	}
	// Must be inside filter:{} (before summarize).
	filterIdx := strings.Index(dql, "filter: {")
	summarizeIdx := strings.Index(dql, "| summarize")
	if filterIdx < 0 || summarizeIdx < 0 || filterIdx > summarizeIdx {
		t.Errorf("composeDQL: search not in filter:{} pre-summarize")
	}
	// Must NOT appear as | search.
	if strings.Contains(dql, "| search") {
		t.Errorf("composeDQL: MetricRoster must not emit | search stage:\n%s", dql)
	}
}

func TestServicesRosterSegmentChangeResetsDiscovery(t *testing.T) {
	a := testApp(t, "services")
	deliverDiscover(a, discoverRows())

	tv := a.top().(*tableView)
	if !tv.discoverDone {
		t.Fatal("discovery should be done after first run")
	}
	snap0 := tv.discoverSegSnap

	// Fake a stale snap: if the current live snap differs, discoverCmd re-runs.
	tv.discoverSegSnap = snap0 + "-stale"
	tv.seq++
	cmd := tv.discoverCmd()
	if cmd == nil {
		t.Error("discoverCmd should return a command when segment snap changed")
	}
	if tv.discoverDone {
		t.Error("discoverDone should be reset when snap differs")
	}
}

func TestServicesRosterDiscoveryDegrades(t *testing.T) {
	a := testApp(t, "services")
	a.ds.primaryTags = []string{"app", "env"}

	tv := a.top().(*tableView)
	tv.seq++
	// Empty discovery result → fallback to declared keys.
	a.Update(dataMsg{owner: discoverOwner{tv}, seq: tv.seq, records: nil})

	if !tv.discoverDone {
		t.Error("discoverDone should be set even on empty result")
	}
	if len(tv.rosterPrimaryTagKeys) != 2 {
		t.Errorf("rosterPrimaryTagKeys should fall back to declared [app env], got %v", tv.rosterPrimaryTagKeys)
	}
	// metricDims empty — no facet candidates from dims.
	if len(tv.metricDims) != 0 {
		t.Errorf("metricDims should be empty on degraded discovery, got %v", tv.metricDims)
	}
}

func TestServicesRosterPeekSkipsNullTags(t *testing.T) {
	a := testApp(t, "services")
	a.ds.primaryTags = []string{"app", "env", "cost-center"} // cost-center absent from row
	deliverDiscover(a, discoverRows())

	row := serviceRows()[0] // has app+env, no cost-center
	lines := a.top().(*tableView).primaryTagPreviewLines(row, 80)

	// app and env should appear.
	preview := strings.Join(lines, "\n")
	if !strings.Contains(preview, "demo-app") {
		t.Errorf("peek should show primary_tags.app value, got:\n%s", preview)
	}
	// cost-center must not appear (null key skipped).
	if strings.Contains(preview, "cost-center") {
		t.Errorf("peek must not show null declared key 'cost-center', got:\n%s", preview)
	}
}
