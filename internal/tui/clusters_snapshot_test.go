package tui

import "testing"

// Clusters mirror the smartscapeNodes K8S_CLUSTER projection: distribution
// (EKS/AKS/GKE), the provider's full version string, and lifetime.
func TestSnapshotClustersTable(t *testing.T) {
	pinSnapshotClock(t)

	cl := func(id, name, dist, version, start string) map[string]any {
		return map[string]any{
			"id": id, "name": name, "type": "K8S_CLUSTER", "distribution": dist, "version": version,
			"lifetime": map[string]any{"start": start, "end": "2026-07-15T11:47:00.000000000Z"},
		}
	}
	rows := []map[string]any{
		cl("K8S_CLUSTER-0000000000000E31", "demo-aks-westeu", "AKS", "v1.32.6", "2025-05-08T13:41:00.000000000Z"),
		cl("K8S_CLUSTER-0000000000000E32", "demo-eks-useast1", "EKS", "v1.33.5-eks-113cf36", "2026-04-28T13:31:00.000000000Z"),
		cl("K8S_CLUSTER-0000000000000E33", "demo-eks-uswest2", "EKS", "v1.33.5-eks-113cf36", "2026-04-27T08:11:00.000000000Z"),
		cl("K8S_CLUSTER-0000000000000E34", "demo-gke-europe-west3", "GKE", "v1.33.4-gke.1245000", "2026-06-30T09:53:00.000000000Z"),
	}

	a := testApp(t, "clusters")
	seedRows(t, a, rows)
	assertSnapshots(t, "clusters", a)
}
