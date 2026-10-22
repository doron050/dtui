// The table's facet manager ('f'): a two-stage in-view overlay that adds
// server-side attribute filters — the first stage lists the active
// searches/facets (edit/remove) above attribute candidates derived from
// the fetched records, the value stage picks from the server's
// fieldsSummary.
package tui

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
	"github.com/dynatrace-oss/dtui/internal/tui/theme"
)

// facetStage is the facet picker's overlay state.
type facetStage int

const (
	facetOff facetStage = iota
	facetFieldStage
	facetValueStage
)

// facetEntry is one selectable row of the manager stage: an active search
// term or facet (edit/remove), or an attribute to add a new facet by.
type facetEntry struct {
	kind facetEntryKind
	idx  int    // index into searches/facets (active kinds)
	attr string // attribute name (entryAttr)
}

type facetEntryKind int

const (
	entrySearch facetEntryKind = iota
	entryFacet
	entryAttr
)

// facetOwner tags the facet picker's fieldsSummary query; field pins the
// result to the attribute it was requested for (two in-flight explorations
// must not cross).
type facetOwner struct {
	v     *tableView
	field string
}

// openFacets opens the manager: active filters (edit/remove) above the
// attribute candidates from the fetched records' keys.
func (v *tableView) openFacets() tea.Cmd {
	if v.spec.API != "" {
		// Facet exploration pairs the fetched records' attributes with
		// fieldsSummary over the view's query — on an API view the records
		// are NOT the query's rows (patterns are analyzer output over a
		// logs pipeline), so interactive faceting would filter on fields
		// the pipeline never carries. Facets inherited from the source
		// list (the 'a' drill) still apply; '/' filters client-side.
		return statusErr("facets need a DQL-backed view — / filters " + v.spec.Name + " client-side")
	}
	v.facetFields = v.facetCandidates()
	if len(v.facetFields) == 0 && len(v.searches)+len(v.facets) == 0 {
		return statusErr("no attributes to facet on (no rows fetched)")
	}
	v.facetMode = facetFieldStage
	// Adding is the common case: start on the first attribute; the active
	// filters sit above, one ↑ away.
	v.facetSel = len(v.searches) + len(v.facets)
	v.facetEdit = -1
	v.facetErr = nil
	v.facetInput.SetValue("")
	v.facetInput.Focus()
	// Lazily discover smartscape tag contexts (k8s labels, aws/azure/gcp tags,
	// …) so they can join the candidate list grouped by context; cached per
	// scope.
	return tea.Batch(textinput.Blink, v.tagDiscoverCmd())
}

// clearFacets drops every server search and facet, refetching unnarrowed.
func (v *tableView) clearFacets() tea.Cmd {
	if len(v.searches) == 0 && len(v.facets) == 0 {
		return status("no active facets")
	}
	v.searches = nil
	v.facets = nil
	return tea.Batch(v.Refresh(), markHistory, status("facets cleared"))
}

// addSearch stacks (or, replacing, swaps in) a server-side search term.
func (v *tableView) addSearch(term string, replace bool) tea.Cmd {
	if replace {
		v.searches = []string{term}
		return tea.Batch(v.Refresh(), markHistory, status(fmt.Sprintf("server search %q — f manages · F clears", term)))
	}
	for _, s := range v.searches {
		if s == term {
			return status(fmt.Sprintf("search %q already active", term))
		}
	}
	v.searches = append(v.searches, term)
	return tea.Batch(v.Refresh(), markHistory, status(fmt.Sprintf("server search %q added — f manages · F clears", term)))
}

// addFacet appends a facet (deduplicated) and refetches — shared by the
// picker's value stage and the inspector's cross-view 'f'.
func (v *tableView) addFacet(f catalog.Facet) tea.Cmd {
	for _, existing := range v.facets {
		if existing == f {
			return status("facet " + f.Label() + " already active")
		}
	}
	v.facets = append(v.facets, f)
	return tea.Batch(v.Refresh(), markHistory, status("facet "+f.Label()+" — f manages · F clears"))
}

