package workspace

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// HomeFollowGlobal is the home-panel timeframe that tracks the session
// window (the `t` picker) instead of a fixed lookback.
const HomeFollowGlobal = "global"

// Home panel columns. Panels sit in the left column unless they say
// column: right; each column lists its panels top to bottom in entry order.
const (
	ColumnLeft  = "left"
	ColumnRight = "right"
)

// Home is the optional home-screen layout: which curated panels the home
// view shows, in which order and column, and each one's lookback. It narrows
// what the session shows like every other workspace key — panels are
// curated ids, not queries.
//
//	home:
//	  panels:
//	    - problems                 # left (the default)
//	    - panel: vulnerabilities
//	      column: right
//	      timeframe: 7d
type Home struct {
	Panels []HomePanel // entry order
}

// HomePanel is one home.panels entry. Panel ids are checked against the
// panel catalog by the consumer (the workspace package knows no views).
type HomePanel struct {
	Panel     string // curated panel id, lower-cased
	Timeframe string // "" (panel default), "<n>m|h|d", or HomeFollowGlobal
	Column    string // ColumnLeft or ColumnRight
	Line      int    // source line, for error messages

	path string
}

// Path is the entry's YAML path ("home.panels[2]"), for messages.
func (p HomePanel) Path() string { return p.path }

// Issue is one finding against the workspace file, pinned to its YAML path
// and source line so a validator can point at it.
type Issue struct {
	Line int    // 1-based source line (0 = unknown)
	Path string // YAML path: "home.panels[2].timeframe"
	Msg  string
}

func (i Issue) Error() string {
	if i.Path == "" {
		return i.Msg
	}
	return i.Path + ": " + i.Msg
}

func issueAt(n *yaml.Node, path, format string, args ...any) Issue {
	return Issue{Line: n.Line, Path: path, Msg: fmt.Sprintf(format, args...)}
}

// parseHome strictly decodes the home block. Unlike top-level keys, unknown
// keys inside it are errors: a typo there would otherwise silently show a
// layout the author did not write. Every finding is reported, not just the
// first, so one validate run lists everything to fix. The panels that did
// parse are returned alongside the issues, so a validator can check them
// against the catalog too.
func parseHome(n *yaml.Node) (*Home, []Issue) {
	if n.Kind != yaml.MappingNode {
		return nil, []Issue{issueAt(n, "home", "expected a mapping with a panels list")}
	}
	var issues []Issue
	var panels *yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Value == "panels" {
			panels = v
			continue
		}
		issues = append(issues, issueAt(k, "home."+k.Value, "unknown key (supported: panels)"))
	}
	switch {
	case panels == nil:
		return nil, append(issues, issueAt(n, "home", "missing panels — list the panels to show"))
	case panels.Kind != yaml.SequenceNode:
		return nil, append(issues, issueAt(panels, "home.panels", "expected a list of panel ids"))
	case len(panels.Content) == 0:
		return nil, append(issues, issueAt(panels, "home.panels", "empty — list at least one, or remove the home block for the default layout"))
	}

	h := &Home{}
	seen := map[string]string{}
	for i, item := range panels.Content {
		path := fmt.Sprintf("home.panels[%d]", i)
		p, errs := parseHomePanel(item, path)
		p.path = path
		issues = append(issues, errs...)
		if p.Panel == "" {
			continue
		}
		if first, dup := seen[p.Panel]; dup {
			issues = append(issues, issueAt(item, path, "duplicate panel %q (already %s)", p.Panel, first))
			continue
		}
		seen[p.Panel] = path
		h.Panels = append(h.Panels, p)
	}
	return h, issues
}

func parseHomePanel(n *yaml.Node, path string) (HomePanel, []Issue) {
	p := HomePanel{Line: n.Line, Column: ColumnLeft}
	switch n.Kind {
	case yaml.ScalarNode:
		p.Panel = strings.ToLower(strings.TrimSpace(n.Value))
		if p.Panel == "" {
			return p, []Issue{issueAt(n, path, "empty panel id")}
		}
		return p, nil
	case yaml.MappingNode:
	default:
		return p, []Issue{issueAt(n, path, "expected a panel id or a {panel, column, timeframe} mapping")}
	}

	var issues []Issue
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if v.Kind != yaml.ScalarNode {
			issues = append(issues, issueAt(v, path+"."+k.Value, "expected a single value"))
			continue
		}
		switch k.Value {
		case "panel":
			p.Panel = strings.ToLower(strings.TrimSpace(v.Value))
		case "timeframe":
			tf := strings.ToLower(strings.TrimSpace(v.Value))
			if tf != HomeFollowGlobal && !validWindow(tf) {
				issues = append(issues, issueAt(v, path+".timeframe", "%q — expected <n>m, <n>h, <n>d (e.g. 7d) or %q", v.Value, HomeFollowGlobal))
				continue
			}
			p.Timeframe = tf
		case "column":
			col := strings.ToLower(strings.TrimSpace(v.Value))
			if col != ColumnLeft && col != ColumnRight {
				issues = append(issues, issueAt(v, path+".column", "%q — expected %q or %q (default %s)", v.Value, ColumnLeft, ColumnRight, ColumnLeft))
				continue
			}
			p.Column = col
		default:
			issues = append(issues, issueAt(k, path+"."+k.Value, "unknown key (supported: panel, column, timeframe)"))
		}
	}
	if p.Panel == "" {
		issues = append(issues, issueAt(n, path, "missing panel id (panel: <id>)"))
	}
	return p, issues
}

// validWindow accepts a positive relative window ("30m", "24h", "7d").
func validWindow(s string) bool {
	return timeframeRe.MatchString(s) && strings.TrimLeft(s[:len(s)-1], "0") != ""
}
