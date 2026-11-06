package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
	"github.com/dynatrace-oss/dtui/internal/tui/theme"
)

// HomePanelConfig is one configured home panel (.dtui.yaml home.panels):
// a catalog.HomePanel id, an optional window override ("7d", or
// catalog.HomeFollowGlobal) and its column.
type HomePanelConfig struct {
	ID        string
	Timeframe string
	Right     bool // right column; the left one is the default
}

// homeWideMin is the width from which the two home columns sit side by side.
const homeWideMin = 110

// DefaultHomePanels is the zero-config home: the curated panels alternating
// left and right, so they pair up row by row.
func DefaultHomePanels() []HomePanelConfig {
	out := make([]HomePanelConfig, len(catalog.HomePanels))
	for i, def := range catalog.HomePanels {
		out[i] = HomePanelConfig{ID: def.ID, Right: i%2 == 1}
	}
	return out
}

// HomeColumns splits panels into rendered columns of panel indices, each in
// entry order: left then right. With no right panel, or on a narrow
// terminal (wide = false), every panel stacks in one full-width column.
func HomeColumns(panels []HomePanelConfig, wide bool) [][]int {
	var left, right, all []int
	for i, p := range panels {
		all = append(all, i)
		if p.Right {
			right = append(right, i)
		} else {
			left = append(left, i)
		}
	}
	if !wide || len(left) == 0 || len(right) == 0 {
		return [][]int{all}
	}
	return [][]int{left, right}
}

// homeLayout is the session's home configuration: the configured panels
// (nil = the default layout) and, when a configuration was rejected, the
// reason the home view keeps on screen.
type homeLayout struct {
	panels []HomePanelConfig
	notice string
}

// homeView is the triage landing page: curated catalog.HomePanels, each
// loading independently. enter on a row jumps into the corresponding full
// view, pre-filtered.
type homeView struct {
	ds *dataSource
	tf catalog.Timeframe // the global window

	panels []*homePanel
	notice string // rejected-configuration banner ("" = none)
	focus  int    // panel index owning the cursor
	cursor int    // line index within the focused panel
	seq    int    // refresh generation; each panel keeps the one it last fired

	width, height int
}

type homePanel struct {
	def      *catalog.HomePanel
	override string // configured window ("" = curated default)
	right    bool   // right column
	records  []map[string]any
	loading  bool
	err      error
	dql      string
	seq      int // generation of the in-flight query — drops stale results
}

// window is the panel's effective window and whether it ignores `t`.
func (p *homePanel) window(global catalog.Timeframe) (catalog.Timeframe, bool) {
	return catalog.HomeWindow(p.def, p.override, global)
}

// panelOwner routes dataMsg results to one panel.
type panelOwner struct {
	v   *homeView
	idx int
}

func newHomeView(ds *dataSource, tf catalog.Timeframe) *homeView {
	v := &homeView{ds: ds, tf: tf, notice: ds.home.notice}
	cfgs := ds.home.panels
	if len(cfgs) == 0 {
		cfgs = DefaultHomePanels()
	}
	for _, c := range cfgs {
		if def := catalog.LookupHomePanel(c.ID); def != nil {
			v.panels = append(v.panels, &homePanel{def: def, override: c.Timeframe, right: c.Right})
		}
	}
	return v
}

// Selection exposes the focused row for the app-level actions (pin, x/X, o).
// Only rows that carry a Smartscape identity yield an entity; the rest still
// return their record so y has something to say.
func (v *homeView) Selection() (map[string]any, *catalog.Entity) {
	p := v.panels[v.focus]
	if v.cursor >= len(p.records) {
		return nil, nil
	}
	rec := p.records[v.cursor]
	if p.def.Entity != nil {
		return rec, p.def.Entity(rec)
	}
	return rec, nil
}

// YankText copies the focused row's most specific identity (the global 'y').
func (v *homeView) YankText() (string, string, bool) {
	p := v.panels[v.focus]
	if v.cursor >= len(p.records) || p.def.Yank == nil {
		return "", "", false
	}
	text := p.def.Yank(p.records[v.cursor])
	if text == "" {
		return "", "", false
	}
	return text, text, true
}

func classRisk(level string) string { return catalog.RiskClass(level) }

func (v *homeView) Init() tea.Cmd { return v.Refresh() }

func (v *homeView) Refresh() tea.Cmd { return v.refresh(false) }

