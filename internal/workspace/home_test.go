package workspace

import (
	"strings"
	"testing"
)

func TestLoadHome(t *testing.T) {
	ws, err := Load(writeFile(t, t.TempDir(), `
timeframe: 2h
home:
  panels:
    - Problems
    - panel: vulnerabilities
      timeframe: 7d
    - panel: failing-services
      timeframe: global
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(ws.HomeIssues) != 0 || ws.Home == nil {
		t.Fatalf("home should parse cleanly: %v", ws.HomeIssues)
	}
	want := []HomePanel{{Panel: "problems"}, {Panel: "vulnerabilities", Timeframe: "7d"}, {Panel: "failing-services", Timeframe: HomeFollowGlobal}}
	for i, p := range ws.Home.Panels {
		if p.Panel != want[i].Panel || p.Timeframe != want[i].Timeframe || p.Line == 0 {
			t.Errorf("panels[%d] = %+v, want %+v", i, p, want[i])
		}
	}
}

// TestLoadHomeIssues: a broken home block lists every finding with its
// path and line, and never rejects the rest of the file.
func TestLoadHomeIssues(t *testing.T) {
	cases := map[string]struct{ yaml, want string }{
		"not a mapping":  {"home: problems", "home: expected a mapping"},
		"missing panels": {"home:\n  layout: grid", "home.layout: unknown key"},
		"panels scalar":  {"home:\n  panels: problems", "home.panels: expected a list"},
		"empty":          {"home:\n  panels: []", "home.panels: empty"},
		"bad timeframe":  {"home:\n  panels:\n    - panel: problems\n      timeframe: 2w", `home.panels[0].timeframe: "2w"`},
		"zero timeframe": {"home:\n  panels:\n    - panel: problems\n      timeframe: 0h", `home.panels[0].timeframe: "0h"`},
		"unknown key":    {"home:\n  panels:\n    - panel: problems\n      limit: 3", "home.panels[0].limit: unknown key"},
		"missing id":     {"home:\n  panels:\n    - timeframe: 7d", "home.panels[0]: missing panel id"},
		"duplicate":      {"home:\n  panels:\n    - problems\n    - PROBLEMS", `home.panels[1]: duplicate panel "problems"`},
		"nested value":   {"home:\n  panels:\n    - panel: [a, b]", "home.panels[0].panel: expected a single value"},
		"list item kind": {"home:\n  panels:\n    - [problems]", "home.panels[0]: expected a panel id"},
		"column key":     {"home:\n  panels:\n    - panel: problems\n      column: right", "home.panels[0].column: unknown key (supported: panel, timeframe)"},
		"rows key":       {"home:\n  rows: [problems]", "home.rows: unknown key (supported: panels)"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ws, err := Load(writeFile(t, t.TempDir(), "timeframe: 2h\n"+c.yaml+"\n"))
			if err != nil {
				t.Fatalf("a broken home block must not reject the file: %v", err)
			}
			if ws.Timeframe != "2h" {
				t.Error("the rest of the file should still apply")
			}
			var got []string
			for _, is := range ws.HomeIssues {
				got = append(got, is.Error())
				if is.Line == 0 {
					t.Errorf("issue %q has no line", is.Error())
				}
			}
			if !strings.Contains(strings.Join(got, "\n"), c.want) {
				t.Errorf("issues %q miss %q", got, c.want)
			}
		})
	}
}

func TestLoadReportsUnknownTopLevelKeys(t *testing.T) {
	ws, err := Load(writeFile(t, t.TempDir(), "timeframe: 2h\ncolour: blue\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ws.UnknownKeys) != 1 || ws.UnknownKeys[0].Path != "colour" || ws.UnknownKeys[0].Line != 2 {
		t.Errorf("unknown keys = %+v", ws.UnknownKeys)
	}
}

// TestIssuePathsQuoteCraftedKeys: keys come from a committed file and the
// validator prints their paths to a terminal, so a key carrying control
// characters is quoted, never passed through.
func TestIssuePathsQuoteCraftedKeys(t *testing.T) {
	ws, err := Load(writeFile(t, t.TempDir(), "\"\\e]0;x\\a\": 1\nhome:\n  \"\\e[5mrows\": []\n  panels:\n    - panel: problems\n      \"\\e[31mlimit\": 3\n"))
	if err != nil {
		t.Fatal(err)
	}
	issues := append(append([]Issue(nil), ws.UnknownKeys...), ws.HomeIssues...)
	if len(issues) != 3 {
		t.Fatalf("want 3 issues, got %+v", issues)
	}
	for _, is := range issues {
		if strings.ContainsAny(is.Error(), "\x1b\a") {
			t.Errorf("issue %q carries a raw control character", is.Error())
		}
	}
	if got := ws.UnknownKeys[0].Path; got != `"\x1b]0;x\a"` {
		t.Errorf("top-level key path = %s", got)
	}
}

func TestKeyPath(t *testing.T) {
	for _, c := range []struct{ prefix, key, want string }{
		{"", "colour", "colour"},
		{"home", "rows", "home.rows"},
		{"home.panels[0]", "primary-tags_2", "home.panels[0].primary-tags_2"},
		{"home", "a b", `home."a b"`},
		{"home", "", `home.""`},
	} {
		if got := keyPath(c.prefix, c.key); got != c.want {
			t.Errorf("keyPath(%q, %q) = %s, want %s", c.prefix, c.key, got, c.want)
		}
	}
}
