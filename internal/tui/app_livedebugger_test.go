package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
)

// TestBreakpointEnterOpensSnapshots: enter on a breakpoint lists its hits,
// the snapshot rows render decoded, and c echoes the dtctl read command.
func TestBreakpointEnterOpensSnapshots(t *testing.T) {
	a := testApp(t, "breakpoints")
	disablePreview(a)
	seedRows(t, a, []map[string]any{{
		"breakpoint.id":    "bp-0001",
		"code.filepath":    "org/example/OrderController.java",
		"code.line.number": float64(306),
		"code.function":    "checkout",
		"hits":             float64(12),
	}})
	if out := ansi.Strip(a.View()); !strings.Contains(out, "OrderController.java:306") {
		t.Fatalf("breakpoints view:\n%s", out)
	}
	press(a, key("enter"))
	tv, ok := a.top().(*tableView)
	if !ok || tv.spec.Name != "snapshots" || tv.scope.Arg != "bp-0001" {
		t.Fatalf("enter on a breakpoint → %s", a.top().Crumb())
	}
	if !strings.Contains(tv.dql, `breakpoint.id == toUid("bp-0001")`) {
		t.Errorf("snapshots query:\n%s", tv.dql)
	}
	if got := tv.Echo(); got != "dtctl get snapshots bp-0001 --decode-snapshots" {
		t.Errorf("echo = %q", got)
	}
	seedRows(t, a, []map[string]any{{
		"timestamp":     "2026-07-07T10:00:00Z",
		"code.filepath": "org/example/OrderController.java",
		"parsed_snapshot": map[string]any{"rookout": map[string]any{
			"frame":     map[string]any{"locals": map[string]any{"orderId": "o-1"}},
			"traceback": []any{map[string]any{"function": "checkout", "filename": "OrderController.java", "line": float64(306)}},
		}},
	}})
	if got := tv.all[0]["variables"]; got == nil {
		t.Fatalf("snapshot rows must be decoded: %v", tv.all[0])
	}
	if got := tv.all[0][catalog.SnapshotSummaryKey]; got != "1 locals · 1 frames" {
		t.Errorf("snapshot summary = %v", got)
	}
}

// TestSnapshotsBareEchoesQuery: without a breakpoint the list echoes its DQL.
func TestSnapshotsBareEchoesQuery(t *testing.T) {
	a := testApp(t, "snapshots")
	tv := a.top().(*tableView)
	if got := tv.Echo(); !strings.HasPrefix(got, "dtctl query 'fetch application.snapshots") {
		t.Errorf("echo = %q", got)
	}
}

// TestBreakpointEnterKeepsScope: a breakpoint's hits are the population its
// row counted — the entity scope and server narrowing carry into snapshots,
// and once narrowed, c echoes the composed query rather than get snapshots.
func TestBreakpointEnterKeepsScope(t *testing.T) {
	a := testApp(t, "breakpoints")
	bp := a.top().(*tableView)
	host := &catalog.Entity{ID: "HOST-0001", Type: "HOST", Name: "host-a"}
	bp.scope.Entity = host
	bp.facets = []catalog.Facet{{Field: "thread.name", Value: "worker-1"}}
	seedRows(t, a, []map[string]any{{"breakpoint.id": "bp-0001", "code.filepath": "A.java"}})
	press(a, key("enter"))
	tv, ok := a.top().(*tableView)
	if !ok || tv.spec.Name != "snapshots" || tv.scope.Arg != "bp-0001" {
		t.Fatalf("enter on a breakpoint → %s", a.top().Crumb())
	}
	if tv.scope.Entity == nil || tv.scope.Entity.ID != "HOST-0001" {
		t.Errorf("entity scope dropped: %+v", tv.scope.Entity)
	}
	if len(tv.facets) != 1 || tv.facets[0].Value != "worker-1" {
		t.Errorf("facets dropped: %+v", tv.facets)
	}
	if got := tv.Echo(); !strings.HasPrefix(got, "dtctl query 'fetch application.snapshots") {
		t.Errorf("narrowed echo = %q", got)
	}
}

// TestSnapshotsSearchFallsBackToQueryEcho: a server search on a breakpoint's
// snapshots makes get snapshots broader than the list — echo the query.
func TestSnapshotsSearchFallsBackToQueryEcho(t *testing.T) {
	a := testApp(t, "breakpoints")
	seedRows(t, a, []map[string]any{{"breakpoint.id": "bp-0001", "code.filepath": "A.java"}})
	press(a, key("enter"))
	tv := a.top().(*tableView)
	tv.searches = []string{"timeout"}
	if got := tv.Echo(); !strings.HasPrefix(got, "dtctl query ") {
		t.Errorf("searched echo = %q", got)
	}
}