// refresh re-runs the panels — only those following the global window when
// globalOnly (a `t` change leaves fixed-window panels untouched).
func (v *homeView) refresh(globalOnly bool) tea.Cmd {
	v.seq++
	var cmds []tea.Cmd
	for i, p := range v.panels {
		tf, fixed := p.window(v.tf)
		if globalOnly && fixed {
			continue
		}
		p.loading = true
		p.err = nil
		p.seq = v.seq
		p.dql = p.def.Query(catalog.Scope{Timeframe: tf})
		cmds = append(cmds, v.ds.query(panelOwner{v: v, idx: i}, v.seq, p.dql))
	}
	return tea.Batch(cmds...)
}

func (v *homeView) SetTimeframe(tf catalog.Timeframe) tea.Cmd {
	v.tf = tf
	return v.refresh(true)
}

// fixedPanels names the panels that ignore `t`, with their windows
// ("problems 24h").
func (v *homeView) fixedPanels() []string {
	var out []string
	for _, p := range v.panels {
		if tf, fixed := p.window(v.tf); fixed {
			out = append(out, p.def.ID+" "+tf.Label)
		}
	}
	return out
}

// differing counts the panels whose window is not the header's — the ones
// whose title carries the ◷ pill.
func (v *homeView) differing() int {
	n := 0
	for _, p := range v.panels {
		if tf, _ := p.window(v.tf); tf.DQL() != v.tf.DQL() {
			n++
		}
	}
	return n
}

func (v *homeView) InputActive() bool { return false }
func (v *homeView) Crumb() string     { return "home" }

// Busy reports whether any panel is still loading (animates the spinner).
func (v *homeView) Busy() bool {
	for _, p := range v.panels {
		if p.loading {
			return true
		}
	}
	return false
}

func (v *homeView) DQL() string {
	if v.focus < len(v.panels) {
		return v.panels[v.focus].dql
	}
	return ""
}

func (v *homeView) Echo() string { return v.ds.echoQuery(v.DQL()) }

func (v *homeView) Hints() []keyHint {
	return []keyHint{{"enter", "open"}, {"tab", "next panel"}, {"j/k", "move"}}
}

func (v *homeView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case bodySizeMsg:
		v.width, v.height = msg.width, msg.height
		return nil

	case dataMsg:
		po, ok := msg.owner.(panelOwner)
		if !ok || po.v != v || po.idx >= len(v.panels) || msg.seq != v.panels[po.idx].seq {
			return nil
		}
		p := v.panels[po.idx]
		p.loading = false
		p.err = msg.err
		if msg.err == nil {
			p.records = msg.records
			if p.def.Post != nil {
				p.records = p.def.Post(p.records)
			}
		}
		return nil

	case tea.KeyMsg:
		return v.handleKey(msg)
	}
	return nil
}

func (v *homeView) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "tab":
		v.focus = (v.focus + 1) % len(v.panels)
		v.cursor = 0
	case "shift+tab":
		v.focus = (v.focus + len(v.panels) - 1) % len(v.panels)
		v.cursor = 0
	case "down", "j":
		v.moveCursor(1)
	case "up", "k":
		v.moveCursor(-1)
	case "enter":
		p := v.panels[v.focus]
		if v.cursor < len(p.records) {
			rec := p.records[v.cursor]
			// The jump carries the panel's window (widened to the global
			// one), or the selected row could filter to an empty list.
			tf, _ := p.window(v.tf)
			j := p.def.Jump(rec, catalog.Scope{Timeframe: catalog.HomeJumpWindow(tf, v.tf)})
			return func() tea.Msg { return homeJumpMsg(j) }
		}
	}
	return nil
}

// homeJumpMsg turns a catalog.HomeJump into the app's navigation message.
func homeJumpMsg(j catalog.HomeJump) tea.Msg {
	if j.Detail != nil {
		return detailMsg{entity: *j.Detail}
	}
	return pushViewMsg{spec: catalog.Lookup(j.View), scope: j.Scope, filter: j.Filter}
}

// moveCursor walks lines within the focused panel and rolls over to the
// neighboring panel at the edges, so j/k tours the whole page.
func (v *homeView) moveCursor(delta int) {
	v.cursor += delta
	if v.cursor < 0 {
		v.focus = (v.focus + len(v.panels) - 1) % len(v.panels)
		v.cursor = maxInt(len(v.panels[v.focus].records)-1, 0)
		return
	}
	if v.cursor >= len(v.panels[v.focus].records) && v.cursor > 0 {
		v.focus = (v.focus + 1) % len(v.panels)
		v.cursor = 0
	}
}

