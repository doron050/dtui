package catalog

import (
	"strings"
	"testing"
)

// fixture scope with a 2h timeframe (no entity).
var serviceScope = Scope{Timeframe: Timeframes[1]}

// serviceScope with discovered primary-tag keys.
func serviceScopeWithTags(keys ...string) Scope {
	s := serviceScope
	s.PrimaryTagKeys = keys
	return s
}

func TestServicesDiscoverQuery(t *testing.T) {
	spec := Lookup("services")
	if spec == nil {
		t.Fatal("services spec not found")
	}
	if spec.DiscoverQuery == nil {
		t.Fatal("services spec missing DiscoverQuery")
	}
	q := spec.DiscoverQuery(serviceScope)
	if !strings.Contains(q, "metrics from:") {
		t.Errorf("DiscoverQuery: expected metrics source, got:\n%s", q)
	}
	for _, key := range ServiceMetricKeys {
		if !strings.Contains(q, key) {
			t.Errorf("DiscoverQuery: missing metric key %q in:\n%s", key, q)
		}
	}
	if !strings.HasPrefix(strings.TrimSpace(q), "metrics from:") {
		t.Errorf("DiscoverQuery: first line must start with 'metrics from:' for convention, got:\n%s", q)
	}
}

func TestServicesListQueryBase(t *testing.T) {
	spec := Lookup("services")
	q := spec.Query(serviceScope)

	// Must be a timeseries scalar/union roster.
	if !strings.HasPrefix(q, "timeseries {") {
		t.Errorf("Query: expected timeseries source, got:\n%s", q)
	}
	for _, key := range ServiceMetricKeys {
		if !strings.Contains(q, key) {
			t.Errorf("Query: missing metric key %q", key)
		}
	}
	if !strings.Contains(q, "scalar: true") {
		t.Error("Query: expected scalar: true")
	}
	if !strings.Contains(q, "union: true") {
		t.Error("Query: expected union: true")
	}
	if !strings.Contains(q, "isNotNull(dt.smartscape.service)") {
		t.Error("Query: expected isNotNull guard in filter:{}")
	}
	// Smartscape lookup for name/lifetime/detection.
	if !strings.Contains(q, `smartscapeNodes "SERVICE"`) {
		t.Error("Query: expected smartscapeNodes lookup")
	}
	if !strings.Contains(q, "getNodeName(dt.smartscape.service)") {
		t.Error("Query: expected getNodeName fallback")
	}
	// Sort/limit tail required by convention.
	if !strings.Contains(q, "| sort name asc") {
		t.Error("Query: expected sort tail")
	}
	if !strings.Contains(q, "| limit 500") {
		t.Error("Query: expected limit tail")
	}
}

func TestServicesListQueryWithPrimaryTags(t *testing.T) {
	spec := Lookup("services")
	q := spec.Query(serviceScopeWithTags("app", "env"))

	// Both keys must appear in by:{} with dotted backtick escaping where needed.
	if !strings.Contains(q, "primary_tags.app") {
		t.Error("Query: expected primary_tags.app in by:{}")
	}
	if !strings.Contains(q, "primary_tags.env") {
		t.Error("Query: expected primary_tags.env in by:{}")
	}
	// takeAny aggregation — null-ignoring, simpler than collectDistinct for
	// ownership metadata that is consistent across a service's series.
	if !strings.Contains(q, "takeAny") {
		t.Error("Query: expected takeAny for primary-tag aggregation")
	}
}

