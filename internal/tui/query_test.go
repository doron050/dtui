package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
)

func TestCtrlQRevealsQuery(t *testing.T) {
	a := testApp(t, "hosts")
	press(a, tea.KeyMsg{Type: tea.KeyCtrlQ})
	qv, ok := a.top().(*queryView)
	if !ok {
		t.Fatalf("ctrl+q should open query view, top = %T", a.top())
	}
	if !strings.Contains(qv.editor.Value(), `smartscapeNodes "HOST"`) {
		t.Errorf("editor not pre-filled: %q", qv.editor.Value())
	}
}

// Submitted queries persist as history and ctrl+p / ctrl+n cycle them in the
// editor (tui.md: the escape hatch carries "query history").
func TestQueryHistoryCyclesInEditor(t *testing.T) {
	a := testApp(t, "hosts")
	press(a, key(":"))
	press(a, key("query"))
	press(a, key("enter"))
	qv := a.top().(*queryView)

	run := func(dql string) {
		press(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(dql)})
		press(a, key("enter"))
		press(a, tea.KeyMsg{Type: tea.KeyTab}) // back to the editor
		qv.editor.SetValue("")
	}
	run("fetch logs")
	run("fetch spans")
	if len(a.qhist.queries) != 2 || a.qhist.queries[0] != "fetch spans" {
		t.Fatalf("history = %v", a.qhist.queries)
	}

	press(a, tea.KeyMsg{Type: tea.KeyCtrlP})
	if got := qv.editor.Value(); got != "fetch spans" {
		t.Fatalf("ctrl+p should recall the last query, editor = %q", got)
	}
	press(a, tea.KeyMsg{Type: tea.KeyCtrlP})
	if got := qv.editor.Value(); got != "fetch logs" {
		t.Fatalf("second ctrl+p should step further back, editor = %q", got)
	}
	press(a, tea.KeyMsg{Type: tea.KeyCtrlN})
	press(a, tea.KeyMsg{Type: tea.KeyCtrlN})
	if got := qv.editor.Value(); got != "" {
		t.Fatalf("ctrl+n past the newest entry should restore the draft, editor = %q", got)
	}

	// Re-running an old query moves it to the front instead of duplicating.
	a.qhist.add("fetch logs")
	if len(a.qhist.queries) != 2 || a.qhist.queries[0] != "fetch logs" {
		t.Fatalf("MRU dedup failed: %v", a.qhist.queries)
	}
}
func TestDeriveColumnsPrefersIdentityAndSkipsEmpties(t *testing.T) {
	cols := deriveColumns([]map[string]any{
		{"timestamp": "2026-07-06T16:00:00Z", "content": "x", "name": "", "level": "INFO"},
		{"timestamp": "2026-07-06T16:00:01Z", "content": "y", "name": "", "level": "WARN"},
	})
	if len(cols) == 0 || cols[0].Title != "TIMESTAMP" {
		t.Fatalf("first column = %+v", cols)
	}
	for _, c := range cols {
		if c.Field == "name" {
			t.Error("all-empty field must not become a column")
		}
	}
}

// ctrl+p stops at the oldest entry, ctrl+n walks back to the stashed draft
// and stops there.
func TestQueryHistoryBoundsAndDraft(t *testing.T) {
	a := testApp(t, "hosts")
	press(a, key(":"))
	press(a, key("query"))
	press(a, key("enter"))
	qv := a.top().(*queryView)
	a.qhist.add("fetch logs")
	a.qhist.add("fetch spans")
	qv.editor.SetValue("fetch ev")

	for range 3 {
		press(a, tea.KeyMsg{Type: tea.KeyCtrlP})
	}
	if got := qv.editor.Value(); got != "fetch logs" {
		t.Fatalf("ctrl+p past the oldest entry should stay on it, editor = %q", got)
	}
	press(a, tea.KeyMsg{Type: tea.KeyCtrlN})
	if got := qv.editor.Value(); got != "fetch spans" {
		t.Fatalf("ctrl+n should step forward, editor = %q", got)
	}
	press(a, tea.KeyMsg{Type: tea.KeyCtrlN})
	press(a, tea.KeyMsg{Type: tea.KeyCtrlN})
	if got := qv.editor.Value(); got != "fetch ev" {
		t.Fatalf("ctrl+n should end on the stashed draft, editor = %q", got)
	}
}

// A query page restored from history comes back with results; a fresh one
// waits for enter.
func TestQueryViewInitRunsOnlyARestoredQuery(t *testing.T) {
	a := testApp(t, "hosts")
	calls := countQueries(a)
	fresh := newQueryView(a.ds, "fetch logs", catalog.Timeframes[0], nil)
	deliver(a, fresh.Init())
	if len(*calls) != 0 {
		t.Fatalf("a fresh query page must not run anything, ran %v", *calls)
	}
	restored := newQueryView(a.ds, "fetch logs", catalog.Timeframes[0], nil)
	restored.current = "fetch logs"
	deliver(a, restored.Init())
	if len(*calls) != 1 || !strings.Contains((*calls)[0], "fetch logs") {
		t.Fatalf("a restored query page should rerun its query once, ran %v", *calls)
	}
}

func TestDeriveColumnsRanksCapsAndFlexes(t *testing.T) {
	var recs []map[string]any
	for i := range 3 {
		r := map[string]any{"name": "svc", "zeta": "z"}
		if i < 2 {
			r["alpha"], r["beta"] = "a", "b"
		}
		r[fmt.Sprintf("c%d", i)] = "c"
		r[fmt.Sprintf("c%d", i+3)] = "c"
		recs = append(recs, r)
	}
	var titles []string
	cols := deriveColumns(recs)
	for _, c := range cols {
		titles = append(titles, c.Title)
	}
	// Lead field first, then by prevalence, ties alphabetical, capped at 8.
	if got, want := strings.Join(titles, ","), "NAME,ZETA,ALPHA,BETA,C0,C1,C2,C3"; got != want {
		t.Fatalf("columns = %s, want %s", got, want)
	}
	if cols[0].Width != 14 || cols[7].Width != 0 {
		t.Fatalf("widths = %d…%d, want 14…0 (last column flexes when none does)", cols[0].Width, cols[7].Width)
	}

	long := strings.Repeat("x", 24)
	cols = deriveColumns([]map[string]any{{long: "v", "service.namespace": "n"}})
	widths := map[string]int{}
	for _, c := range cols {
		widths[c.Field] = c.Width
	}
	if widths[long] != 0 || widths["service.namespace"] != len("service.namespace")+2 {
		t.Fatalf("widths = %v: a 24+ char key flexes, shorter keys get len+2", widths)
	}
}