// hasField reports whether any fetched record carries the key — the guard
// that keeps a cross-view facet from silently emptying the list.
func (v *tableView) hasField(field string) bool {
	for _, rec := range v.all {
		if _, ok := rec[field]; ok {
			return true
		}
	}
	return false
}

// facetCandidates returns the scalar attribute keys available for faceting.
// For MetricRoster specs this is the pass-1 discovered dim set (all values are
// inherently populated — a value-less dim is never returned by the metrics
// descriptor scan). For all other specs it scans the fetched records; for
// primary_tags.* keys in projection-based views at least one non-nil value is
// required before the key is offered.
func (v *tableView) facetCandidates() []string {
	if v.spec.MetricRoster {
		// Return the discovered dims, leading with the bucket field if eligible.
		dims := make([]string, 0, len(v.metricDims))
		dims = append(dims, v.metricDims...)
		if catalog.BucketEligible(v.dql) {
			dims = append([]string{catalog.BucketField}, dims...)
		}
		return dims
	}
	seen := map[string]bool{}
	// primaryTagNonNull tracks primary_tags.* keys that have at least one
	// non-null value in the fetched sample — used to suppress null-only keys.
	primaryTagNonNull := map[string]bool{}
	var fields []string
	for _, rec := range v.all {
		for k, val := range rec {
			// Skip synthetic keys (enrichment series, resolved display values):
			// the "__" convention marks fields the app stamped on the record, not
			// Grail fields — faceting one would hit a non-existent server field
			// and silently return empty. See ADR-0016 and inspector.rebuild.
			if seen[k] || strings.HasPrefix(k, "__") || k == catalog.BucketField {
				continue
			}
			switch val.(type) {
			case map[string]any, []any:
				// Structured values are never scalar facets. Smartscape
				// tags:<context> maps are faceted per-key through the lazily
				// discovered tag census (tagCandidates), not scanned here.
				continue
			}
			if strings.HasPrefix(k, "primary_tags.") {
				// Track whether this key has any non-nil/non-empty value.
				if val != nil && catalog.FormatValue(val) != "" {
					primaryTagNonNull[k] = true
				}
				if !seen[k] {
					seen[k] = true
					fields = append(fields, k)
				}
				continue
			}
			seen[k] = true
			fields = append(fields, k)
		}
	}
	// Remove primary_tags.* keys that are null in every fetched record — they
	// were injected by fieldsAdd on a projection spec and have no actual values.
	filtered := fields[:0:0]
	for _, k := range fields {
		if strings.HasPrefix(k, "primary_tags.") && !primaryTagNonNull[k] {
			continue
		}
		filtered = append(filtered, k)
	}
	fields = filtered
	sort.Strings(fields)
	return v.orderCandidates(fields, v.tagCandidates())
}

// tagCandidates builds the grouped smartscape-tag facet candidates from the
// discovered census and (re)populates v.tagRefs. Identities are
// "<display-context>:<key>" (e.g. "k8s.labels:app", "azure:Dtp_Stage",
// "gcp:zone"); the render layer shows the short key under a per-context header.
// General across k8s / aws / azure / gcp / custom contexts.
func (v *tableView) tagCandidates() []string {
	v.tagRefs = map[string]tagRef{}
	if len(v.tagContexts) == 0 {
		return nil
	}
	var out []string
	for _, ctx := range tagContextOrder(v.tagContexts) {
		ident := tagContextIdentity(ctx)
		for _, key := range v.tagContexts[ctx] {
			id := ident + ":" + key
			v.tagRefs[id] = tagRef{mapField: tagFieldPrefix + ctx, mapKey: key}
			out = append(out, id)
		}
	}
	return out
}