func (v *homeView) View(width, height int) string {
	v.width, v.height = width, height

	var banner string
	if v.notice != "" {
		banner = ansi.Truncate(theme.Error.Render("✗ home config ignored — "+v.notice)+
			theme.Dim.Render(" · default layout shown · check with: dtui workspace validate"), width, "…")
		height--
	}

	cfgs := make([]HomePanelConfig, len(v.panels))
	for i, p := range v.panels {
		cfgs[i] = HomePanelConfig{ID: p.def.ID, Right: p.right}
	}
	cols := HomeColumns(cfgs, width >= homeWideMin)
	panelW := width/len(cols) - 1
	blocks := make([]string, len(cols))
	for c, col := range cols {
		panelH := maxInt(height/len(col), 4)
		stack := make([]string, len(col))
		for r, idx := range col {
			stack[r] = v.renderPanel(idx, v.panels[idx], panelW, panelH)
		}
		blocks[c] = strings.Join(stack, "\n")
	}

	var out []string
	if banner != "" {
		out = append(out, banner)
	}
	out = append(out, joinHorizontal(blocks, panelW))
	return strings.Join(out, "\n")
}

// panelDot renders a catalog.HomePanel.Dot color ("error", "series:3").
func panelDot(color string) string {
	if n, ok := strings.CutPrefix(color, "series:"); ok {
		i, _ := strconv.Atoi(n)
		return theme.SeriesAt(i).Render("●")
	}
	return theme.Class(color, "●")
}

// windowLabel is the panel title's window: dim when it is the header's
// window, a ◷ pill when it differs — the title never hides that a panel
// looks at another window than the header claims (ADR-0007).
func (v *homeView) windowLabel(p *homePanel) string {
	tf, _ := p.window(v.tf)
	if tf.DQL() == v.tf.DQL() {
		return theme.Dim.Render("· " + tf.Label)
	}
	return theme.Badge.Render("◷ " + tf.Label)
}

// renderPanel draws one panel as a rounded box with the title embedded in the
// top border; the focused panel's border lights up in the accent color.
func (v *homeView) renderPanel(idx int, p *homePanel, width, height int) string {
	border := theme.PanelBlur
	title := theme.PanelTitle2.Render(p.def.Title)
	if idx == v.focus {
		border = theme.PanelFocus
		title = theme.PanelTitle.Render(p.def.Title)
	}
	head := panelDot(p.def.Dot) + " " + title + " " + v.windowLabel(p)
	if !p.loading && p.err == nil && len(p.records) > 0 {
		head += theme.Dim.Render(fmt.Sprintf(" · %d", len(p.records)))
	}
	head = ansi.Truncate(head, max(width-7, 1), "…")
	fill := width - lipgloss.Width(head) - 5
	if fill < 0 {
		fill = 0
	}
	top := border.Render("╭─") + " " + head + " " + border.Render(strings.Repeat("─", fill)+"╮")

	innerW := width - 4
	var body []string
	switch {
	case p.loading:
		body = append(body, theme.Spinner.Render(theme.Spin()+" loading…"))
	case p.err != nil:
		body = append(body, theme.Error.Render(ansi.Truncate("✗ "+p.err.Error(), innerW, "…")))
	case len(p.records) == 0:
		body = append(body, theme.StatusOK.Render("✓ nothing to report"))
	default:
		for li, rec := range p.records {
			if len(body) >= height-2 {
				break
			}
			text, class := p.def.Line(rec)
			text = ansi.Truncate(catalog.SafeText(text), innerW, "…")
			if idx == v.focus && li == v.cursor {
				text = theme.Selected.Render(pad(text, innerW))
			} else if class != "" {
				text = theme.Class(class, text)
			}
			body = append(body, text)
		}
	}
	for len(body) < height-2 {
		body = append(body, "")
	}

	side := border.Render("│")
	lines := []string{top}
	for _, l := range body[:height-2] {
		lines = append(lines, side+" "+pad(l, innerW)+" "+side)
	}
	lines = append(lines, border.Render("╰"+strings.Repeat("─", max(width-2, 0))+"╯"))
	return strings.Join(lines, "\n")
}

// joinHorizontal places panel blocks side by side.
func joinHorizontal(blocks []string, width int) string {
	if len(blocks) == 1 {
		return blocks[0]
	}
	split := make([][]string, len(blocks))
	maxLines := 0
	for i, b := range blocks {
		split[i] = strings.Split(b, "\n")
		if len(split[i]) > maxLines {
			maxLines = len(split[i])
		}
	}
	var out []string
	for l := 0; l < maxLines; l++ {
		var parts []string
		for i := range split {
			line := ""
			if l < len(split[i]) {
				line = split[i][l]
			}
			parts = append(parts, pad(line, width))
		}
		out = append(out, strings.Join(parts, " "))
	}
	return strings.Join(out, "\n")
}
