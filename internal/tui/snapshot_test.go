package tui

// Opt-in layout snapshots for View output.
//
// Unlike the substring assertions elsewhere in this package — which pin down
// *what* a view must show — a snapshot pins down *how* it looks: column
// alignment, truncation, borders, ordering. The rendered, ANSI-stripped screen
// is diffed against a committed golden file under testdata/. Regenerate with:
//
//	go test ./internal/tui -run Snapshot -update
//
// Two conventions keep churn low (docs/design/tui.md "Testing"):
//   - only main screens and their sanity states get a snapshot — a populated
//     table view, not every overlay, picker, or edge state — so a small
//     feature change can't break a dozen goldens; and seeded data only
//     (no live data, no timers, catalog.Now pinned);
//   - every snapshot covers the canonical size matrix in snapshotSizes, so a
//     new view snapshot is one loop, not a bespoke set of resolutions.
//
// This file is the generic harness only. Each view's snapshot test lives in
// <view>_snapshot_test.go (pods_snapshot_test.go, hosts_snapshot_test.go, …)
// with fixtures shaped like that view's real Grail records.

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
)

// snapshotSizes is the canonical terminal-size matrix every layout snapshot
// covers. Each size exercises a distinct layout branch, so regressions in any
// of them fail loudly:
//
//	80x24  — classic minimum terminal; preview pane hidden (w<110, h<30)
//	100x40 — bottom preview band (w<110, h≥30; see tablepreview.go)
//	120x40 — side preview, the default desktop size used across the suite
//	160x50 — wide; flex columns expand to fill
//
// Adding a view snapshot means looping snapshotSizes — never inventing a
// one-off resolution — and only on a main screen's sanity state.
var snapshotSizes = []struct{ w, h int }{
	{80, 24},
	{100, 40},
	{120, 40},
	{160, 50},
}

// assertSnapshots resizes the app through the real WindowSizeMsg path and
// diffs each render against testdata/<prefix>-<w>x<h>.golden.
func assertSnapshots(t *testing.T, prefix string, a *app) {
	t.Helper()
	for _, s := range snapshotSizes {
		a.Update(tea.WindowSizeMsg{Width: s.w, Height: s.h})
		assertSnapshot(t, fmt.Sprintf("%s-%dx%d", prefix, s.w, s.h), screen(a))
	}
}

var updateSnapshots = flag.Bool("update", false, "rewrite testdata/*.golden files with current output")

// assertSnapshot diffs a rendered screen against its golden file. `name` is
// the golden's basename under testdata/ (e.g. "pods-120x40").
func assertSnapshot(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	// Trailing-space, blank-line, and line-ending drift is noise, not layout:
	// normalize CRLF (Windows checkouts mangle it via autocrlf), trim each
	// line on the right, and collapse trailing blank lines before comparing.
	trim := func(s string) string {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		lines := strings.Split(s, "\n")
		for i := range lines {
			lines[i] = strings.TrimRight(lines[i], " ")
		}
		return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
	}
	got = trim(got)
	if *updateSnapshots {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated %s", path)
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run with -update to create)", path, err)
	}
	if trim(string(want)) != got {
		t.Errorf("snapshot %s differs (run with -update to accept):\n%s", path, lineDiff(trim(string(want)), got))
	}
}

// lineDiff renders only the screen rows that differ, with line numbers, so a
// layout regression reads as a small visual diff instead of two full screens.
func lineDiff(want, got string) string {
	wl, gl := strings.Split(want, "\n"), strings.Split(got, "\n")
	var b strings.Builder
	for i := 0; i < len(wl) || i < len(gl); i++ {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w == g {
			continue
		}
		fmt.Fprintf(&b, "  line %3d want |%s|\n           got  |%s|\n", i+1, w, g)
	}
	return b.String()
}

// pinSnapshotClock pins the render clock and zone so relative ages (SEEN,
// AGE) and wall-clock cells (TIME, first/last seen) read the same on every
// machine: catalog.Now drives the former, time.Local the latter.
func pinSnapshotClock(t *testing.T) {
	t.Helper()
	now, local := catalog.Now, time.Local
	catalog.Now = func() time.Time { return snapshotNow }
	time.Local = time.UTC
	t.Cleanup(func() { catalog.Now, time.Local = now, local })
}

// snapshotNow is the pinned render instant; fixtures place their timestamps
// relative to it.
var snapshotNow = time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)

// screen renders the whole app (header + top view + status bar) stripped of
// ANSI, at whatever size the last WindowSizeMsg set.
func screen(a *app) string {
	return ansi.Strip(a.View())
}
