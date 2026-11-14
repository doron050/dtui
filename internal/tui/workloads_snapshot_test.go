package tui

import "testing"

// k8sLabels mirrors the `tags:k8s.labels` object the K8s list queries project:
// the app.kubernetes.io/* recommended labels plus a chart label.
func k8sLabels(app, version string) map[string]any {
	return map[string]any{
		"app.kubernetes.io/component": app,
		"app.kubernetes.io/instance":  app,
		"app.kubernetes.io/name":      app,
		"app.kubernetes.io/part-of":   "storefront",
		"app.kubernetes.io/version":   version,
		"helm.sh/chart":               app + "-" + version,
	}
}

// cpuSeries is a 2h/5m page-level CPU enrichment series in the live shape:
// 25 buckets, the newest one still null.
func cpuSeries(base, spike float64) []any {
	s := make([]any, 25)
	wobble := []float64{0, 0.12, 0.08, -0.04, -0.02, 0.15, -0.01, 0.1, 0.11, -0.05, -0.08, -0.06}
	for i := range 24 {
		s[i] = base * (1 + wobble[i%len(wobble)])
	}
	s[23] = spike
	return s
}

// Workloads mirror the smartscapeNodes K8S_DEPLOYMENT/STATEFULSET/DAEMONSET
// projection: string ready/desired counts, kind, creation timestamp, labels,
// and __enrich.cpu (absent for workloads the timeseries returned no series
// for — the live result had such rows).
func TestSnapshotWorkloadsTable(t *testing.T) {
	pinSnapshotClock(t)

	wl := func(id, name, ns, kind, ready, desired, created string, cpu []any) map[string]any {
		rec := map[string]any{
			"id": id, "name": name, "type": "K8S_" + map[string]string{"deployment": "DEPLOYMENT", "statefulset": "STATEFULSET", "daemonset": "DAEMONSET"}[kind],
			"namespace": ns, "kind": kind, "ready": ready, "desired": desired, "created": created,
			"tags:k8s.labels": k8sLabels(name, "2.14.1"),
		}
		if cpu != nil {
			rec["__enrich.cpu"] = cpu
		}
		return rec
	}
	rows := []map[string]any{
		wl("K8S_DEPLOYMENT-0000000000000E01", "cart-api", "checkout", "deployment", "2", "2", "2026-04-30T16:06:03.000000000Z", cpuSeries(330, 783)),
		wl("K8S_DEPLOYMENT-0000000000000E02", "payment-service", "checkout", "deployment", "1", "2", "2026-07-15T09:51:02.000000000Z", cpuSeries(41, 96)),
		wl("K8S_STATEFULSET-0000000000000E03", "orders-postgres", "checkout", "statefulset", "1", "1", "2026-06-20T08:09:20.000000000Z", cpuSeries(12, 13)),
		wl("K8S_DAEMONSET-0000000000000E04", "dynatrace-oneagent", "dynatrace", "daemonset", "6", "6", "2026-03-23T07:36:00.000000000Z", cpuSeries(210, 230)),
		wl("K8S_DEPLOYMENT-0000000000000E05", "dynatrace-operator", "dynatrace", "deployment", "1", "1", "2026-03-23T07:35:12.000000000Z", nil),
		wl("K8S_DAEMONSET-0000000000000E06", "kube-proxy", "kube-system", "daemonset", "6", "6", "2025-05-08T13:41:00.000000000Z", nil),
	}

	a := testApp(t, "workloads")
	seedRows(t, a, rows)
	assertSnapshots(t, "workloads", a)
}
