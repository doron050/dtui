package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
	"github.com/dynatrace-oss/dtui/internal/tui/theme"
)

// logLimitSteps are the caps "load more" (+) walks through past the default.
var logLimitSteps = []int{1000, 3000, 10000}

// logLimit is the effective row cap for the current scope (the default until
// "load more" raises it).
func (v *tableView) logLimit() int {
	if v.scope.Limit > 0 {
		return v.scope.Limit
	}
	return catalog.DefaultLogLimit
}

// nextLogLimit returns the next cap above cur, or cur when already at the top.
func nextLogLimit(cur int) int {
	for _, s := range logLimitSteps {
		if s > cur {
			return s
		}
	}
	return cur
}

// handleReaderKey applies the reader's record-cursor movement. Records are
// variable height (a wrapped multi-line body), so the reader can't share the
// fixed-page scroller: the cursor moves per record and renderReader recomputes
// the top-of-window index to keep it on screen. Reports whether the key was one
// of the movement keys.
func (v *tableView) handleReaderKey(key string) bool {
	n := len(v.rows)
	if n == 0 {
		return false
	}
	page := v.readerPage
	if page < 1 {
		page = 1
	}
	switch key {
	case "up", "k":
		v.readerMove(-1)
	case "down", "j":
		v.readerMove(1)
	case "pgup", "ctrl+b":
		v.readerMove(-page)
	case "pgdown", "ctrl+f", " ":
		v.readerMove(page)
	case "home", "g":
		v.cursor, v.readerTop = 0, 0
	case "end", "G":
		v.readerMove(n)
	default:
		return false
	}
	return true
}

// readerMove shifts the reader cursor by delta, clamped to the row range.
func (v *tableView) readerMove(delta int) {
	v.cursor += delta
	if v.cursor >= len(v.rows) {
		v.cursor = len(v.rows) - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
}

// splitReaderCols picks the body column (the first flex column — CONTENT on the
// logs specs) and returns the rest as the metadata prefix. ok is false when the
// column set has no flex column to read.
func splitReaderCols(cols []catalog.Column) (body catalog.Column, meta []catalog.Column, ok bool) {
	for _, c := range cols {
		if c.Width == 0 && !ok {
			body, ok = c, true
			continue
		}
		meta = append(meta, c)
	}
	return body, meta, ok
}

// renderReader draws the file-like log stream: each record's body column
// wrapped to width, preceded by an optional dim metadata line, with the
// selected record highlighted as one block. The window is variable-height, so
// the top index is derived here from the cursor.
func (v *tableView) renderReader(width, height int) string {
	var b strings.Builder

	// Status line — mirrors the table head, plus the reader's mode badges.
	head := " " + theme.Count.Render(fmt.Sprintf("%d", len(v.rows)))
	if len(v.rows) != len(v.all) {
		head += theme.Dim.Render(fmt.Sprintf("/%d", len(v.all)))
	}
	head += theme.Dim.Render(" lines")
	if v.elapsed != "" {
		head += theme.Dim.Render(" · " + v.elapsed)
	}
	head += theme.Dim.Render(" · ") + theme.Badge.Render("raw")
	ord := "tail"
	if v.scope.LogHead {
		ord = "head"
	}
	head += " " + theme.Dim.Render(ord+fmt.Sprintf(" · limit %d", v.logLimit()))
	for _, s := range v.searches {
		head += "  " + theme.Badge.Render("⌕ "+s)
	}
	for _, f := range v.facets {
		head += " " + theme.Badge.Render(f.Label())
	}
	if v.filtering || v.filter != "" {
		head += "  " + v.filterInput.View()
	}
	if v.loading {
		head = " " + theme.Spinner.Render(theme.Spin()) + head
	}
	b.WriteString(ansi.Truncate(head, width, "…"))
	b.WriteString("\n")

	bodyH := height - 1
	if bodyH < 1 {
		bodyH = 1
	}

	if v.err != nil {
		b.WriteString("\n" + theme.Error.Render("✗ "+wrap(v.err.Error(), width-2)))
		return b.String()
	}

	cols := v.columns()
	body, meta, ok := splitReaderCols(cols)
	if !ok || len(v.rows) == 0 {
		msg := "∅ no data in timeframe (last " + v.scope.Timeframe.Label + ")"
		if v.filter != "" && len(v.all) > 0 {
			msg = fmt.Sprintf("no lines match %q (%d fetched — esc clears the filter)", v.filter, len(v.all))
		} else if !ok {
			msg = "this view has no content column to read"
		}
		b.WriteString("\n" + lipgloss.PlaceHorizontal(width, lipgloss.Center, theme.Dim.Render(msg)))
		return b.String()
	}

	block := func(i int) []string { return v.readerBlock(i, body, meta, width) }

	// Keep the cursor's record in view. Scroll up if it fell above the window;
	// otherwise, if it fell below the last fully-visible record, re-anchor the
	// top so the cursor sits at the bottom of the window.
	if v.cursor < v.readerTop {
		v.readerTop = v.cursor
	} else {
		used, last := 0, v.readerTop
		for i := v.readerTop; i < len(v.rows); i++ {
			h := len(block(i))
			if used > 0 && used+h > bodyH {
				break
			}
			used += h
			last = i
			if used >= bodyH {
				break
			}
		}
		if v.cursor > last {
			used, top := 0, v.cursor
			for i := v.cursor; i >= 0; i-- {
				h := len(block(i))
				if i != v.cursor && used+h > bodyH {
					break
				}
				used += h
				top = i
				if used >= bodyH {
					break
				}
			}
			v.readerTop = top
		}
	}

	// Emit records from the top index until the body is full.
	var lines []string
	shown := 0
	for i := v.readerTop; i < len(v.rows) && len(lines) < bodyH; i++ {
		lines = append(lines, block(i)...)
		shown++
	}
	v.readerPage = shown
	if len(lines) > bodyH {
		lines = lines[:bodyH]
	}
	b.WriteString(strings.Join(lines, "\n"))
	return b.String()
}

// readerBlock renders one record as its wrapped body — content only, so the
// stream reads like a log file (metadata is one enter away, in the inspector).
// The selected record renders as one highlighted block; others are tinted by
// log level.
func (v *tableView) readerBlock(i int, body catalog.Column, meta []catalog.Column, width int) []string {
	rec := v.rows[i]
	sel := i == v.cursor

	var out []string
	cw := width - 2
	if cw < 8 {
		cw = 8
	}
	// Split on real newlines first so an embedded stack trace keeps its shape,
	// then soft-wrap each segment to width.
	var content []string
	for _, seg := range strings.Split(body.Text(rec), "\n") {
		seg = strings.TrimRight(seg, "\r")
		if seg == "" {
			content = append(content, "")
			continue
		}
		content = append(content, wrapLines(seg, cw)...)
	}
	if len(content) == 0 {
		content = []string{""}
	}

	cls := ""
	if !sel {
		cls = rowClass(rec, meta)
	}
	for _, cl := range content {
		switch {
		case sel:
			out = append(out, theme.Gutter.Render("▌")+theme.Selected.Render(pad(cl, width-1)))
		case cls != "":
			out = append(out, "  "+theme.Class(cls, cl))
		default:
			out = append(out, "  "+cl)
		}
	}
	return out
}

// rowClass returns the record's log-level render class (from the first metadata
// column that classes its value), used to tint raw content lines.
func rowClass(rec map[string]any, meta []catalog.Column) string {
	for _, c := range meta {
		if c.Class == nil {
			continue
		}
		if cls := c.Class(strings.TrimSpace(c.Text(rec))); cls != "" {
			return cls
		}
	}
	return ""
}
