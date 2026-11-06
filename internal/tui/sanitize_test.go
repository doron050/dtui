package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
	"github.com/dynatrace-oss/dtui/internal/tui/theme"
)

// hostile carries an OSC 52 clipboard write and a clear-screen CSI — the
// shapes telemetry text must never smuggle onto the operator's terminal.
const hostile = "evil\x1b]52;c;ZWNobyBoaQ==\x07\x1b[2Jtail"

// assertInert fails when s still carries a record-borne control sequence.
func assertInert(t *testing.T, where, s string) {
	t.Helper()
	for _, bad := range []string{"\x1b]", "\x1b[2J", "\x07"} {
		if strings.Contains(s, bad) {
			t.Errorf("%s leaks %q:\n%q", where, bad, s)
		}
	}
}

func TestSanitizeFrame(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })

	styled := theme.Error.Render("boom") + "\n" + theme.Dim.Render("ok")
	if got := sanitizeFrame(styled); got != styled {
		t.Errorf("SGR styling must pass through:\n got %q\nwant %q", got, styled)
	}
	for in, want := range map[string]string{
		hostile:                "eviltail",
		"a\x1b]0;title\x1b\\b": "ab",
		"a\x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\": "alink",
		"a\x1bPq#0\x1b\\b":                          "ab",
		"a\x1bcb\x1b7c\x1b(Bd":                      "abcd",
		"a\x1b[1;1Hb\x1b[?1049lc\x1b[38;5;1md":      "abc\x1b[38;5;1md",
		"a\u009b2Jb\u009d0;t\x07c":                  "a2Jb0;tc",
		"a\rb\x00c\x7fd\te":                         "abcd e",
		"a\x1b]52;c;Zm9v\nb":                        "a\nb",
		"bad\xffbyte":                               "bad\ufffdbyte",
		"trailing\x1b":                              "trailing",
	} {
		if got := sanitizeFrame(in); got != want {
			t.Errorf("sanitizeFrame(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestTelemetryEscapesStayInert feeds hostile record text through the
// preview panes, table cells, the log reader, the inspector, and the full
// frame — each path must render it as visible glyphs, not live escapes.
func TestTelemetryEscapesStayInert(t *testing.T) {
	a := testApp(t, "sessions")
	seedRows(t, a, []map[string]any{{"dt.rum.session.id": "abcdef0123456789-0"}})
	press(a, key("enter"))
	tl := a.top().(*timelineView)
	tl.Update(dataMsg{owner: tl, seq: tl.seq, records: []map[string]any{
		{"characteristics.classifier": "view_summary", "view.name": hostile,
			"start_time": "2026-07-06T16:18:52.000000000Z", "duration": "60000000000"},
	}})
	preview := strings.Join(tl.previewLines(60), "\n")
	assertInert(t, "timeline preview", preview)
	if !strings.Contains(preview, "evil␛]52;c;ZWNobyBoaQ==␇␛[2Jtail") {
		t.Errorf("timeline title should show the escapes as glyphs:\n%q", preview)
	}
	assertInert(t, "timeline view", tl.View(120, 30))
	assertInert(t, "app frame", a.View())

	a = testApp(t, "traces")
	seedRows(t, a, []map[string]any{{
		"trace.id": "0000000000000000000000000000a002", "span.id": "aaaaaaaaaaaaaaaa",
		"span.name": hostile, "span.kind": "server",
	}})
	assertInert(t, "spans table", a.View())
	press(a, key("enter"))
	wf := a.top().(*waterfallView)
	wf.Update(dataMsg{owner: wf, seq: wf.seq, records: []map[string]any{
		{"span.id": "aaaaaaaaaaaaaaaa", "span.name": hostile, "span.kind": "server",
			"service.name": hostile, "duration": "1000000000",
			"start_time": "2026-07-06T16:18:52.000000000Z", "end_time": "2026-07-06T16:18:53.000000000Z"},
	}})
	assertInert(t, "waterfall preview", strings.Join(wf.previewLines(60), "\n"))
	assertInert(t, "waterfall view", wf.View(120, 30))

	spec := catalog.Lookup("logs")
	tv := newTableView(a.ds, spec,
		catalog.Scope{Timeframe: a.tf, Entity: &catalog.Entity{ID: "HOST-1", Name: "web-01", Type: "HOST"}})
	tv.seq++
	rec := map[string]any{"timestamp": "2026-07-16T10:00:00Z", "loglevel": "ERROR",
		"content": hostile + "\nsecond " + hostile}
	tv.Update(dataMsg{owner: tv, seq: tv.seq, elapsed: time.Second, records: []map[string]any{rec}})
	for _, c := range spec.Columns {
		assertInert(t, "cell "+c.Title, pad(c.Text(rec), 40))
	}
	assertInert(t, "logs grid", tv.View(100, 24))
	tv.handleKey(key("w"))
	assertInert(t, "reader", tv.View(100, 24))
	var body catalog.Column
	for _, c := range spec.Columns {
		if c.Field == "content" {
			body = c
		}
	}
	assertInert(t, "readerBlock", strings.Join(tv.readerBlock(0, body, spec.Columns, 80), "\n"))

	vv := renderString("content", hostile, 80)
	assertInert(t, "inspector", strings.Join(vv.lines, "\n")+vv.compact)
	if vv.raw != hostile {
		t.Errorf("inspector raw value must stay unsanitized for yank, got %q", vv.raw)
	}
}
