package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
)

// Invariants from docs/design/vision.md and the ADRs, checked across every
// catalog view so a new Spec is covered without writing a new test.

// countQueries swaps the executor for one that records every DQL it is asked
// to run, so a test can measure what one navigation action costs.
func countQueries(a *app) *[]string {
	var calls []string
	a.ds.runFn = func(dql string) ([]map[string]any, error) {
		calls = append(calls, dql)
		return nil, nil
	}
	return &calls
}

func openView(a *app, name string) {
	_, cmd := a.Update(NavigateMsg{Target: name})
	deliver(a, cmd)
}

// Vision "the boundary": every screen has two exits — `o` deep-links into the
// web UI and `c` echoes the equivalent dtctl command. A view that can't do
// both is incomplete.
func TestEveryViewHasBothExits(t *testing.T) {
	for _, spec := range catalog.All() {
		t.Run(spec.Name, func(t *testing.T) {
			a := testApp(t, "home")
			a.opts.Environment = "https://env.example.invalid"
			openView(a, spec.Name)

			tv, ok := a.top().(*tableView)
			if !ok || tv.spec.Name != spec.Name {
				t.Fatalf("NavigateMsg{%q} opened %T", spec.Name, a.top())
			}
			echo := tv.Echo()
			if !strings.HasPrefix(echo, "dtctl ") {
				t.Errorf("c echoes %q, want a dtctl command", echo)
			}
			if spec.API != "" {
				// API views have no DQL to open as a notebook; their web exit
				// is the selected record's own app link.
				return
			}
			if !strings.HasPrefix(echo, "dtctl query '") {
				t.Errorf("DQL view echo %q is not `dtctl query`", echo)
			}
			if len(linkOptionsFor(a.opts.Environment, nil, nil, "", tv.DQL())) == 0 {
				t.Error("o has no web target for the view's query")
			}
		})
	}
}

// ADR-0013: opening a view costs one primary scoped query. Session-cached
// metadata (ADR-0010) is exempt and must not be re-paid on the next visit.
func TestOpeningAViewFitsQueryBudget(t *testing.T) {
	for _, spec := range catalog.All() {
		t.Run(spec.Name, func(t *testing.T) {
			a := testApp(t, "home")
			calls := countQueries(a)
			openView(a, spec.Name)
			tv := a.top().(*tableView)

			if spec.API != "" {
				if len(*calls) != 0 {
					t.Fatalf("API view ran %d DQL queries, want 0:\n%s", len(*calls), strings.Join(*calls, "\n---\n"))
				}
				return
			}
			primary := 0
			for _, q := range *calls {
				if q == tv.DQL() {
					primary++
				}
			}
			if primary != 1 || len(*calls) > 2 {
				t.Fatalf("first open ran %d queries (%d primary), want 1 primary + at most 1 metadata:\n%s",
					len(*calls), primary, strings.Join(*calls, "\n---\n"))
			}

			// Second visit: metadata is cached, only the primary query runs.
			// Known gap: a metric roster's DiscoverQuery is cached on the
			// tableView, not the session, so a fresh visit re-pays it. When
			// that cache moves to the session this allowance must go.
			want := 1
			if spec.DiscoverQuery != nil {
				want = 2
			}
			press(a, key("esc"))
			*calls = nil
			openView(a, spec.Name)
			if got := *calls; len(got) != want || got[len(got)-1] != a.top().(*tableView).DQL() {
				t.Fatalf("second open ran %d queries, want %d ending in the primary:\n%s", len(got), want, strings.Join(got, "\n---\n"))
			}
		})
	}
}

// ADR-0013: backtracking is free — esc returns to the previous view with its
// rows intact and issues no query.
func TestBacktrackingIsFree(t *testing.T) {
	a := testApp(t, "problems")
	seedRows(t, a, []map[string]any{problemRow()})
	press(a, key("l"))
	if tv, ok := a.top().(*tableView); !ok || tv.spec.Name != "logs" {
		t.Fatalf("after l: top = %v", a.top().Crumb())
	}

	calls := countQueries(a)
	press(a, key("esc"))
	if len(*calls) != 0 {
		t.Fatalf("esc ran %d queries, want 0:\n%s", len(*calls), strings.Join(*calls, "\n---\n"))
	}
	if tv, ok := a.top().(*tableView); !ok || tv.spec.Name != "problems" || len(tv.rows) != 1 {
		t.Fatal("esc did not restore problems with its rows")
	}
}

// ADR-0013: sparklines for a whole page arrive in one batched enrichment
// query — each entity once, capped at enrichCap — never one query per row.
func TestEnrichmentIsOneBatchedQuery(t *testing.T) {
	a := testApp(t, "hosts")
	tv := a.top().(*tableView)
	if tv.spec.Enrich == nil {
		t.Fatal("fixture assumption: hosts has sparkline enrichment")
	}
	var rows []map[string]any
	for i := range enrichCap + 5 {
		r := hostRow()
		r["id"] = fmt.Sprintf("HOST-%016d", i)
		rows = append(rows, r, r) // every entity twice
	}
	seedRows(t, a, rows)

	calls := countQueries(a)
	deliver(a, tv.enrich())
	if len(*calls) != 1 {
		t.Fatalf("enrichment ran %d queries, want 1", len(*calls))
	}
	q := (*calls)[0]
	for i := range enrichCap + 5 {
		id := fmt.Sprintf("%q", fmt.Sprintf("HOST-%016d", i))
		want := 1
		if i >= enrichCap {
			want = 0
		}
		if got := strings.Count(q, id); got != want {
			t.Fatalf("%s appears %d times in the batch, want %d:\n%s", id, got, want, q)
		}
	}

	tv.all = nil
	if tv.enrich() != nil {
		t.Error("an empty page must not issue an enrichment query")
	}
}

// ADR-0010/0013: the smartscape tag census is fetched once per scoped query
// and reused until the scope changes.
func TestTagCensusCachedPerScope(t *testing.T) {
	a := testApp(t, "hosts")
	tv := a.top().(*tableView)
	if tv.tagDiscoverCmd() == nil {
		t.Fatal("first facet-picker open should fetch the tag census")
	}
	tv.tagContexts = map[string][]string{}
	if tv.tagDiscoverCmd() != nil {
		t.Fatal("census for the current scope must be reused")
	}
	tv.tagDiscoverKey = "census of an earlier scope"
	if tv.tagDiscoverCmd() == nil {
		t.Fatal("a census from another scope must be refetched")
	}

	slos := testApp(t, "slos").top().(*tableView)
	if slos.tagDiscoverCmd() != nil {
		t.Error("API-backed views have no smartscape census")
	}
}
