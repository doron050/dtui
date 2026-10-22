package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
)

// logReaderView builds a HOST-scoped logs table (no scope-widening hop) seeded
// with two records — the first carries an embedded newline so the reader has a
// multi-line body to wrap.
func logReaderView(t *testing.T) *tableView {
	t.Helper()
	a := testApp(t, "home")
	tv := newTableView(a.ds, catalog.Lookup("logs"),
		catalog.Scope{Timeframe: a.tf, Entity: &catalog.Entity{ID: "HOST-1", Name: "web-01", Type: "HOST"}})
	tv.seq++
	tv.Update(dataMsg{owner: tv, seq: tv.seq, elapsed: time.Second, records: []map[string]any{
		{"timestamp": "2026-07-16T10:00:00Z", "loglevel": "ERROR", "host.name": "webhost",
			"content": "boom happened\njava.lang.NullPointerException at some.very.long.package.name.that.keeps.going.and.should.wrap.Across.The.Reader.Width"},
		{"timestamp": "2026-07-16T09:59:00Z", "loglevel": "INFO", "host.name": "webhost", "content": "second line"},
	}})
	return tv
}

func TestReaderTogglesAndRendersRawContent(t *testing.T) {
	tv := logReaderView(t)

	// The grid flattens content to one line; entering the reader ('w') wraps it.
	if cmd := tv.handleKey(key("w")); cmd == nil {
		t.Fatal("w should return a status command")
	}
	if !tv.reader {
		t.Fatal("w did not enter reader mode")
	}
	body := ansi.Strip(tv.View(80, 24))
	// The reader shows content only, wrapped; the embedded newline keeps the
	// stack frame on its own line.
	for _, want := range []string{"raw", "tail", "boom happened", "NullPointerException", "second line"} {
		if !strings.Contains(body, want) {
			t.Errorf("reader view missing %q:\n%s", want, body)
		}
	}
	// Per-record metadata is NOT drawn as text — it reads like a file. Source
	// text and level text never appear (level survives only as color, which
	// ansi.Strip removes). Metadata stays one enter away, in the inspector.
	if strings.Contains(body, "webhost") || strings.Contains(body, "ERROR") {
		t.Errorf("raw reader must not print per-line metadata:\n%s", body)
	}

	// 'w' again returns to the grid.
	tv.handleKey(key("w"))
	if tv.reader {
		t.Fatal("second w should leave reader mode")
	}
}

func TestReaderHeadTailAndLoadMore(t *testing.T) {
	tv := logReaderView(t)
	tv.handleKey(key("w"))

	// Head/tail flips the DQL sort direction (a re-fetch).
	tv.handleKey(key("T"))
	if !tv.scope.LogHead {
		t.Fatal("T did not flip to head order")
	}
	if q := tv.composeDQL(); !strings.Contains(q, "| sort timestamp asc") {
		t.Errorf("head query should sort asc:\n%s", q)
	}
	tv.handleKey(key("T"))
	if tv.scope.LogHead {
		t.Fatal("second T should return to tail")
	}

	// Load-more raises the cap in steps.
	tv.handleKey(key("+"))
	if tv.scope.Limit != 1000 {
		t.Fatalf("first + should raise the cap to 1000, got %d", tv.scope.Limit)
	}
	if q := tv.composeDQL(); !strings.Contains(q, "| limit 1000") {
		t.Errorf("raised cap missing from query:\n%s", q)
	}
	tv.handleKey(key("+"))
	if tv.scope.Limit != 3000 {
		t.Fatalf("second + should raise the cap to 3000, got %d", tv.scope.Limit)
	}
}

func TestReaderRefusedOnNonReadableView(t *testing.T) {
	a := testApp(t, "home")
	tv := newTableView(a.ds, catalog.Lookup("problems"), catalog.Scope{Timeframe: a.tf})
	tv.handleKey(key("w"))
	if tv.reader {
		t.Error("problems view is not RawReadable — w must not enter the reader")
	}
}
