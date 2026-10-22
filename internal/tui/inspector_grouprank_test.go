package tui

import (
	"sort"
	"testing"
)

// groupRank sinks resource-context namespaces below the record's own payload
// and dt.* plumbing below everything — an implicit ordering hint only.
func TestGroupRankTiers(t *testing.T) {
	tiers := map[string]int{
		"":     0, // ungrouped top-level fields
		"log":  1, // payload
		"http": 1, // payload (signal-dominant, not demoted despite straddling)
		"db":   1,
		"host": 2, // resource context
		"k8s":  2,
		"aws":  2,
		"dt":   3, // plumbing, very bottom
	}
	for g, want := range tiers {
		if got := groupRank(g); got != want {
			t.Errorf("groupRank(%q) = %d, want %d", g, got, want)
		}
	}
}

func TestGroupRankSortOrder(t *testing.T) {
	names := []string{"dt", "host", "log", "", "k8s", "http", "cloud"}
	sort.Slice(names, func(i, j int) bool {
		if ri, rj := groupRank(names[i]), groupRank(names[j]); ri != rj {
			return ri < rj
		}
		return names[i] < names[j]
	})
	want := []string{"", "http", "log", "cloud", "host", "k8s", "dt"}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("sorted groups = %v, want %v", names, want)
		}
	}
}
