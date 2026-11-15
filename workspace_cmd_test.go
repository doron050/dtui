package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
	"github.com/dynatrace-oss/dtui/internal/workspace"
)

func TestHomeFollowGlobalAgrees(t *testing.T) {
	if workspace.HomeFollowGlobal != catalog.HomeFollowGlobal {
		t.Fatal("workspace and catalog disagree on the follow-global keyword")
	}
}

func TestHomeLayout(t *testing.T) {
	ws, err := workspace.Load(writeWorkspaceFile(t, `
home:
  panels:
    - attacks
    - panel: problems
      timeframe: 3d
`))
	if err != nil {
		t.Fatal(err)
	}
	panels, issues := homeLayout(ws)
	if len(issues) != 0 || len(panels) != 2 || panels[0].ID != "attacks" || panels[1].Timeframe != "3d" {
		t.Fatalf("homeLayout = %+v, %v", panels, issues)
	}
	if !strings.Contains(workspaceNotice(ws), "2 home panels") {
		t.Errorf("notice should announce the layout: %q", workspaceNotice(ws))
	}

	none, _ := workspace.Load(writeWorkspaceFile(t, "timeframe: 2h\n"))
	if p, is := homeLayout(none); p != nil || is != nil {
		t.Errorf("no home block should mean the default layout, got %+v %v", p, is)
	}
}

// TestHomeLayoutRejectsWhole: any bad entry rejects the whole layout — the
// home falls back to the default panels — and every finding is reported.
func TestHomeLayoutRejectsWhole(t *testing.T) {
	ws, err := workspace.Load(writeWorkspaceFile(t, `
home:
  panels:
    - problems
    - vulns
    - panel: attacks
      timeframe: 2w
`))
	if err != nil {
		t.Fatal(err)
	}
	panels, issues := homeLayout(ws)
	if panels != nil || len(issues) != 2 {
		t.Fatalf("homeLayout = %+v, %v", panels, issues)
	}
	if !strings.Contains(issues[0].Error(), `home.panels[1]: unknown panel "vulns" — available: problems,`) {
		t.Errorf("first issue = %q", issues[0].Error())
	}
	if got := homeNotice(issues); !strings.HasSuffix(got, "(+1 more)") {
		t.Errorf("notice = %q", got)
	}

	tooMany, _ := workspace.Load(writeWorkspaceFile(t, "home:\n  panels: [problems, failing-services, k8s-warnings, frontend-errors, vulnerabilities, attacks, problems]\n"))
	if _, issues := homeLayout(tooMany); len(issues) == 0 {
		t.Error("duplicates beyond the cap should be rejected")
	}
}

func TestReportWorkspace(t *testing.T) {
	var out bytes.Buffer
	ok := reportWorkspace(&out, writeWorkspaceFile(t, `
timeframe: 2h
colour: blue
home:
  panels:
    - panel: vulnerabilities
      timeframe: 7d
    - failing-services
    - attacks
`))
	got := out.String()
	if !ok {
		t.Fatalf("warnings alone must not fail validation:\n%s", got)
	}
	for _, want := range []string{
		"✓ home          3 panels",
		"! line 3   colour: unknown key",
		"left   1. vulnerabilities    7d    fixed · ◷ pinned away from header",
		"right  1. failing-services   2h    follows t",
		"2. attacks            24h   fixed · curated default",
		"✓ valid, 1 warning",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report misses %q:\n%s", want, got)
		}
	}

	out.Reset()
	reportWorkspace(&out, writeWorkspaceFile(t, "view: pods\n"))
	if want := "✓ version       (not set) — 1"; !strings.Contains(out.String(), want) {
		t.Errorf("report misses %q:\n%s", want, out.String())
	}

	for _, label := range []string{"0h", "99999999999d"} {
		out.Reset()
		if reportWorkspace(&out, writeWorkspaceFile(t, "timeframe: "+label+"\n")) {
			t.Errorf("timeframe %s is unusable, validation must fail:\n%s", label, out.String())
		}
		if want := "✗ timeframe     \"" + label + "\" is not a usable window — dtui uses 2h"; !strings.Contains(out.String(), want) {
			t.Errorf("report misses %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	if reportWorkspace(&out, writeWorkspaceFile(t, "view: nope\nhome:\n  panels: [vulns]\n")) {
		t.Errorf("unknown view and panel should fail validation:\n%s", out.String())
	}
	for _, want := range []string{`✗ view          unknown view "nope"`, `line 3   home.panels[0]: unknown panel "vulns"`, "default — the configured one is rejected", "✗ 2 errors"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report misses %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	if reportWorkspace(&out, writeWorkspaceFile(t, "timeframe: forever\n")) || !strings.Contains(out.String(), "ignores this whole file") {
		t.Errorf("a file-level error should fail and say the whole file is ignored:\n%s", out.String())
	}
}

// TestReportWorkspaceIsTerminalSafe: a committed .dtui.yaml is untrusted
// input, so nothing from it reaches the terminal as a control sequence —
// not keys, not values, not the startup notice.
func TestReportWorkspaceIsTerminalSafe(t *testing.T) {
	files := []string{
		"\"\\e]0;x\\a\": 1\nview: \"\\e[5mnope\"\nhome:\n  \"\\e[31mrows\": []\n  panels:\n    - panel: \"\\e[2Jvulns\"\n      \"\\e[1mlimit\": 3\n",
		"timeframe: \"\\e[31mforever\"\n",
		"segments: [\"\\e]0;x\\a\\e[31mred\"]\nprimary-tags: [\"\\e[5mblink\"]\n",
	}
	for _, f := range files {
		var out bytes.Buffer
		reportWorkspace(&out, writeWorkspaceFile(t, f))
		if strings.ContainsAny(out.String(), "\x1b\a") {
			t.Errorf("report carries a raw control character:\n%q", out.String())
		}
	}
	if runtime.GOOS == "windows" {
		return // NTFS rejects control characters in file names
	}
	dir := filepath.Join(t.TempDir(), "\x1b]0;x\a\x1b[31mrepo")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, workspace.FileName)
	for _, body := range []string{"view: pods\n", "view: [\n"} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		reportWorkspace(&out, path)
		if strings.ContainsAny(out.String(), "\x1b\a") {
			t.Errorf("report carries a raw control character from the path:\n%q", out.String())
		}
	}
	ws, err := workspace.Load(writeWorkspaceFile(t, files[0]))
	if err != nil {
		t.Fatal(err)
	}
	_, issues := homeLayout(ws)
	if n := homeNotice(issues); n == "" || strings.ContainsAny(n, "\x1b\a") {
		t.Errorf("home notice = %q", n)
	}
}