func TestServicesListQueryWithFilterConds(t *testing.T) {
	spec := Lookup("services")
	s := serviceScopeWithTags("app")
	s.FilterConds = []string{`primary_tags.app == "demo-app"`, `k8s.workload.kind == "deployment"`}
	q := spec.Query(s)

	if !strings.Contains(q, `primary_tags.app == "demo-app"`) {
		t.Error("Query: facet condition not embedded in filter:{}")
	}
	if !strings.Contains(q, `k8s.workload.kind == "deployment"`) {
		t.Error("Query: non-by: dim condition not embedded in filter:{}")
	}
	// Conditions must be inside filter:{...} before summarize — i.e. before | summarize.
	filterIdx := strings.Index(q, "filter: {")
	summarizeIdx := strings.Index(q, "| summarize")
	if filterIdx < 0 || summarizeIdx < 0 || filterIdx > summarizeIdx {
		t.Errorf("Query: filter:{} must appear before | summarize; filterIdx=%d summarizeIdx=%d", filterIdx, summarizeIdx)
	}
}

func TestScanMetricDims(t *testing.T) {
	records := []map[string]any{
		{"metric.key": "dt.service.request.count", "dt.smartscape.service": "S-1", "primary_tags.app": "foo", "k8s.namespace.name": "ns"},
		{"metric.key": "dt.service.faas_invoke.count", "dt.smartscape.service": "S-1", "primary_tags.env": "prod"},
		{"dt.metrics.source": "openpipeline", "failed": true},
	}
	declared := []string{"app"}
	dims, ptKeys := ScanMetricDims(records, ServiceDimExclude, declared)

	// Exclude set stripped.
	for _, excluded := range []string{"metric.key", "dt.smartscape.service", "dt.metrics.source", "failed"} {
		for _, d := range dims {
			if d == excluded {
				t.Errorf("ScanMetricDims: excluded key %q survived in dims", excluded)
			}
		}
	}
	// primary_tags.* and k8s.* survive.
	found := map[string]bool{}
	for _, d := range dims {
		found[d] = true
	}
	for _, want := range []string{"primary_tags.app", "primary_tags.env", "k8s.namespace.name"} {
		if !found[want] {
			t.Errorf("ScanMetricDims: expected %q in dims, got %v", want, dims)
		}
	}

	// primaryTagKeys: declared "app" first, then discovered "env".
	if len(ptKeys) < 2 {
		t.Fatalf("ScanMetricDims: expected ≥2 primaryTagKeys, got %v", ptKeys)
	}
	if ptKeys[0] != "app" {
		t.Errorf("ScanMetricDims: declared key 'app' must be first, got %v", ptKeys)
	}
	if ptKeys[1] != "env" {
		t.Errorf("ScanMetricDims: discovered key 'env' must follow declared, got %v", ptKeys)
	}
}

func TestServiceFieldsSummaryQuery(t *testing.T) {
	tf := Timeframes[1]
	q := ServiceFieldsSummaryQuery(tf, Facet{Field: "primary_tags.app"}, nil)

	if !strings.HasPrefix(q, "metrics from:") {
		t.Errorf("ServiceFieldsSummaryQuery: must start with 'metrics from:', got:\n%s", q)
	}
	for _, key := range ServiceMetricKeys {
		if !strings.Contains(q, key) {
			t.Errorf("ServiceFieldsSummaryQuery: missing metric key %q", key)
		}
	}
	if !strings.Contains(q, "fieldsSummary") {
		t.Error("ServiceFieldsSummaryQuery: missing fieldsSummary")
	}
	if !strings.Contains(q, "primary_tags.app") {
		t.Error("ServiceFieldsSummaryQuery: missing target field")
	}
}

func TestFacetConditionParenthesized(t *testing.T) {
	// Multi-leg facets (integer, id-shaped) must be parenthesized for filter:{}.
	intFacet := Facet{Field: "logical_cores", Value: "4"}
	cond := intFacet.Condition()
	if !strings.HasPrefix(cond, "(") || !strings.HasSuffix(cond, ")") {
		t.Errorf("Condition(): multi-leg predicate must be parenthesized, got: %s", cond)
	}
	// Plain string — no parens needed.
	strFacet := Facet{Field: "k8s.namespace.name", Value: "demo-app"}
	cond2 := strFacet.Condition()
	if strings.HasPrefix(cond2, "(") {
		t.Errorf("Condition(): plain string predicate should not be parenthesized, got: %s", cond2)
	}
}

