package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
	"github.com/dynatrace-oss/dtui/internal/tui/theme"
)

// cmdPalette is the ':' command bar: a fuzzy view finder whose input renders
// in the header line and whose matches render as a picker near the top.
type cmdPalette struct {
	app     *app
	input   textinput.Model
	matches []*catalog.Spec
	sel     int
}

// cmdPalettePage is how far pgup/pgdown jump the selection through the match
// list — a coarse step for the long, unfiltered view list.
const cmdPalettePage = 10

// bespokeSpecs are the command palette's entries for the non-catalog screens,
// so :nav and friends are discoverable by scanning or typing. Display-only
// stubs — deliberately NOT in the catalog registry (Lookup must never hand
// them to newTableView; the enter path routes them by name).
var bespokeSpecs = []*catalog.Spec{
	{Name: "home", Desc: "Triage landing page"},
	{Name: "query", Aliases: []string{"dql"}, Desc: "DQL escape hatch"},
	{Name: "nav", Aliases: []string{"smartscape", "navigator"}, Desc: "Smartscape topology navigator"},
	{Name: "segments", Aliases: []string{"seg"}, Desc: "Filter segments — global DQL scope"},
	{Name: "ctx", Aliases: []string{"context"}, Desc: "Switch dtctl context (session-local)"},
}

// openCmdPalette shows the command bar (':').
func (a *app) openCmdPalette() tea.Cmd {
	ci := newTextInput()
	ci.Prompt = ":"
	ci.PromptStyle = theme.Crumb
	ci.CharLimit = 64
	ci.Focus()
	p := &cmdPalette{app: a, input: ci}
	p.updateMatches()
	a.overlay = p
	return textinput.Blink
}

func (p *cmdPalette) Hints() []keyHint {
	return []keyHint{{"enter", "open"}, {"tab", "next match"}, {"esc", "cancel"}}
}

func (p *cmdPalette) HandleKey(msg tea.KeyMsg) tea.Cmd {
	a := p.app
	switch msg.String() {
	case "esc":
		a.overlay = nil
		return nil
	case "tab", "down":
		if len(p.matches) > 0 {
			p.sel = (p.sel + 1) % len(p.matches)
		}
		return nil
	case "shift+tab", "up":
		if len(p.matches) > 0 {
			p.sel = (p.sel + len(p.matches) - 1) % len(p.matches)
		}
		return nil
	case "pgdown":
		p.sel = clampSel(p.sel+cmdPalettePage, len(p.matches))
		return nil
	case "pgup":
		p.sel = clampSel(p.sel-cmdPalettePage, len(p.matches))
		return nil
	case "enter":
		a.overlay = nil
		input := strings.Fields(strings.TrimSpace(p.input.Value()))
		if len(input) == 0 {
			// Empty input still has a highlighted suggestion (tab/down cycles
			// it) — enter opens it rather than silently dropping the choice.
			// Route through openSpec so a highlighted bespoke entry (:ctx,
			// :segments, :nav …) lands on its handler instead of being fed to
			// viewFor as an unknown view.
			if p.sel < len(p.matches) {
				return a.openSpec(p.matches[p.sel], "")
			}
			return nil
		}
		arg := strings.Join(input[1:], " ")
		switch input[0] {
		case "q", "quit":
			return a.quit()
		case "help":
			return a.openHelp()
		case "history", "hist":
			return a.openHistory()
		case "home", "query", "dql":
			return a.jumpTo(input[0], "")
		case "nav", "smartscape", "navigator":
			return a.openNav(arg)
		case "trace":
			if arg == "" {
				return statusErr("usage: trace <trace-id>")
			}
			return func() tea.Msg { return waterfallMsg{traceID: arg} }
		case "segments", "seg":
			return a.openSegmentPicker()
		case "ctx", "context":
			return a.switchContext(arg)
		}
		// Honor the highlighted match (WYSIWYG): the picker shows what enter
		// opens. An exact name/alias Lookup is only the fallback for a token
		// that surfaces no match — never a silent override of the visible
		// selection. (Otherwise a token that is an exact alias of some *other*
		// view could open that view while the picker highlighted a different
		// one — see MatchSpecs's exact-match tier.)
		var spec *catalog.Spec
		if p.sel < len(p.matches) {
			spec = p.matches[p.sel]
		}
		if spec == nil {
			spec = catalog.Lookup(input[0])
		}
		if spec == nil {
			return statusErr(fmt.Sprintf("unknown view %q", input[0]))
		}
		return a.openSpec(spec, arg)
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	p.updateMatches()
	return cmd
}

// openSpec routes a resolved palette entry — whether the user typed it or
// highlighted it with tab/down — to its handler. Bespoke entries (:nav,
// :segments, :ctx …) are display-only stubs that are NOT in the catalog
// registry, so they must never reach viewFor (it would report them as unknown
// views); everything else is a real catalog view opened via jumpTo, with any
// argument narrowing the jump (":pods checkout" pre-fills the filter).
func (a *app) openSpec(spec *catalog.Spec, arg string) tea.Cmd {
	switch spec.Name {
	case "home", "query":
		return a.jumpTo(spec.Name, "")
	case "nav":
		return a.openNav(arg)
	case "segments":
		return a.openSegmentPicker()
	case "ctx":
		return a.switchContext(arg)
	}
	return a.jumpTo(spec.Name, arg)
}

func (p *cmdPalette) updateMatches() {
	first := ""
	if fields := strings.Fields(p.input.Value()); len(fields) > 0 {
		first = fields[0]
	}
	candidates := append(append([]*catalog.Spec{}, bespokeSpecs...), catalog.All()...)
	p.matches = catalog.MatchSpecs(candidates, first)
	p.sel = 0
}

// View renders the matches as a picker near the top (the input itself lives
// in the header line): view name, aliases, and description, with the current
// suggestion highlighted.
func (p *cmdPalette) View(width, height int) string {
	var b strings.Builder
	b.WriteString(theme.OverlayTitle.Render("views") + "\n\n")
	nameW, aliasW, descW := 14, 12, 36
	rowW := nameW + aliasW + descW + 3
	if len(p.matches) == 0 {
		b.WriteString(theme.Dim.Render(" no matching view") + "\n")
	}
	// Window around the selection (not the top) so tab/pgdn can walk the whole
	// list — the old fixed [0,limit) window left the highlight and any "N more"
	// rows off-screen and unreachable.
	overlayRows(&b, len(p.matches), p.sel, max(height-8, 4), func(i int) string {
		s := p.matches[i]
		row := pad(s.Name, nameW) + " " + pad(strings.Join(s.Aliases, " "), aliasW) + " " + pad(s.Desc, descW)
		if i == p.sel {
			return theme.Selected.Render(pad(" "+row, rowW))
		}
		return " " + theme.HeaderVal.Render(pad(s.Name, nameW)) + " " +
			theme.Dim.Render(pad(strings.Join(s.Aliases, " "), aliasW)) + " " +
			theme.CrumbDim.Render(pad(s.Desc, descW))
	})
	b.WriteString("\n" + theme.Dim.Render("tab next · enter open · also :history :trace <id>"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Position(0.2),
		theme.OverlayBox.Render(b.String()))
}
