package catalog

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// MaxHomePanels caps a configured home layout: one scoped query per panel
// (ADR-0013), and the two-column grid stays readable.
const MaxHomePanels = 6

// HomeFollowGlobal is the .dtui.yaml timeframe value that makes a home panel
// track the header window (the `t` picker).
const HomeFollowGlobal = "global"

// HomePanel is one curated home-screen panel: a scoped, limit-capped DQL
// rollup plus how its rows render and where enter jumps. Panels are data,
// like Specs; the home view only lays them out.
type HomePanel struct {
	// ID is the stable .dtui.yaml name (`home.panels[].panel`).
	ID    string
	Title string
	// Dot is the title dot's color: a semantic class ("error", "warn") or
	// "series:<n>" for the chart palette.
	Dot string
	// Window is the curated lookback ("24h"); "" follows the global window.
	Window string
	// Query renders the panel's single query; s.Timeframe is the panel's
	// effective window.
	Query func(s Scope) string
	// Line renders one row and its semantic class.
	Line func(rec map[string]any) (text, class string)
	// Jump is where enter goes; s carries the jump window.
	Jump func(rec map[string]any, s Scope) HomeJump
	// Entity derives the row's Smartscape entity for the global actions
	// (pin, x/X, o) — nil for panels whose rows carry none.
	Entity func(rec map[string]any) *Entity
	// Yank names the row's most copyable identity.
	Yank func(rec map[string]any) string
	// Post reorders/trims fetched records client-side (nil = as fetched).
	Post func(recs []map[string]any) []map[string]any
}

// HomeJump is a home row's enter target: a catalog view (View + Scope +
// client-side Filter) or, when Detail is set, the entity's detail page.
type HomeJump struct {
	View   string
	Scope  Scope
	Filter string
	Detail *Entity
}

// HomePanels is the default home layout, in render order.
var HomePanels = []*HomePanel{
	homeProblems, homeFailingServices, homeK8sWarnings,
	homeFrontendErrors, homeVulnerabilities, homeAttacks,
}