// orderCandidates assembles the final picker order: the bucket field (Grail's
// primary narrowing axis) first on bucket-backed views, then the spec's
// FacetPriority "preselected" dimensions (those actually present in the
// sample), then the remaining scalars, then the discovered tag tail (always
// last, grouped by context via tagContextOrder in tagCandidates).
func (v *tableView) orderCandidates(scalars, tagKeys []string) []string {
	present := make(map[string]bool, len(scalars))
	for _, f := range scalars {
		present[f] = true
	}
	added := map[string]bool{}
	out := make([]string, 0, len(scalars)+len(tagKeys)+1)
	for _, p := range v.spec.FacetPriority {
		if present[p] && !added[p] {
			out = append(out, p)
			added[p] = true
		}
	}
	for _, f := range scalars {
		if !added[f] {
			out = append(out, f)
		}
	}
	// Discovered tag keys always trail — one identity per context+key, already
	// grouped by tagContextOrder.
	out = append(out, tagKeys...)
	if catalog.BucketEligible(v.spec.Query(v.scope)) {
		out = append([]string{catalog.BucketField}, out...)
	}
	return out
}

func (v *tableView) handleFacetKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		if v.facetMode == facetValueStage {
			// Back to the manager, not out of the picker.
			v.facetMode = facetFieldStage
			v.facetSel = len(v.searches) + len(v.facets)
			v.facetEdit = -1
			v.facetErr = nil
			v.facetInput.SetValue("")
			return claimKey
		}
		v.closeFacets()
		return claimKey
	case "up", "ctrl+k":
		v.facetSel = clampSel(v.facetSel-1, v.facetListLen())
	case "down", "ctrl+j", "tab":
		v.facetSel = clampSel(v.facetSel+1, v.facetListLen())
	case "pgup":
		v.facetSel = clampSel(v.facetSel-v.facetPage(), v.facetListLen())
	case "pgdown":
		v.facetSel = clampSel(v.facetSel+v.facetPage(), v.facetListLen())
	case "enter":
		if v.facetMode == facetFieldStage {
			entries := v.facetEntries()
			if len(entries) == 0 {
				return nil
			}
			return v.enterFacetEntry(entries[clampSel(v.facetSel, len(entries))])
		}
		return v.applyFacet()
	case "ctrl+x", "delete":
		if v.facetMode == facetFieldStage {
			return v.removeFacetEntry()
		}
		fallthrough
	default:
		var cmd tea.Cmd
		v.facetInput, cmd = v.facetInput.Update(msg)
		if v.facetMode == facetFieldStage {
			// Typing hunts for an attribute — jump the selection past the
			// active filters so enter adds rather than edits.
			v.facetSel = len(v.searches) + len(v.facets)
		} else {
			v.facetSel = 0
		}
		return cmd
	}
	return nil
}

// enterFacetEntry acts on a manager row: attributes explore their values,
// active filters open prefilled for editing.
func (v *tableView) enterFacetEntry(e facetEntry) tea.Cmd {
	switch e.kind {
	case entryAttr:
		v.facetEdit = -1
		return v.openFacetValues(e.attr)
	case entryFacet:
		v.facetEdit = e.idx
		cmd := v.openFacetValues(v.facets[e.idx].Field)
		v.facetInput.SetValue(v.facets[e.idx].Value)
		return cmd
	default: // entrySearch — edit the term in place, no suggestions to fetch
		v.facetMode = facetValueStage
		v.facetField = ""
		v.facetMapField, v.facetMapKey = "", ""
		v.facetEdit = e.idx
		v.facetOptions = nil
		v.facetErr = nil
		v.facetLoading = false
		v.facetSel = 0
		v.facetInput.SetValue(v.searches[e.idx])
		return nil
	}
}

// removeFacetEntry drops the selected active filter (manager stage).
func (v *tableView) removeFacetEntry() tea.Cmd {
	entries := v.facetEntries()
	if len(entries) == 0 {
		return nil
	}
	e := entries[clampSel(v.facetSel, len(entries))]
	var label string
	switch e.kind {
	case entrySearch:
		label = "⌕" + v.searches[e.idx]
		v.searches = append(v.searches[:e.idx], v.searches[e.idx+1:]...)
	case entryFacet:
		label = v.facets[e.idx].Label()
		v.facets = append(v.facets[:e.idx], v.facets[e.idx+1:]...)
	default:
		return status("nothing to remove — select an active filter above")
	}
	v.facetSel = clampSel(v.facetSel, len(v.facetEntries()))
	return tea.Batch(v.Refresh(), markHistory, status("removed "+label))
}