func TestContainsSearchCondition(t *testing.T) {
	cond := ContainsSearchCondition("rpc", []string{"dt.service.name", "primary_tags.app"})
	if !strings.Contains(cond, "toString(dt.service.name)") {
		t.Errorf("ContainsSearchCondition: expected toString wrap, got: %s", cond)
	}
	if !strings.Contains(cond, `"rpc"`) {
		t.Errorf("ContainsSearchCondition: expected search term, got: %s", cond)
	}
	if !strings.HasPrefix(cond, "(") {
		t.Errorf("ContainsSearchCondition: expected outer parens, got: %s", cond)
	}

	// Empty dims fallback.
	cond2 := ContainsSearchCondition("foo", nil)
	if !strings.Contains(cond2, "dt.service.name") {
		t.Errorf("ContainsSearchCondition: empty dims should fall back to dt.service.name, got: %s", cond2)
	}
}

func TestServiceDetailQuery(t *testing.T) {
	e := Entity{ID: "SERVICE-AABBCCDDEEFF0011", Type: "SERVICE"}
	q := ServiceDetailQuery(e)

	// Must be a metrics command (0 datapoints — series descriptors only).
	if !strings.HasPrefix(q, "metrics from:") {
		t.Errorf("ServiceDetailQuery: must start with 'metrics from:', got:\n%s", q)
	}
	for _, key := range ServiceMetricKeys {
		if !strings.Contains(q, key) {
			t.Errorf("ServiceDetailQuery: missing metric key %q", key)
		}
	}
	if !strings.Contains(q, "SERVICE-AABBCCDDEEFF0011") {
		t.Error("ServiceDetailQuery: entity ID not scoped in filter")
	}
	if !strings.Contains(q, "toSmartscapeId") {
		t.Error("ServiceDetailQuery: must use toSmartscapeId for id comparison")
	}
	// No summarize — raw series rows are folded client-side by the inspector.
	if strings.Contains(q, "| summarize") {
		t.Error("ServiceDetailQuery: must not summarize — dims are discovered client-side by folding all rows")
	}
	// No hardcoded dim list.
	if strings.Contains(q, "k8s.namespace.name") {
		t.Error("ServiceDetailQuery: must not hardcode dims — fully dynamic via row folding")
	}
}

func TestDetailQueryDispatchesServiceToMetricQuery(t *testing.T) {
	svc := Entity{ID: "SERVICE-AABBCCDDEEFF0011", Type: "SERVICE"}
	q := DetailQuery(svc)
	if strings.HasPrefix(q, "smartscapeNodes") {
		t.Error("DetailQuery(SERVICE): must not use smartscapeNodes — service nodes are attribute-poor")
	}
	if !strings.HasPrefix(q, "metrics from:") {
		t.Errorf("DetailQuery(SERVICE): expected metrics-based detail query, got:\n%s", q)
	}

	// Other types still use smartscapeNodes.
	host := Entity{ID: "HOST-AABBCCDDEEFF0011", Type: "HOST"}
	qh := DetailQuery(host)
	if !strings.HasPrefix(qh, "smartscapeNodes") {
		t.Errorf("DetailQuery(HOST): expected smartscapeNodes, got:\n%s", qh)
	}
}

func TestServicesSpecMetricRosterFlag(t *testing.T) {
	spec := Lookup("services")
	if !spec.MetricRoster {
		t.Error("services spec must have MetricRoster=true")
	}
}

func TestServicesServiceNameKeyFact(t *testing.T) {
	facts := KeyFacts("SERVICE")
	labels := make([]string, len(facts))
	for i, f := range facts {
		labels[i] = f.Label
	}
	found := false
	for _, l := range labels {
		if l == "otel name" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("KeyFacts(SERVICE): expected 'otel name' fact for service.name, got %v", labels)
	}
}
