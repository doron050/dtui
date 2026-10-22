package catalog

import (
	"strings"
	"testing"
)

func TestPodsQueryComposition(t *testing.T) {
	spec := Lookup("pods")

	unscoped := spec.Query(fixtureScope(nil))
	for _, want := range []string{
		`smartscapeNodes "K8S_POD", from:now() - 2h`,
		`parse k8s.object, "JSON:obj"`,
		"expand cs = obj[status][containerStatuses]",
		"restarts = sum(toLong(cs[restartCount]))",
		// The label map is projected raw (not aliased) so the peek can render
		// it and tag discovery finds it. It lives in the tags: map — the
		// manifest strips it (learnings §1.22). Annotations are NOT projected
		// onto rows (bloat); they stay facetable via lazy discovery only.
		"`tags:k8s.labels` = takeAny(`tags:k8s.labels`)",
		"| sort phase asc, namespace asc, name asc",
	} {
		if !strings.Contains(unscoped, want) {
			t.Errorf("pods query missing %q:\n%s", want, unscoped)
		}
	}
	if strings.Contains(unscoped, "tags:k8s.annotations") {
		t.Errorf("annotations must not be projected onto pod rows:\n%s", unscoped)
	}

	// The label projection sits inside the summarize body, before its closing
	// brace — a | fieldsAdd after summarize would yield null.
	if lblIdx, sumClose := strings.Index(unscoped, "`tags:k8s.labels` = takeAny"), strings.Index(unscoped, "}, by:{id}"); lblIdx < 0 || lblIdx > sumClose {
		t.Errorf("labels projection not inside summarize body (lblIdx=%d sumClose=%d)", lblIdx, sumClose)
	}

	byNS := spec.Query(fixtureScope(&Entity{ID: "K8S_NAMESPACE-1", Name: "shop", Type: "K8S_NAMESPACE"}))
	if !strings.Contains(byNS, `| filter k8s.namespace.name == "shop"`) {
		t.Errorf("namespace scope not composed:\n%s", byNS)
	}

	byNode := spec.Query(fixtureScope(&Entity{ID: "K8S_NODE-1", Name: "node-1", Type: "K8S_NODE"}))
	if !strings.Contains(byNode, `| filter k8s.node.name == "node-1"`) {
		t.Errorf("node scope not composed:\n%s", byNode)
	}

	byWL := spec.Query(fixtureScope(&Entity{ID: "K8S_STATEFULSET-1", Name: "kafka", Type: "K8S_STATEFULSET"}))
	if !strings.Contains(byWL, `k8s.workload.kind == "statefulset" and k8s.workload.name == "kafka"`) {
		t.Errorf("workload scope not composed:\n%s", byWL)
	}

	// A service scope has no pod-side field to filter by; it joins through
	// the service's runs_on edges instead (validated live on two tenants).
	bySvc := spec.Query(fixtureScope(&Entity{ID: "SERVICE-1", Name: "checkout", Type: "SERVICE"}))
	for _, want := range []string{
		`| join [smartscapeEdges "*" | filter source_id == toSmartscapeId("SERVICE-1") and type == "runs_on"`,
		"on:{left[id] == right[target_id]}, kind:inner",
		"| fieldsRemove right.target_id",
	} {
		if !strings.Contains(bySvc, want) {
			t.Errorf("service scope missing %q:\n%s", want, bySvc)
		}
	}
}
func TestWorkloadsQueryCoalescesDaemonSetStatus(t *testing.T) {
	q := Lookup("workloads").Query(fixtureScope(nil))
	for _, want := range []string{
		`smartscapeNodes "K8S_DEPLOYMENT", "K8S_STATEFULSET", "K8S_DAEMONSET"`,
		"coalesce(obj[status][readyReplicas], obj[status][numberReady])",
		"coalesce(obj[spec][replicas], obj[status][desiredNumberScheduled])",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("workloads query missing %q:\n%s", want, q)
		}
	}
}

// TestK8sSpecsProjectLabelMaps pins the raw label-map projection (for the peek)
// and the FacetPriority pinning across every k8s list where the label tag is
// populated (pods, workloads, namespaces, nodes — clusters carry none,
// validated live). Annotations are never projected onto rows (bloat) — they
// stay facetable via lazy discovery only.
func TestK8sSpecsProjectLabelMaps(t *testing.T) {
	for _, name := range []string{"pods", "workloads", "namespaces", "nodes"} {
		spec := Lookup(name)
		q := spec.Query(fixtureScope(nil))
		if !strings.Contains(q, "`tags:k8s.labels`") {
			t.Errorf("%s query missing raw `tags:k8s.labels` projection:\n%s", name, q)
		}
		if strings.Contains(q, "tags:k8s.annotations") {
			t.Errorf("%s must not project annotations onto rows:\n%s", name, q)
		}
		if len(spec.FacetPriority) == 0 {
			t.Errorf("%s should pin preselected facets on top (FacetPriority empty)", name)
		}
	}
	// Clusters carry no k8s label tags — no projection.
	if cl := Lookup("clusters"); strings.Contains(cl.Query(fixtureScope(nil)), "tags:k8s.labels") {
		t.Errorf("clusters must not project label maps")
	}
}

