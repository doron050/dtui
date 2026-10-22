package tui

import "sort"

// Smartscape entities carry their tags as multiple `tags:<context>` map fields
// (records of key→value): k8s labels/annotations, cloud provider tags
// (tags:aws, tags:azure, tags:google_compute_engine), and custom contexts
// (tags:environment, …). These helpers give the peek pane and the facet picker
// one shared notion of context ordering and display naming so the two surfaces
// render identically. Validated live across tenants — see
// docs/dev/learnings.md §1.4b.

// tagFieldPrefix is the DQL field-name prefix every tag-context map carries.
const tagFieldPrefix = "tags:"

// tagRef is the raw DQL location of one map-tag facet candidate, keyed in the
// picker by its friendly display identity ("k8s.labels:app", "gcp:zone").
type tagRef struct {
	mapField string // e.g. "tags:k8s.labels"
	mapKey   string // e.g. "app"
}

// tagContextPriority leads the contexts a user most often wants; everything
// else follows alphabetically. Entries are RAW contexts (post-`tags:`).
var tagContextPriority = []string{"k8s.labels", "aws", "azure", "google_compute_engine"}

// tagContextIdentity returns the short raw-context label used as the facet
// identity prefix ("k8s.labels:app", "aws:Name", "gcp:zone") — no `tags:`
// prefix, google_compute_engine abbreviated to gcp. Used in Facet.Field and
// tagRefs keys; must be stable (changing it breaks saved facets).
func tagContextIdentity(ctx string) string {
	if ctx == "google_compute_engine" {
		return "gcp"
	}
	return ctx
}

// tagContextDisplay is the header/label shown in peek and the facet picker:
// always prefixed with "tags:" to match the inspector field name, with
// google_compute_engine shortened to "tags:gcp". Kept identical between
// peek and facet picker UI.
func tagContextDisplay(ctx string) string {
	return "tags:" + tagContextIdentity(ctx)
}

// tagContextOrder orders the given raw contexts: the priority list first (in
// order, when present), then the remainder alphabetically.
func tagContextOrder(contexts map[string][]string) []string {
	out := make([]string, 0, len(contexts))
	seen := map[string]bool{}
	for _, c := range tagContextPriority {
		if _, ok := contexts[c]; ok {
			out = append(out, c)
			seen[c] = true
		}
	}
	rest := make([]string, 0, len(contexts))
	for c := range contexts {
		if !seen[c] {
			rest = append(rest, c)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}
