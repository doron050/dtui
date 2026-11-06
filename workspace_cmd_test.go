package main

import (
	"bytes"
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
`))
	got := out.String()
	if !ok {
		t.Fatalf("warnings alone must not fail validation:\n%s", got)
	}
	for _, want := range []string{
		"✓ home          2 panels",
		"! line 3   colour: unknown key",
		"full   1. vulnerabilities    7d    fixed · ◷ differs from header",
		"       2. failing-services   2h    follows t",
		"✓ valid, 1 warning",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report misses %q:\n%s", want, got)
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

func TestHomeLayoutColumns(t *testing.T) {
	path := writeWorkspaceFile(t, `
home:
  panels:
    - problems
    - panel: attacks
      column: right
    - panel: vulnerabilities
      timeframe: 7d
`)
	ws, err := workspace.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	panels, issues := homeLayout(ws)
	if len(issues) != 0 || len(panels) != 3 || panels[0].Right || !panels[1].Right || panels[2].Right {
		t.Fatalf("homeLayout = %+v, %v", panels, issues)
	}
	var out bytes.Buffer
	if !reportWorkspace(&out, path) {
		t.Fatalf("column layout should validate:\n%s", out.String())
	}
	for _, want := range []string{"left   1. problems", "       2. vulnerabilities    7d", "right  1. attacks"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report misses %q:\n%s", want, out.String())
		}
	}
}

// TestHomeLayoutConfigurations walks the home block shapes a user can write:
// each valid one maps to its panels and columns, each invalid one rejects
// the whole layout with a finding that names the entry.
func TestHomeLayoutConfigurations(t *testing.T) {
	for name, c := range map[string]struct {
		yaml  string
		cols  string // "L"/"R" per panel, for valid layouts
		issue string // substring of the first finding, for invalid ones
	}{
		"bare ids":         {yaml: "panels: [problems, attacks]", cols: "LL"},
		"all six":          {yaml: "panels: [problems, failing-services, k8s-warnings, frontend-errors, vulnerabilities, attacks]", cols: "LLLLLL"},
		"mixed columns":    {yaml: "panels:\n    - problems\n    - {panel: attacks, column: right}\n    - {panel: vulnerabilities, column: left, timeframe: 7d}", cols: "LRL"},
		"right only":       {yaml: "panels:\n    - {panel: problems, column: right}\n    - {panel: attacks, column: right}", cols: "RR"},
		"global window":    {yaml: "panels:\n    - {panel: problems, timeframe: global, column: right}", cols: "R"},
		"too many":         {yaml: "panels: [problems, failing-services, k8s-warnings, frontend-errors, vulnerabilities, attacks, problems]", issue: "duplicate panel"},
		"unknown panel":    {yaml: "panels: [problems, vulns]", issue: `home.panels[1]: unknown panel "vulns"`},
		"bad column":       {yaml: "panels:\n    - {panel: problems, column: center}", issue: `home.panels[0].column: "center"`},
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
			var cols string
			for _, p := range panels {
				cols += map[bool]string{false: "L", true: "R"}[p.Right]
			}
			if cols != c.cols {
				t.Errorf("columns = %s, want %s", cols, c.cols)
			}
		})
	}
}
