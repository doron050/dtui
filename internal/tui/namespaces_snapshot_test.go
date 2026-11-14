package tui

import "testing"

// Namespaces mirror the smartscapeNodes K8S_NAMESPACE projection — cluster,
// lifetime, labels — with the pods/running enrichment joined as Grail returns
// summarize counts: strings. The same name recurs across clusters.
func TestSnapshotNamespacesTable(t *testing.T) {
	pinSnapshotClock(t)

	ns := func(id, name, cluster, start, pods, running string) map[string]any {
		return map[string]any{
			"id": id, "name": name, "type": "K8S_NAMESPACE", "cluster": cluster,
			"lifetime": map[string]any{"start": start, "end": "2026-07-15T11:45:00.000000000Z"},
			"tags:k8s.labels": map[string]any{
				"istio.io/rev":                "default",
				"kubernetes.io/metadata.name": name,
			},
			"__enrich.pods": pods, "__enrich.running": running,
		}
	}
	rows := []map[string]any{
		ns("K8S_NAMESPACE-0000000000000E11", "checkout", "demo-eks-useast1", "2026-04-28T13:07:00.000000000Z", "30", "16"),
		ns("K8S_NAMESPACE-0000000000000E12", "checkout", "demo-aks-westeu", "2026-06-30T12:00:00.000000000Z", "12", "12"),
		ns("K8S_NAMESPACE-0000000000000E13", "dynatrace", "demo-eks-useast1", "2026-03-23T07:36:00.000000000Z", "110", "110"),
		ns("K8S_NAMESPACE-0000000000000E14", "kube-system", "demo-eks-useast1", "2025-04-25T20:30:00.000000000Z", "41", "41"),
		ns("K8S_NAMESPACE-0000000000000E15", "monitoring", "demo-aks-westeu", "2026-07-14T06:05:00.000000000Z", "3", "3"),
	}

	a := testApp(t, "namespaces")
	seedRows(t, a, rows)
	assertSnapshots(t, "namespaces", a)
}