// facetListLen is the length of whichever list the picker currently shows.
func (v *tableView) facetListLen() int {
	if v.facetMode == facetFieldStage {
		return len(v.facetEntries())
	}
	return len(v.facetValueMatches())
}

// openFacetValues advances to the value stage: the server's top values for
// field (v.facetEdit >= 0 means the chosen value replaces that facet).
func (v *tableView) openFacetValues(field string) tea.Cmd {
	v.facetMode = facetValueStage
	v.facetField = field
	// Resolve the map-subkey target when field is a discovered tag identity.
	// When editing an already-applied tag facet whose census is not loaded on
	// this view, fall back to the parts stored on the facet itself.
	v.facetMapField, v.facetMapKey = "", ""
	if ref, ok := v.tagRefs[field]; ok {
		v.facetMapField, v.facetMapKey = ref.mapField, ref.mapKey
	} else {
		for _, f := range v.facets {
			if f.Field == field && f.MapField != "" {
				v.facetMapField, v.facetMapKey = f.MapField, f.MapKey
				break
			}
		}
	}
	v.facetOptions = nil
	v.facetErr = nil
	v.facetLoading = true
	v.facetSel = 0
	v.facetInput.SetValue("")
	target := catalog.Facet{Field: field, MapField: v.facetMapField, MapKey: v.facetMapKey}
	var dql string
	if v.spec.MetricRoster {
		// For the metric roster, explore values on the raw series descriptors
		// so every dimension's distinct values are reachable regardless of the
		// summarize collapse.
		dql = catalog.ServiceFieldsSummaryQuery(v.scope.Timeframe, target, v.facets)
	} else {
		dql = catalog.FieldsSummaryQuery(v.spec.Query(v.scope), target, v.searches, v.facets)
	}
	return v.ds.query(facetOwner{v: v, field: field}, v.seq, dql)
}

// applyFacet commits the value-stage choice: input containing '*' applies as
// a wildcard pattern, a highlighted suggestion applies exactly, and
// free-typed text without suggestions applies as an exact value. In edit
// mode the result replaces the entry it was opened from.
func (v *tableView) applyFacet() tea.Cmd {
	input := strings.TrimSpace(v.facetInput.Value())
	if v.facetField == "" { // editing a search term
		if input == "" {
			return statusErr("empty term — esc goes back, ctrl+x in the manager removes")
		}
		v.searches[v.facetEdit] = input
		v.closeFacets()
		return tea.Batch(v.Refresh(), markHistory, status(fmt.Sprintf("search %q updated", input)))
	}
	matches := v.facetValueMatches()
	var value string
	switch {
	case strings.Contains(input, "*"):
		value = input
	case len(matches) > 0:
		value = matches[clampSel(v.facetSel, len(matches))].Value
	case input != "":
		value = input
	default:
		return nil
	}
	f := catalog.Facet{Field: v.facetField, Value: value, MapField: v.facetMapField, MapKey: v.facetMapKey}
	if v.facetEdit >= 0 {
		v.facets[v.facetEdit] = f
		v.closeFacets()
		return tea.Batch(v.Refresh(), markHistory, status("facet "+f.Label()+" updated"))
	}
	v.closeFacets()
	return v.addFacet(f)
}

func (v *tableView) closeFacets() {
	v.facetMode = facetOff
	v.facetLoading = false
	v.facetEdit = -1
	v.facetInput.Blur()
	v.facetInput.SetValue("")
}

