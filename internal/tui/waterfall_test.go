package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
)

func TestBuildWaterfallTreeAndOrphans(t *testing.T) {
	rows := buildWaterfall([]map[string]any{
		{"span.id": "root", "span.name": "GET /checkout", "span.kind": "server",
			"start_time": "2026-07-06T16:00:00.000000000Z", "end_time": "2026-07-06T16:00:00.341000000Z",
			"request.is_failed": false, "service.name": "checkout"},
		{"span.id": "child", "span.parent_id": "root", "span.name": "SELECT",
			"start_time": "2026-07-06T16:00:00.100000000Z", "end_time": "2026-07-06T16:00:00.270000000Z",
			"request.is_failed": true, "service.name": "payments"},
		{"span.id": "orphan", "span.parent_id": "missing", "span.name": "async",
			"start_time": "2026-07-06T16:00:00.200000000Z", "end_time": "2026-07-06T16:00:00.210000000Z"},
	})
	if len(rows) != 3 {
		t.Fatalf("rows = %d", len(rows))
	}
	if rows[0].guide != "" || rows[1].guide != "└─ " || rows[1].label != "SELECT" || !rows[1].failed {
		t.Errorf("tree wrong: %+v", rows[:2])
	}
	if rows[2].guide != "" {
		t.Errorf("orphan (parent outside window) must render as root, guide = %q", rows[2].guide)
	}
}

// TestWaterfallAnchorsToOriginSpan: entering a trace from a specific span row
// selects that span instead of leaving the cursor at the trace root.
func TestWaterfallAnchorsToOriginSpan(t *testing.T) {
	a := testApp(t, "traces")
	seedRows(t, a, []map[string]any{{
		"trace.id": "0000000000000000000000000000a002", "span.id": "bbbbbbbbbbbbbbbb",
		"span.name": "child op", "span.kind": "internal",
		"start_time": "2026-07-06T16:18:52.100000000Z", "end_time": "2026-07-06T16:18:52.200000000Z",
	}})
	press(a, key("enter"))
	wf, ok := a.top().(*waterfallView)
	if !ok {
		t.Fatalf("enter on a span should open the waterfall, top = %T", a.top())
	}
	if wf.focusSpan != "bbbbbbbbbbbbbbbb" {
		t.Fatalf("focus span = %q, want the originating span", wf.focusSpan)
	}
	wf.Update(dataMsg{owner: wf, seq: wf.seq, records: []map[string]any{
		{"span.id": "aaaaaaaaaaaaaaaa", "span.name": "GET /", "span.kind": "server",
			"start_time": "2026-07-06T16:18:52.000000000Z", "end_time": "2026-07-06T16:18:53.000000000Z"},
		{"span.id": "bbbbbbbbbbbbbbbb", "span.parent_id": "aaaaaaaaaaaaaaaa",
			"span.name": "child op", "span.kind": "internal",
			"start_time": "2026-07-06T16:18:52.100000000Z", "end_time": "2026-07-06T16:18:52.200000000Z"},
	}})
	if wf.cursor != 1 {
		t.Errorf("cursor = %d, want anchored on the originating span", wf.cursor)
	}
	if wf.focusSpan != "" {
		t.Error("the anchor must be consumed so refreshes keep the user's cursor")
	}
}

