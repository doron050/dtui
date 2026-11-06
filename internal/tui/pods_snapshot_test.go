package tui

import (
	"fmt"
	"testing"
)

func TestSnapshotPodsTable(t *testing.T) {
	pinSnapshotClock(t)

	rows := []map[string]any{
		podRow("checkout-1", "shop", "Running", 0),
		podRow("checkout-2", "shop", "Running", 2),
		podRow("payments-1", "shop", "Pending", 0),
		podRow("inventory-1", "back", "CrashLoopBackOff", 31),
		podRow("report-job-29173", "back", "Failed", 0),
	}
	// Shape the rows like real Grail output: node, owning workload + kind,
	// string ready/total counts (a Failed pod reads 0/2), the pod's
	// `tags:k8s.labels`, and the enrichment CPU series the table fetches
	// page-level (SparkColumn reads __enrich.cpu).
	cpus := [][]any{
		{120.0, 130.0, 125.0, 140.0, 135.0, 150.0, 145.0, 160.0},
		{80.0, 85.0, 90.0, 88.0, 92.0, 95.0, 91.0, 93.0},
		{nil, nil, nil, nil, nil, nil, nil, nil},
		{400.0, 420.0, 380.0, 410.0, 430.0, 390.0, 415.0, 405.0},
		{3.1, 3.4, 3.0, 2.9, 2.6, 3.2, 8.7, nil},
	}
	workloads := []string{"checkout", "checkout", "payments", "inventory", "report-job"}
	kinds := []string{"deployment", "deployment", "deployment", "statefulset", "job"}
	for i, r := range rows {
		r["node"] = fmt.Sprintf("ip-10-0-1-%d", i+1)
		r["workload"], r["kind"] = workloads[i], kinds[i]
		r["tags:k8s.labels"] = map[string]any{
			"app.kubernetes.io/name": workloads[i], "app.kubernetes.io/part-of": "shop",
			"topology.kubernetes.io/zone": "us-east-1a",
		}
		r["__enrich.cpu"] = cpus[i]
	}
	rows[4]["ready"], rows[4]["total"] = "0", "2"

	a := testApp(t, "pods")
	seedRows(t, a, rows)
	assertSnapshots(t, "pods", a)

	// The preview toggle is orthogonal to size: pin the bare-table render too.
	a = testApp(t, "pods")
	disablePreview(a)
	seedRows(t, a, rows)
	assertSnapshot(t, "pods-120x40-nopeek", screen(a))
}