func TestInjectPrimaryTagsIntoSummarize(t *testing.T) {
	keys := []string{"app", "env"}
	dql := Lookup("pods").Query(fixtureScope(nil))
	got := injectPrimaryTagsIntoSummarize(dql, keys)
	for _, want := range []string{
		"primary_tags.app = takeFirst(primary_tags.app)",
		"primary_tags.env = takeFirst(primary_tags.env)",
		"}, by:{id}",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summarize inject missing %q:\n%s", want, got)
		}
	}
	// keys must be inside the summarize, before the closing }
	sumClose := strings.Index(got, "}, by:{id}")
	appIdx := strings.Index(got, "primary_tags.app = takeFirst")
	if appIdx < 0 || appIdx >= sumClose {
		t.Errorf("primary_tags key not inside summarize body (appIdx=%d sumClose=%d)", appIdx, sumClose)
	}
	// empty keys = no-op
	if injectPrimaryTagsIntoSummarize(dql, nil) != dql {
		t.Error("empty keys should be no-op")
	}
	// special-char keys must be whole-path backticked on both sides of the
	// assignment (docs/dev/learnings.md §1.21) — a bare hyphen is invalid DQL.
	got = injectPrimaryTagsIntoSummarize(dql, []string{"cost-center"})
	if want := "`primary_tags.cost-center` = takeFirst(`primary_tags.cost-center`)"; !strings.Contains(got, want) {
		t.Errorf("special-char key not backticked, missing %q:\n%s", want, got)
	}
	// whitespace variants around the summarize close still match.
	spaced := strings.Replace(dql, "}, by:", "}  ,  by:", 1)
	if injectPrimaryTagsIntoSummarize(spaced, keys) == spaced {
		t.Error("whitespace-varied summarize close should still inject")
	}
	// no summarize close = no-op, not malformed DQL.
	if injectPrimaryTagsIntoSummarize("smartscapeNodes \"K8S_POD\"", keys) != "smartscapeNodes \"K8S_POD\"" {
		t.Error("missing summarize close should be a no-op")
	}
}

func TestInjectPrimaryTagsIntoFields(t *testing.T) {
	keys := []string{"app", "env"}
	for _, name := range []string{"workloads", "namespaces", "nodes", "clusters"} {
		spec := Lookup(name)
		dql := spec.Query(fixtureScope(nil))
		got := injectPrimaryTagsIntoFields(dql, keys)
		for _, want := range []string{"primary_tags.app", "primary_tags.env"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: fields inject missing %q:\n%s", name, want, got)
			}
		}
		// keys must appear on a | fields line, not a | fieldsAdd line
		for _, line := range strings.Split(got, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "| fields ") && strings.Contains(trimmed, "primary_tags.app") {
				goto found
			}
		}
		t.Errorf("%s: primary_tags.app not on | fields line", name)
	found:
	}
	// empty keys = no-op
	dql := Lookup("clusters").Query(fixtureScope(nil))
	if injectPrimaryTagsIntoFields(dql, nil) != dql {
		t.Error("empty keys should be no-op")
	}
	// special-char keys must be whole-path backticked (docs/dev/learnings.md §1.21).
	got := injectPrimaryTagsIntoFields(dql, []string{"cost-center"})
	if want := "`primary_tags.cost-center`"; !strings.Contains(got, want) {
		t.Errorf("special-char key not backticked, missing %q:\n%s", want, got)
	}
}

func TestPodReadyAndClasses(t *testing.T) {
	if got := podReady(map[string]any{"ready": "1", "total": "2"}); got != "1/2" {
		t.Errorf("podReady = %q", got)
	}
	// Kubernetes omits readyReplicas when zero are ready.
	if got := workloadReady(map[string]any{"desired": "3"}); got != "0/3" {
		t.Errorf("workloadReady = %q", got)
	}
	if classPodReady("1/2") != "warn" || classPodReady("2/2") != "ok" {
		t.Error("classPodReady thresholds wrong")
	}
	if classPodPhase("Failed") != "error" || classPodPhase("Running") != "ok" {
		t.Error("classPodPhase mapping wrong")
	}
}