// Drilling into a trace from a span row must scope the fetch to a tight window
// around that span, NOT the whole global timeframe — the trace.id filter
// narrows the result but not the scan, so an unscoped fetch hits Grail's scan
// limit on a large tenant (validated live on a large tenant).
func TestWaterfallScopesFetchToAnchorWindow(t *testing.T) {
	a := testApp(t, "traces")
	seedRows(t, a, []map[string]any{{
		"trace.id": "0000000000000000000000000000a002", "span.id": "bbbbbbbbbbbbbbbb",
		"span.name": "child op", "span.kind": "internal",
		"start_time": "2026-07-06T16:18:52.100000000Z", "end_time": "2026-07-06T16:18:52.200000000Z",
	}})
	press(a, key("enter"))
	wf := a.top().(*waterfallView)

	if wf.anchor.IsZero() {
		t.Fatal("the originating span's start_time must anchor the fetch window")
	}
	if !wf.tf.Absolute() {
		t.Fatalf("anchored window must be absolute, got %+v", wf.tf)
	}
	if wf.tf.Dur != 2*waterfallPad {
		t.Errorf("window span = %s, want %s", wf.tf.Dur, 2*waterfallPad)
	}
	want := time.Date(2026, 7, 6, 16, 18, 52, 100000000, time.UTC)
	if !wf.tf.From.Equal(want.Add(-waterfallPad)) || !wf.tf.To.Equal(want.Add(waterfallPad)) {
		t.Errorf("window = [%s, %s], want centered on %s", wf.tf.From, wf.tf.To, want)
	}
	// The composed DQL pins both bounds and never scans from now().
	if !strings.Contains(wf.dql, "toTimestamp(") || strings.Contains(wf.dql, "now() -") {
		t.Errorf("waterfall DQL must use absolute bounds, got:\n%s", wf.dql)
	}
}

func anchoredWaterfall(t *testing.T) *waterfallView {
	t.Helper()
	a := testApp(t, "traces")
	at := time.Date(2026, 7, 6, 16, 18, 52, 0, time.UTC)
	wf := newWaterfallView(a.ds, "0000000000000000000000000000a002", "", catalog.DefaultTimeframe, at)
	deliverView(wf, wf.Init()) // sets seq + dql, result dropped
	return wf
}

// An empty anchored result (approximate anchor, or a trace straddling the
// window edge) widens ONCE — but still bounded around the anchor, never a
// blind fetch to now() that would re-trigger the scan limit.
func TestWaterfallWidensAroundAnchorOnEmpty(t *testing.T) {
	wf := anchoredWaterfall(t)
	wf.Update(dataMsg{owner: wf, seq: wf.seq, records: nil})
	if !wf.widened {
		t.Fatal("empty anchored result should widen once")
	}
	if !wf.tf.Absolute() || wf.tf.Dur != 2*waterfallWidePad {
		t.Errorf("widened window = %+v, want absolute span %s", wf.tf, 2*waterfallWidePad)
	}
}

// An empty result that came WITH a scan-limit notice is "the scan gave up",
// not "the trace isn't here" — widening would scan more and fail again, so it
// must be left alone (the app surfaces the notice instead).
func TestWaterfallDoesNotWidenOnScanLimit(t *testing.T) {
	wf := anchoredWaterfall(t)
	before := wf.tf
	wf.Update(dataMsg{owner: wf, seq: wf.seq, records: nil, notices: scanLimitNotices()})
	if wf.widened {
		t.Error("a scan-limit empty must not trigger a wider (heavier) retry")
	}
	if wf.tf != before {
		t.Errorf("window changed to %+v, want the tight window %+v", wf.tf, before)
	}
}

// The manual `trace <id>` path has no anchor; it keeps the legacy behavior of
// widening to a relative 24h window when the active window came back empty.
func TestWaterfallUnanchoredWidensToRelative24h(t *testing.T) {
	a := testApp(t, "traces")
	wf := newWaterfallView(a.ds, "0000000000000000000000000000a002", "", catalog.DefaultTimeframe, time.Time{})
	deliverView(wf, wf.Init())
	if wf.tf.Absolute() {
		t.Fatal("unanchored waterfall must inherit the relative global window")
	}
	wf.Update(dataMsg{owner: wf, seq: wf.seq, records: nil})
	if !wf.widened || wf.tf.Absolute() || wf.tf.Dur != 24*time.Hour {
		t.Errorf("unanchored empty should widen to relative 24h, got %+v", wf.tf)
	}
}