// facetEntries lists the manager rows: active searches, active facets, then
// the attribute candidates narrowed by the picker input (active filters stay
// pinned — they are few, and removal must not require clearing the input).
func (v *tableView) facetEntries() []facetEntry {
	entries := make([]facetEntry, 0, len(v.searches)+len(v.facets)+len(v.facetFields))
	for i := range v.searches {
		entries = append(entries, facetEntry{kind: entrySearch, idx: i})
	}
	for i := range v.facets {
		entries = append(entries, facetEntry{kind: entryFacet, idx: i})
	}
	matches := v.facetFieldMatches()
	for _, f := range matches {
		entries = append(entries, facetEntry{kind: entryAttr, attr: f})
	}
	// Escape hatch: the candidates are only the keys present in the fetched
	// sample (rows are `| limit N`-capped), so a field that exists on the
	// tenant but not in these rows never shows up. When a typed field name
	// matches no candidate, offer it verbatim — the value stage runs
	// fieldsSummary on it server-side and finds its values regardless of the
	// sample.
	if len(matches) == 0 {
		if typed := strings.TrimSpace(v.facetInput.Value()); typed != "" {
			entries = append(entries, facetEntry{kind: entryAttr, attr: typed})
		}
	}
	return entries
}

// attrGroupHeader is the section label an attribute candidate sits under: its
// tag context (display form) for a map-tag identity, "add by attribute" for a
// scalar field, "" for non-attribute rows.
func (v *tableView) attrGroupHeader(e facetEntry) string {
	if e.kind != entryAttr {
		return ""
	}
	if ref, ok := v.tagRefs[e.attr]; ok {
		ctx, _ := strings.CutPrefix(ref.mapField, tagFieldPrefix)
		return tagContextDisplay(ctx)
	}
	return "add by attribute"
}

// facetFieldMatches filters attribute candidates by the picker input.
func (v *tableView) facetFieldMatches() []string {
	needle := strings.ToLower(strings.TrimSpace(v.facetInput.Value()))
	if needle == "" {
		return v.facetFields
	}
	var out []string
	for _, f := range v.facetFields {
		if strings.Contains(strings.ToLower(f), needle) {
			out = append(out, f)
		}
	}
	return out
}

// facetValueMatches filters suggestions by the picker input. Input holding a
// '*' is pattern syntax headed for applyFacet, not a suggestion filter.
func (v *tableView) facetValueMatches() []catalog.FacetValue {
	needle := strings.ToLower(strings.TrimSpace(v.facetInput.Value()))
	if needle == "" || strings.Contains(needle, "*") {
		return v.facetOptions
	}
	var out []catalog.FacetValue
	for _, fv := range v.facetOptions {
		if strings.Contains(strings.ToLower(fv.Value), needle) {
			out = append(out, fv)
		}
	}
	return out
}

// clampSel bounds a selection index against a (possibly shrunken) list.
func clampSel(sel, n int) int {
	if sel >= n {
		sel = n - 1
	}
	if sel < 0 {
		return 0
	}
	return sel
}

