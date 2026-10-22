// The record-list preview pane: shared size gates and layout (side pane
// on wide screens, bottom panel on narrow-but-tall ones) plus the
// table's preview content — the selected row's curated facts and
// cross-signal jump hints.
package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
	"github.com/dynatrace-oss/dtui/internal/tui/theme"
)

// previewPaneMinWidth is the narrowest screen that fits a side preview; below
// it the preview renders as a bottom panel instead.
const previewPaneMinWidth = 110

// previewBottomH is the bottom preview panel's line budget on narrow screens.
const previewBottomH = 9

// previewBottomMinHeight is the shortest screen that affords the bottom
// panel — below it the panel's ten-line bite starves the table, so the
// preview auto-hides instead.
const previewBottomMinHeight = 30

// previewSideOn / previewBottomOn are the shared size gates — every
// record-list view (tables, the trace waterfall, the session timeline)
// places its preview pane by the same rules. on is the app-wide preference
// (dataSource.previewOn).
func previewSideOn(on bool, width int) bool { return on && width >= previewPaneMinWidth }

func previewBottomOn(on bool, width, height int) bool {
	return on && width < previewPaneMinWidth && height >= previewBottomMinHeight
}

func (v *tableView) previewSide() bool { return previewSideOn(v.ds.previewOn(), v.width) }

func (v *tableView) previewBottom() bool {
	return previewBottomOn(v.ds.previewOn(), v.width, v.height)
}

// previewLayout composes a view body with the selected row's preview pane: a
// side pane on wide screens, a bottom panel on narrow-but-tall ones, the body
// alone otherwise (cramped screens, or preview toggled off with P).
func previewLayout(on bool, width, height int, body func(w, h int) string, preview func(w int) []string) string {
	switch {
	case previewSideOn(on, width):
		paneW := width * 2 / 5
		if paneW > 48 {
			paneW = 48
		}
		leftW := width - paneW - 3
		left := strings.Split(body(leftW, height), "\n")
		right := preview(paneW)
		if len(right) > height {
			right = append(right[:height-1], theme.Dim.Render(fmt.Sprintf("… +%d more (enter opens)", len(right)-height+1)))
		}
		sep := theme.Rule.Render("│")
		var b strings.Builder
		rows := max(len(left), len(right))
		if rows > height {
			rows = height
		}
		for i := 0; i < rows; i++ {
			l, r := "", ""
			if i < len(left) {
				l = left[i]
			}
			if i < len(right) {
				r = right[i]
			}
			b.WriteString(pad(l, leftW) + " " + sep + " " + ansi.Truncate(r, paneW, "…"))
			if i < rows-1 {
				b.WriteString("\n")
			}
		}
		return b.String()
	case previewBottomOn(on, width, height):
		bodyH := max(height-previewBottomH-1, 1)
		out := body(width, bodyH)
		if gap := bodyH - lipgloss.Height(out); gap > 0 {
			out += strings.Repeat("\n", gap)
		}
		lines := preview(width - 2)
		if len(lines) > previewBottomH {
			lines = append(lines[:previewBottomH-1], theme.Dim.Render("… (enter opens the full record)"))
		}
		out += "\n" + theme.Rule.Render(strings.Repeat("─", max(width, 0)))
		for _, l := range lines {
			out += "\n " + ansi.Truncate(l, width-2, "…")
		}
		return out
	}
	return body(width, height)
}