// TestHomeWindowGrammarsAgree: validate accepts a panel window exactly when
// the runtime can parse it, so "✓ valid" never names a window dtui drops.
func TestHomeWindowGrammarsAgree(t *testing.T) {
	for _, label := range []string{"30m", "2h", "7d", "106751d", "106752d", "99999999999d", "0h", "00m", "2w", "-2h", "1.5h"} {
		ws, err := workspace.Load(writeWorkspaceFile(t, "home:\n  panels:\n    - panel: problems\n      timeframe: "+label+"\n"))
		if err != nil {
			t.Fatal(err)
		}
		_, issues := homeLayout(ws)
		_, ok := catalog.ParseTimeframe(label)
		if (len(issues) == 0) != ok {
			t.Errorf("%s: validate accepts = %v, runtime parses = %v (%v)", label, len(issues) == 0, ok, issues)
		}
	}
}

func TestHomeLayoutColumns(t *testing.T) {
	path := writeWorkspaceFile(t, `
home:
  panels:
    - problems
    - attacks
    - panel: vulnerabilities
      timeframe: 7d
`)
	ws, err := workspace.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	panels, issues := homeLayout(ws)
	if len(issues) != 0 || len(panels) != 3 || panels[1].ID != "attacks" {
		t.Fatalf("homeLayout = %+v, %v", panels, issues)
	}
	var out bytes.Buffer
	if !reportWorkspace(&out, path) {
		t.Fatalf("layout should validate:\n%s", out.String())
	}
	for _, want := range []string{"left   1. problems", "       2. vulnerabilities    7d", "right  1. attacks"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report misses %q:\n%s", want, out.String())
		}
	}
}

// TestHomeLayoutConfigurations walks the home block shapes a user can write:
// each valid one maps to its panels in entry order, each invalid one rejects
// the whole layout with a finding that names the entry.
func TestHomeLayoutConfigurations(t *testing.T) {
	for name, c := range map[string]struct {
		yaml  string
		ids   string // panel ids in order, for valid layouts
		issue string // substring of the first finding, for invalid ones
	}{
		"bare ids":         {yaml: "panels: [problems, attacks]", ids: "problems attacks"},
		"all six":          {yaml: "panels: [problems, failing-services, k8s-warnings, frontend-errors, vulnerabilities, attacks]", ids: "problems failing-services k8s-warnings frontend-errors vulnerabilities attacks"},
		"mixed entries":    {yaml: "panels:\n    - problems\n    - {panel: attacks}\n    - {panel: vulnerabilities, timeframe: 7d}", ids: "problems attacks vulnerabilities"},
		"global window":    {yaml: "panels:\n    - {panel: problems, timeframe: global}", ids: "problems"},
		"too many":         {yaml: "panels: [problems, failing-services, k8s-warnings, frontend-errors, vulnerabilities, attacks, problems]", issue: "duplicate panel"},
		"unknown panel":    {yaml: "panels: [problems, vulns]", issue: `home.panels[1]: unknown panel "vulns"`},
		"column key":       {yaml: "panels:\n    - {panel: problems, column: right}", issue: "home.panels[0].column: unknown key"},
		"bad window":       {yaml: "panels:\n    - {panel: problems, timeframe: 1w}", issue: `home.panels[0].timeframe: "1w"`},
		"empty list":       {yaml: "panels: []", issue: "home.panels: empty"},
		"unknown home key": {yaml: "panels: [problems]\n  layout: grid", issue: "home.layout: unknown key"},
	} {
		t.Run(name, func(t *testing.T) {
			ws, err := workspace.Load(writeWorkspaceFile(t, "home:\n  "+c.yaml+"\n"))
			if err != nil {
				t.Fatal(err)
			}
			panels, issues := homeLayout(ws)
			if c.issue != "" {
				if panels != nil || len(issues) == 0 || !strings.Contains(issues[0].Error(), c.issue) {
					t.Fatalf("want rejection %q, got %+v / %v", c.issue, panels, issues)
				}
				return
			}
			if len(issues) != 0 {
				t.Fatalf("unexpected findings: %v", issues)
			}
			ids := make([]string, len(panels))
			for i, p := range panels {
				ids[i] = p.ID
			}
			if got := strings.Join(ids, " "); got != c.ids {
				t.Errorf("panels = %s, want %s", got, c.ids)
			}
		})
	}
}
