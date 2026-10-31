package tui

// Spike: opt-in layout snapshots for View output.
//
// Unlike the substring assertions elsewhere in this package — which pin down
// *what* a view must show — a snapshot pins down *how* it looks: column
// alignment, truncation, borders, ordering. The rendered, ANSI-stripped screen
// is diffed against a committed golden file under testdata/. Regenerate with:
//
//	go test ./internal/tui -run Snapshot -update
//
// The convention deliberately rejects blanket golden coverage (see
// docs/design/tui.md "Testing"): only stable, seeded screens get a snapshot,
// so layout tweaks churn a few files the reviewer can eyeball, not the suite.

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
)

var updateSnapshots = flag.Bool("update", false, "rewrite testdata/*.golden files with current output")

// assertSnapshot diffs a rendered screen against its golden file. `name` is
// the golden's basename under testdata/ (e.g. "pods-120x40").
func assertSnapshot(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	// Trailing-space and blank-line drift is noise, not layout: trim each line
	// on the right and collapse trailing blank lines before comparing.
	trim := func(s string) string {
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
	if string(want) != got {
		t.Errorf("snapshot %s differs (run with -update to accept):\n%s", path, lineDiff(string(want), got))
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

// screen renders the whole app (header + top view + status bar) stripped of
// ANSI, at whatever size the last WindowSizeMsg set.
func screen(a *app) string {
	return ansi.Strip(a.View())
}

func TestSnapshotPodsTable(t *testing.T) {
	// Pin the render clock so AGE cells are deterministic in the golden.
	catalog.Now = func() time.Time { return time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { catalog.Now = time.Now })

	rows := []map[string]any{
		podRow("checkout-1", "shop", "Running", 0),
		podRow("checkout-2", "shop", "Running", 2),
		podRow("payments-1", "shop", "Pending", 0),
		podRow("inventory-1", "back", "CrashLoopBackOff", 31),
	}
	for i, r := range rows {
		r["node"] = fmt.Sprintf("ip-10-0-1-%d", i+1)
	}

	a := testApp(t, "pods")
	seedRows(t, a, rows)
	assertSnapshot(t, "pods-120x40-peek", screen(a))

	a = testApp(t, "pods")
	disablePreview(a)
	seedRows(t, a, rows)
	assertSnapshot(t, "pods-120x40", screen(a))
}