// previewLines renders the selected row's peek pane: identity, then the
// curated key facts for entity rows or the per-kind curated facts for signal
// records — all from the record already in hand, zero queries.
func (v *tableView) previewLines(w int) []string {
	rec := v.selected()
	if rec == nil {
		return []string{theme.Dim.Render("no selection")}
	}
	entity := v.entityOf(rec)
	title := catalog.PreviewTitle(rec)
	if title == "" && entity != nil && entity.Name != "" {
		title = entity.Name
	}
	if title == "" {
		title = v.spec.Name
		for _, key := range []string{"title", "name", "event.name", "span.name", "endpoint.name", "display_id", "content"} {
			if t := catalog.Str(rec, key); t != "" {
				title = t
				break
			}
		}
	}
	lines := []string{theme.OverlayTitle.Render(ansi.Truncate(flatten(title), w, "…"))}
	// Entity LIST rows preview the entity's curated key facts. Signal rows
	// that merely reference an entity (a session's frontend, a problem's
	// affected service) must NOT land here — the record is the signal, and
	// probing entity facts against it reads as an empty pane.
	if v.spec.Kind == catalog.KindEntity && entity != nil {
		id := ansi.Truncate(" "+entity.ID, max(w-lipgloss.Width(entity.Type), 8), "…")
		lines = append(lines, theme.Badge.Render(entity.Type)+theme.Dim.Render(id), "")
		shown := 0
		for _, fact := range catalog.KeyFacts(entity.Type) {
			val := fact.Value(rec)
			if val == "" {
				continue
			}
			text := ansi.Truncate(flatten(val), max(w-len(fact.Label)-4, 8), "…")
			if fact.Class != nil {
				if class := fact.Class(val); class != "" {
					text = theme.Class(class, text)
				}
			}
			lines = append(lines, " "+theme.FactLabel.Render(fact.Label+":")+" "+text)
			shown++
		}
		// Primary-tag lines: declared active keys first, then any additional
		// primary_tags.* keys found on the record that were not in the active set.
		tagLines := v.primaryTagPreviewLines(rec, w)
		lines = append(lines, tagLines...)
		// Smartscape tag groups (tags:k8s.labels, tags:aws, tags:azure, …) that
		// ride the row — grouped by context, short keys under each.
		mapTagLines := v.tagPreviewLines(rec, w)
		lines = append(lines, mapTagLines...)
		if shown <= 1 && len(tagLines) == 0 && len(mapTagLines) == 0 {
			lines = append(lines, theme.Dim.Render(" (sparse list row — enter opens details)"))
		}
		return lines
	}
	lines = append(lines, "")
	if facts := catalog.PreviewFacts(rec); facts != nil {
		lines = append(lines, renderPreviewFacts(facts, w)...)
	} else {
		for _, key := range catalog.PriorityFields(rec) {
			val, ok := rec[key]
			if !ok {
				continue
			}
			text := flatten(catalog.PreviewValue(key, val))
			if text == "" {
				continue
			}
			if key == "event.severity" {
				text = catalog.SeverityBadge(catalog.Str(rec, key))
			}
			// Long prose fields (log content, event descriptions) wrap over a
			// few lines; everything else stays a one-line fact.
			if key == "content" || key == "event.description" {
				lines = append(lines, wrapFactLines(key, text, w)...)
				continue
			}
			if class := catalog.SeverityFieldClass(key, text); class != "" {
				text = theme.Class(class, text)
			}
			lines = append(lines, " "+theme.FactLabel.Render(key+":")+" "+
				ansi.Truncate(text, max(w-len(key)-4, 8), "…"))
		}
	}
	return append(lines, v.previewJumpHints(rec)...)
}

// renderPreviewFacts renders curated preview facts as pane lines — shared by
// the table, the trace waterfall, and the session timeline.
func renderPreviewFacts(facts []catalog.PreviewFact, w int) []string {
	var lines []string
	for _, f := range facts {
		if f.Wrap {
			lines = append(lines, wrapFactLines(f.Label, flatten(f.Value), w)...)
			continue
		}
		text := ansi.Truncate(flatten(f.Value), max(w-len(f.Label)-4, 8), "…")
		if f.Class != "" {
			text = theme.Class(f.Class, text)
		}
		lines = append(lines, " "+theme.FactLabel.Render(f.Label+":")+" "+text)
	}
	return lines
}

// wrapFactLines renders a long prose fact: its label on one line, the value
// flowing over up to four wrapped lines below.
func wrapFactLines(label, text string, w int) []string {
	lines := []string{" " + theme.FactLabel.Render(label)}
	wrapped := wrapLines(text, max(w-2, 8))
	if len(wrapped) > 4 {
		wrapped = append(wrapped[:4], theme.Dim.Render("…"))
	}
	for _, l := range wrapped {
		lines = append(lines, "  "+l)
	}
	return lines
}

// previewJumpHints footers the pane with the record's cross-signal jumps —
// the trace behind a span/log/RUM request, the session behind a RUM event —
// each labeled with the key that actually takes it there on this view.
func (v *tableView) previewJumpHints(rec map[string]any) []string {
	var hints []string
	if v.spec.Trace != nil {
		if id := v.spec.Trace(rec); id != "" {
			key := ""
			switch {
			case v.spec.EnterTarget == "waterfall":
				key = " (enter opens)"
			case v.spec.Drills["s"] == "trace":
				key = " (s opens)"
			}
			hints = append(hints, " "+theme.FactLabel.Render("trace:")+" "+theme.UID.Render(shortID(id))+theme.Dim.Render(key))
		}
	}
	if v.spec.Drills["u"] == "session" {
		if id := catalog.Str(rec, "dt.rum.session.id"); id != "" {
			hints = append(hints, " "+theme.FactLabel.Render("session:")+" "+theme.UID.Render(shortID(id))+theme.Dim.Render(" (u opens)"))
		}
	}
	if len(hints) == 0 {
		return nil
	}
	return append([]string{""}, hints...)
}

