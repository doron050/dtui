package tui

import "testing"

// Nodes mirror the smartscapeNodes K8S_NODE projection from an AKS cluster:
// string cpus, kubelet version and OS image dug from k8s.object, instance
// type and zone lifted from `tags:k8s.labels`, lifetime, and the running-pods
// enrichment count (a string, like every Grail summarize count).
func TestSnapshotNodesTable(t *testing.T) {
	pinSnapshotClock(t)

	node := func(id, name, cluster, start, pods string) map[string]any {
		return map[string]any{
			"id": id, "name": name, "type": "K8S_NODE", "cluster": cluster,
			"cpus": "4", "instance": "Standard_D4ds_v5", "zone": "westeurope-1",
			"version": "v1.32.6", "os": "Ubuntu 22.04.5 LTS",
			"lifetime": map[string]any{"start": start, "end": "2026-07-15T11:45:00.000000000Z"},
			"tags:k8s.labels": map[string]any{
				"agentpool":                        "system",
				"kubernetes.io/arch":               "amd64",
				"kubernetes.io/hostname":           name,
				"kubernetes.io/os":                 "linux",
				"node.kubernetes.io/instance-type": "Standard_D4ds_v5",
				"topology.kubernetes.io/region":    "westeurope",
				"topology.kubernetes.io/zone":      "westeurope-1",
			},
			"__enrich.pods": pods,
		}
	}
	rows := []map[string]any{
		node("K8S_NODE-0000000000000E21", "aks-system-31337-vmss000004", "demo-aks-westeu", "2026-07-13T10:16:00.000000000Z", "11"),
		node("K8S_NODE-0000000000000E22", "aks-system-31337-vmss000005", "demo-aks-westeu", "2026-07-12T16:46:00.000000000Z", "11"),
		node("K8S_NODE-0000000000000E23", "aks-user-27182-vmss00000a", "demo-aks-westeu", "2026-07-13T15:44:00.000000000Z", "23"),
		node("K8S_NODE-0000000000000E24", "aks-user-27182-vmss00000b", "demo-aks-westeu", "2026-06-29T12:33:00.000000000Z", "19"),
	}

	a := testApp(t, "nodes")
	seedRows(t, a, rows)
	assertSnapshots(t, "nodes", a)
}
