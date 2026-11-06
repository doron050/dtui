package catalog

import (
	"strings"
	"testing"
)

func TestHomePanelsRegistry(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range HomePanels {
		if p.ID == "" || seen[p.ID] || p.Query == nil || p.Line == nil || p.Jump == nil {
			t.Errorf("panel %q is incomplete or duplicated", p.ID)
		}
		seen[p.ID] = true
		if LookupHomePanel(strings.ToUpper(p.ID)) != p {
			t.Errorf("LookupHomePanel(%q) should be case-insensitive", p.ID)
		}
		if j := p.Jump(map[string]any{}, Scope{}); j.Detail == nil && Lookup(j.View) == nil {
			t.Errorf("panel %q jumps to unknown view %q", p.ID, j.View)
		}
	}
	if len(HomePanels) > MaxHomePanels {
		t.Errorf("default layout exceeds MaxHomePanels")
	}
	if LookupHomePanel("nope") != nil {
		t.Error("unknown id should not resolve")
	}
}

// TestHomeWindow pins the window resolution: curated default, override,
// "global", and the follow-the-header default of failing-services.
func TestHomeWindow(t *testing.T) {
	global, _ := ParseTimeframe("2h")
	cases := []struct {
		id, override, want string
		fixed              bool
	}{
		{"problems", "", "24h", true},
		{"problems", "7d", "7d", true},
		{"problems", HomeFollowGlobal, "2h", false},
		{"failing-services", "", "2h", false},
		{"failing-services", "30m", "30m", true},
	}
	for _, c := range cases {
		tf, fixed := HomeWindow(LookupHomePanel(c.id), c.override, global)
		if tf.Label != c.want || fixed != c.fixed {
			t.Errorf("%s/%q = %s fixed=%v, want %s fixed=%v", c.id, c.override, tf.Label, fixed, c.want, c.fixed)
		}
		q := LookupHomePanel(c.id).Query(Scope{Timeframe: tf})
		if !strings.Contains(q, "from:"+tf.DQL()) {
			t.Errorf("%s query misses its window %q:\n%s", c.id, tf.DQL(), q)
		}
	}
}

func TestHomeJumpWindowWidens(t *testing.T) {
	day, _ := ParseTimeframe("24h")
	week, _ := ParseTimeframe("7d")
	if got := HomeJumpWindow(day, week); got.Label != "7d" {
		t.Errorf("wider global should win, got %s", got.Label)
	}
	two, _ := ParseTimeframe("2h")
	if got := HomeJumpWindow(day, two); got.Label != "24h" {
		t.Errorf("panel window should win over a narrower global, got %s", got.Label)
	}
}

func TestHomeK8sIntervalScalesWithWindow(t *testing.T) {
	p := LookupHomePanel("k8s-warnings")
	for label, want := range map[string]string{"30m": "interval: 5m", "24h": "interval: 1h", "30d": "interval: 1d"} {
		tf, _ := ParseTimeframe(label)
		if q := p.Query(Scope{Timeframe: tf}); !strings.Contains(q, want) {
			t.Errorf("%s: query misses %q:\n%s", label, want, q)
		}
	}
	recs := []map[string]any{{"events": []any{1.0}}, {"events": []any{5.0, 2.0}}}
	if got := p.Post(recs); SeriesTotal(got[0], "events") != 7 {
		t.Errorf("k8s warnings should sort by total occurrences")
	}
}