// LookupHomePanel resolves a panel id (case-insensitive).
func LookupHomePanel(id string) *HomePanel {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, p := range HomePanels {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// HomePanelIDs lists the panel ids in default order.
func HomePanelIDs() []string {
	ids := make([]string, len(HomePanels))
	for i, p := range HomePanels {
		ids[i] = p.ID
	}
	return ids
}

// HomeWindow resolves a panel's effective window: a configured override
// ("7d", or HomeFollowGlobal), else the curated Window, else the global one.
// fixed reports whether the result ignores the global window.
func HomeWindow(p *HomePanel, override string, global Timeframe) (tf Timeframe, fixed bool) {
	label := p.Window
	if override != "" {
		label = override
	}
	if label == "" || label == HomeFollowGlobal {
		return global, false
	}
	if tf, ok := ParseTimeframe(label); ok {
		return tf, true
	}
	return global, false
}

// HomeJumpWindow is the window a home jump carries: the panel's window, or
// the global one when that is wider — so the jumped-to list always contains
// the row that was selected.
func HomeJumpWindow(panel, global Timeframe) Timeframe {
	if global.Absolute() || global.Dur > panel.Dur {
		return global
	}
	return panel
}

var homeProblems = &HomePanel{
	ID: "problems", Title: "active problems", Dot: "error", Window: "24h",
	Query: func(s Scope) string {
		return fmt.Sprintf(`fetch dt.davis.problems, from:%s
| filter not(dt.davis.is_duplicate)
| sort timestamp asc
| summarize { status = takeLast(event.status), name = takeLast(event.name), start = takeLast(event.start), affected = takeLast(affected_entity_names) }, by:{display_id}
| filter status == "ACTIVE"
| sort start desc
| limit 8`, s.Timeframe.DQL())
	},
	Line: func(rec map[string]any) (string, string) {
		affected := ""
		if names, _ := rec["affected"].([]any); len(names) > 0 {
			affected = " · " + FormatValue(names[0])
			if len(names) > 1 {
				affected += fmt.Sprintf(" +%d", len(names)-1)
			}
		}
		return fmt.Sprintf("%-8s %4s  %s%s",
			Str(rec, "display_id"), Age(Str(rec, "start")),
			Str(rec, "name"), affected), "error"
	},
	Jump: func(rec map[string]any, s Scope) HomeJump {
		return HomeJump{View: "problems", Scope: s, Filter: Str(rec, "display_id")}
	},
	Yank: func(rec map[string]any) string { return Str(rec, "display_id") },
}

var homeFailingServices = &HomePanel{
	ID: "failing-services", Title: "failing services", Dot: "series:3",
	Query: func(s Scope) string {
		return fmt.Sprintf(`fetch spans, from:%s
| filter request.is_failed == true and isNotNull(dt.smartscape.service)
| summarize failed = count(), svc = takeFirst(coalesce(dt.service.name, service.name)), ns = takeFirst(k8s.namespace.name), by:{dt.smartscape.service}
| sort failed desc
| limit 8`, s.Timeframe.DQL())
	},
	Line: func(rec map[string]any) (string, string) {
		svc := Str(rec, "svc")
		if ns := Str(rec, "ns"); ns != "" {
			svc += " (" + ns + ")"
		}
		return fmt.Sprintf("%-42s %6s failed spans", svc, Str(rec, "failed")), "warn"
	},
	Jump: func(rec map[string]any, s Scope) HomeJump {
		if e := failingServiceEntity(rec); e != nil {
			return HomeJump{Detail: e}
		}
		return HomeJump{View: "services", Scope: s}
	},
	Entity: failingServiceEntity,
	Yank:   func(rec map[string]any) string { return Str(rec, "dt.smartscape.service") },
}

func failingServiceEntity(rec map[string]any) *Entity {
	id := Str(rec, "dt.smartscape.service")
	if id == "" {
		return nil
	}
	return &Entity{ID: id, Name: Str(rec, "svc"), Type: "SERVICE"}
}

// homeK8sWarnings reads the dt.kubernetes.events METRIC on purpose: K8s
// warning events (FailedScheduling, FailedMount, …) are not ingested into
// `fetch events` on real tenants (validated live — no k8s.event.reason rows
// exist there), so the metric is the only record of them.
var homeK8sWarnings = &HomePanel{
	ID: "k8s-warnings", Title: "kubernetes warnings", Dot: "warn", Window: "24h",
	Query: func(s Scope) string {
		return fmt.Sprintf(`timeseries events = sum(dt.kubernetes.events, default: 0), by:{k8s.pod.name, k8s.event.reason}, from:%s, interval: %s, filter: { k8s.event.type == "Warning" }
| limit 100`, s.Timeframe.DQL(), homeSeriesInterval(s.Timeframe))
	},
	Line: func(rec map[string]any) (string, string) {
		return fmt.Sprintf("%-24s %5.0f×  %s",
			Str(rec, "k8s.event.reason"), SeriesTotal(rec, "events"), Str(rec, "k8s.pod.name")), "warn"
	},
	// The pod name goes into the query itself: a client-side filter could
	// miss a pod cut from the limit-capped page.
	Jump: func(rec map[string]any, s Scope) HomeJump {
		s.Entity = &Entity{Name: Str(rec, "k8s.pod.name"), Type: "K8S_POD"}
		return HomeJump{View: "pods", Scope: s}
	},
	Yank: func(rec map[string]any) string { return Str(rec, "k8s.pod.name") },
	// Order by total occurrences, then keep the panel's 8.
	Post: func(recs []map[string]any) []map[string]any {
		sort.SliceStable(recs, func(i, j int) bool {
			return SeriesTotal(recs[i], "events") > SeriesTotal(recs[j], "events")
		})
		if len(recs) > 8 {
			recs = recs[:8]
		}
		return recs
	},
}

// homeSeriesInterval keeps the k8s timeseries at the curated 1h buckets for
// windows up to a week and widens it beyond, so long windows stay small.
func homeSeriesInterval(tf Timeframe) string {
	if tf.Dur > 7*24*time.Hour {
		return "1d"
	}
	if tf.Dur > 0 && tf.Dur < 2*time.Hour {
		return "5m"
	}
	return "1h"
}

// SeriesTotal sums a timeseries column of a record.
func SeriesTotal(rec map[string]any, key string) float64 {
	total := 0.0
	for _, p := range FloatSeries(rec[key]) {
		total += p
	}
	return total
}

var homeFrontendErrors = &HomePanel{
	ID: "frontend-errors", Title: "frontend errors", Dot: "series:1", Window: "24h",
	Query: func(s Scope) string {
		return fmt.Sprintf(`fetch user.events, from:%s
| filter characteristics.classifier == "error"
| summarize count = count(), app = takeFirst(frontend.name), by:{error.display_name}
| sort count desc
| limit 8`, s.Timeframe.DQL())
	},
	Line: func(rec map[string]any) (string, string) {
		return fmt.Sprintf("%5s×  %s · %s",
			Str(rec, "count"), Str(rec, "error.display_name"), Str(rec, "app")), "warn"
	},
	// Lens 1 is the userevents errors lens; the error name narrows
	// client-side.
	Jump: func(rec map[string]any, s Scope) HomeJump {
		s.Lens = 1
		return HomeJump{View: "userevents", Scope: s, Filter: Str(rec, "error.display_name")}
	},
	Yank: func(rec map[string]any) string { return Str(rec, "error.display_name") },
}

// homeVulnerabilities reads the VULNERABILITY level only — entity-level rows
// would pollute the rollup (same filter as the vulnerabilities view).
var homeVulnerabilities = &HomePanel{
	ID: "vulnerabilities", Title: "open vulnerabilities", Dot: "series:2", Window: "24h",
	Query: func(s Scope) string {
		return fmt.Sprintf(`fetch security.events, from:%s
| filter event.type == "VULNERABILITY_STATE_REPORT_EVENT" and event.level == "VULNERABILITY"
| sort timestamp asc
| summarize { title = takeLast(vulnerability.title), display_id = takeLast(vulnerability.display_id), level = takeLast(vulnerability.risk.level), score = takeLast(vulnerability.risk.score), status = takeLast(vulnerability.resolution.status), muted = takeLast(vulnerability.mute.status) }, by:{vulnerability.id}
| filter status == "OPEN" and muted != "MUTED"
| sort score desc
| limit 8`, s.Timeframe.DQL())
	},
	Line: func(rec map[string]any) (string, string) {
		level := Str(rec, "level")
		return fmt.Sprintf("%-5s %4s %-8s %s",
			Str(rec, "display_id"), FormatScore(rec["score"]), level, Str(rec, "title")), RiskClass(level)
	},
	Jump: func(rec map[string]any, s Scope) HomeJump {
		return HomeJump{View: "vulnerabilities", Scope: s, Filter: Str(rec, "display_id")}
	},
	Yank: func(rec map[string]any) string { return Str(rec, "display_id") },
}

// RiskClass maps a vulnerability risk level to a semantic cell class.
func RiskClass(level string) string {
	switch level {
	case "CRITICAL":
		return "error"
	case "HIGH":
		return "warn"
	}
	return "dim"
}

var homeAttacks = &HomePanel{
	ID: "attacks", Title: "attack detections", Dot: "series:4", Window: "24h",
	Query: func(s Scope) string {
		return fmt.Sprintf(`fetch security.events, from:%s
| filter event.type == "DETECTION_FINDING" and product.name == "Runtime Application Protection"
| sort timestamp desc
| limit 8`, s.Timeframe.DQL())
	},
	Line: func(rec map[string]any) (string, string) {
		action := Str(rec, "finding.action")
		class := "warn"
		if action == "Blocked" {
			class = "ok"
		}
		return fmt.Sprintf("%-5s %-14s %-7s %s",
			FormatTime(Str(rec, "timestamp")), Str(rec, "finding.type"),
			action, Str(rec, "k8s.workload.name")), class
	},
	Jump: func(rec map[string]any, s Scope) HomeJump {
		return HomeJump{View: "attacks", Scope: s, Filter: Str(rec, "finding.id")}
	},
	Entity: func(rec map[string]any) *Entity {
		id := Str(rec, "dt.smartscape_source.id")
		if id == "" {
			return nil
		}
		return &Entity{ID: id, Name: Str(rec, "k8s.workload.name"), Type: Str(rec, "dt.smartscape_source.type")}
	},
	Yank: func(rec map[string]any) string { return Str(rec, "finding.id") },
}
