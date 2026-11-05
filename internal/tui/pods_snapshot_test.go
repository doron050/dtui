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
	}
	// Shape the rows like real Grail output: node name + the enrichment CPU
	// series the table fetches page-level (SparkColumn reads __enrich.cpu).
	cpus := [][]any{
		{120.0, 130.0, 125.0, 140.0, 135.0, 150.0, 145.0, 160.0},
		{80.0, 85.0, 90.0, 88.0, 92.0, 95.0, 91.0, 93.0},
		{nil, nil, nil, nil, nil, nil, nil, nil},
		{400.0, 420.0, 380.0, 410.0, 430.0, 390.0, 415.0, 405.0},
	}
	for i, r := range rows {
		r["node"] = fmt.Sprintf("ip-10-0-1-%d", i+1)
		r["__enrich.cpu"] = cpus[i]
	}

	a := testApp(t, "pods")
	seedRows(t, a, rows)
	assertSnapshots(t, "pods", a)

	// The preview toggle is orthogonal to size: pin the bare-table render too.
	a = testApp(t, "pods")
	disablePreview(a)
	seedRows(t, a, rows)
	assertSnapshot(t, "pods-120x40-nopeek", screen(a))
}