// renderFacetPicker renders the overlay: the manager (active filters +
// attribute list), or the value stage for the explored attribute.
func (v *tableView) renderFacetPicker() string {
	const valueW, countW = 44, 10
	var b strings.Builder
	if v.facetMode == facetFieldStage {
		b.WriteString(theme.OverlayTitle.Render("facets — "+v.spec.Name) + "\n\n")
		b.WriteString(" " + v.facetInput.View() + "\n\n")
		entries := v.facetEntries()
		sel := clampSel(v.facetSel, len(entries))
		active := len(v.searches) + len(v.facets)
		if active > 0 {
			b.WriteString(theme.Section("active — enter edits · ctrl+x removes") + "\n")
		}
		v.renderFacetList(&b, len(entries), sel, func(i int) string {
			e := entries[i]
			switch e.kind {
			case entrySearch:
				return pad("⌕ "+v.searches[e.idx], valueW) + " " + cell("search", countW, true)
			case entryFacet:
				return pad(v.facets[e.idx].Label(), valueW) + " " + cell("facet", countW, true)
			default:
				if e.attr == catalog.BucketField {
					// Pinned first on bucket-backed views; the tag says why.
					return pad(e.attr, valueW) + " " + cell("bucket", countW, true)
				}
				if ref, ok := v.tagRefs[e.attr]; ok {
					// Discovered tag candidate: show the short key, indented
					// under its per-context header (see the header func below).
					return pad("  "+ref.mapKey, valueW+countW+1)
				}
				if !slices.Contains(v.facetFields, e.attr) {
					// The typed-field escape hatch (not in the sampled keys).
					return pad(e.attr, valueW) + " " + cell("typed", countW, true)
				}
				return pad(e.attr, valueW+countW+1)
			}
		}, func(i int) string {
			// Attribute headers: each tag context group gets its own header
			// (tags:k8s.labels, tags:azure, …); scalar attributes sit under
			// "add by attribute". A header prints before the first attr of a
			// run whose group differs from the row above it.
			if i < active {
				return ""
			}
			cur := v.attrGroupHeader(entries[i])
			if cur == "" {
				return ""
			}
			if i == active || entries[i-1].kind != entryAttr {
				return theme.Section(cur)
			}
			if v.attrGroupHeader(entries[i-1]) != cur {
				return theme.Section(cur)
			}
			return ""
		})
		b.WriteString("\n" + theme.Dim.Render("type to narrow · enter add/edit · esc close"))
		return b.String()
	}

	title := "facet — " + v.facetField
	if v.facetField == "" {
		title = "search — full text (any field)"
	}
	b.WriteString(theme.OverlayTitle.Render(title) + "\n\n")
	b.WriteString(" " + v.facetInput.View() + "\n\n")
	switch {
	case v.facetField == "":
		b.WriteString(theme.Dim.Render(" matches records containing the term in any field") + "\n")
	case v.facetLoading:
		b.WriteString(" " + theme.Spinner.Render(theme.Spin()) + theme.Dim.Render(" exploring top values…") + "\n")
	case v.facetErr != nil:
		b.WriteString(" " + theme.Error.Render("✗ "+wrap(v.facetErr.Error(), valueW+countW)) + "\n")
	default:
		matches := v.facetValueMatches()
		sel := clampSel(v.facetSel, len(matches))
		if len(matches) == 0 {
			b.WriteString(theme.Dim.Render(" no values (type one — enter applies it)") + "\n")
		}
		v.renderFacetList(&b, len(matches), sel, func(i int) string {
			return pad(matches[i].Value, valueW) + " " + cell(matches[i].Count, countW, true)
		}, nil)
	}
	b.WriteString("\n" + theme.Dim.Render("enter apply · payment* / *ayment* patterns · esc back"))
	return b.String()
}

// facetPage is the vertical step of a pgup/pgdown through the facet lists —
// the visible-row window, so a page moves one screenful.
func (v *tableView) facetPage() int { return max(v.height-12, 4) }

// renderFacetList writes a windowed, selection-highlighted list of n rows;
// header (optional) injects a section line before row i. The window follows
// the selection and is bracketed with "↑ N more"/"↓ N more" markers when it
// clips, so long lists (a high-cardinality field can return hundreds of
// values) scroll visibly instead of stopping at the first screenful.
func (v *tableView) renderFacetList(b *strings.Builder, n, sel int, row func(i int) string, header func(i int) string) {
	limit := v.facetPage()
	offset := 0
	if sel >= limit {
		offset = sel - limit + 1
	}
	end := offset + limit
	if end > n {
		end = n
	}
	if offset > 0 {
		b.WriteString(theme.Dim.Render(fmt.Sprintf(" ↑ %d more", offset)) + "\n")
	}
	for i := offset; i < end; i++ {
		if header != nil {
			if h := header(i); h != "" {
				b.WriteString(h + "\n")
			}
		}
		if i == sel {
			b.WriteString(theme.Selected.Render(" " + row(i) + " "))
		} else {
			b.WriteString(" " + row(i) + " ")
		}
		b.WriteString("\n")
	}
	if rest := n - end; rest > 0 {
		b.WriteString(theme.Dim.Render(fmt.Sprintf(" ↓ %d more", rest)) + "\n")
	}
}