// primaryTagPreviewLines renders preview-pane lines for the record's
// primary_tags.* fields. Declared active keys (v.ds.primaryTags) appear
// first in order; any additional keys found on the record that are NOT in
// the active set are appended after. Keys with empty/nil values are skipped.
// Returns nil when no primary tags have values on this record.
func (v *tableView) primaryTagPreviewLines(rec map[string]any, w int) []string {
	const labelW = 8 // fixed label column width (matches KeyFacts alignment)
	activeSeen := map[string]bool{}
	var lines []string
	// Declared active keys first — in user-defined order.
	for _, k := range v.ds.primaryTags {
		field := "primary_tags." + k
		val, ok := rec[field]
		if !ok || val == nil {
			continue
		}
		text := catalog.FormatValue(val)
		if text == "" {
			continue
		}
		activeSeen[k] = true
		labelRunes := []rune(k)
		if len(labelRunes) > labelW {
			labelRunes = labelRunes[:labelW]
		}
		label := string(labelRunes)
		lines = append(lines,
			" "+theme.FactLabel.Render(fmt.Sprintf("%-*s", labelW, label))+" "+
				ansi.Truncate(text, max(w-labelW-4, 8), "…"))
	}
	// Discovered-but-undeclared keys: primary_tags.* keys on the record that
	// were NOT in the active set (e.g. from fullstack agents not referenced in
	// the workspace file). Sorted for stable output.
	var undeclaredKeys []string
	for k := range rec {
		key, ok := strings.CutPrefix(k, "primary_tags.")
		if !ok || activeSeen[key] || rec[k] == nil {
			continue
		}
		if catalog.FormatValue(rec[k]) != "" {
			undeclaredKeys = append(undeclaredKeys, key)
		}
	}
	sort.Strings(undeclaredKeys)
	for _, key := range undeclaredKeys {
		text := catalog.FormatValue(rec["primary_tags."+key])
		labelRunes := []rune(key)
		if len(labelRunes) > labelW {
			labelRunes = labelRunes[:labelW]
		}
		label := string(labelRunes)
		lines = append(lines,
			" "+theme.FactLabel.Render(fmt.Sprintf("%-*s", labelW, label))+" "+
				ansi.Truncate(text, max(w-labelW-4, 8), "…"))
	}
	return lines
}

// tagPreviewLines renders the row's tags:<context> map fields as peek groups:
// a dim context header (via tagContextDisplay: keeps the `tags:` prefix,
// shortens google_compute_engine to gcp) then its keys as " key: value" short
// lines. Contexts are
// ordered by tagContextPriority then alphabetical; the section is capped per
// group and overall with a "… +N more (enter opens)" fold. Valueless entries
// (semantic k8s marker labels) render as a bare key. Reads the record already
// in hand — zero queries. Annotations never appear here: they are not projected
// onto list rows. General across k8s labels and cloud (aws/azure/gcp) tags.
func (v *tableView) tagPreviewLines(rec map[string]any, w int) []string {
	const perGroup, overall = 6, 10
	maps := map[string]map[string]any{}
	order := map[string][]string{}
	for k, val := range rec {
		ctx, ok := strings.CutPrefix(k, tagFieldPrefix)
		if !ok {
			continue
		}
		m, ok := val.(map[string]any)
		if !ok || len(m) == 0 {
			continue
		}
		maps[ctx] = m
		order[ctx] = nil
	}
	if len(maps) == 0 {
		return nil
	}
	tw := max(w, 8)
	var lines []string
	total := 0
	for _, ctx := range tagContextOrder(order) {
		if total >= overall {
			break
		}
		m := maps[ctx]
		keys := make([]string, 0, len(m))
		for kk := range m {
			keys = append(keys, kk)
		}
		sort.Strings(keys)
		lines = append(lines, " "+theme.Dim.Render(tagContextDisplay(ctx)))
		shown := 0
		for _, kk := range keys {
			if shown >= perGroup || total >= overall {
				break
			}
			line := "  " + kk
			if val := catalog.FormatValue(m[kk]); val != "" {
				line += ": " + val
			}
			lines = append(lines, ansi.Truncate(line, tw, "…"))
			shown++
			total++
		}
		if rest := len(keys) - shown; rest > 0 {
			lines = append(lines, "  "+theme.Dim.Render(fmt.Sprintf("… +%d more (enter opens)", rest)))
		}
	}
	return lines
}
