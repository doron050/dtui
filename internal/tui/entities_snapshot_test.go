package tui

import "testing"

// The entities census mirrors `smartscapeNodes "*" | summarize count()`:
// raw type names, string counts, count desc — long Azure type names included.
func TestSnapshotEntitiesTable(t *testing.T) {
	pinSnapshotClock(t)
	a := testApp(t, "entities")
	seedRows(t, a, navCensus())
	assertSnapshots(t, "entities", a)
}
