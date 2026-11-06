package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dynatrace-oss/dtui/internal/tui"
	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
	"github.com/dynatrace-oss/dtui/internal/workspace"
)

var workspaceCmd = &cobra.Command{
	Use:   "workspace",
	Short: "Inspect the project's .dtui.yaml workspace file",
}

var workspaceValidateCmd = &cobra.Command{
	Use:   "validate [path]",
	Short: "Check a .dtui.yaml and show what it configures",
	Long: `Validate a .dtui.yaml workspace file and print what dtui will do with it.

Without a path, the file is discovered like dtui does at launch: walking up
from the working directory. Every problem is listed with its line and YAML
path. Exits non-zero when dtui would ignore any part of the file; unknown
top-level keys are warnings (they are ignored, for forward compatibility).

Validation is offline: segment names and the environment's context are only
resolved against dtctl's config and the tenant when dtui launches.`,
	Example: `  dtui workspace validate
  dtui workspace validate ./services/checkout/.dtui.yaml`,
	Args:          cobra.MaximumNArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		path := ""
		if len(args) == 1 {
			path = args[0]
		} else {
			found, ok := workspace.Find()
			if !ok {
				cwd, _ := os.Getwd()
				fmt.Fprintf(cmd.ErrOrStderr(), "✗ no %s found in %s or any parent directory\n", workspace.FileName, cwd)
				return errInvalidWorkspace
			}
			path = found
		}
		if !reportWorkspace(cmd.OutOrStdout(), path) {
			return errInvalidWorkspace
		}
		return nil
	},
}

var errInvalidWorkspace = errors.New("invalid workspace file")

// homeLayout checks the parsed home block against the panel catalog. Any
// finding rejects the whole layout — a half-applied home would look
// intentional — and dtui shows the default panels with a warning.
func homeLayout(ws *workspace.Workspace) ([]tui.HomePanelConfig, []workspace.Issue) {
	if ws == nil || (ws.Home == nil && len(ws.HomeIssues) == 0) {
		return nil, nil
	}
	issues := append([]workspace.Issue(nil), ws.HomeIssues...)
	if ws.Home == nil {
		return nil, issues
	}
	if n := len(ws.Home.Panels); n > catalog.MaxHomePanels {
		issues = append(issues, workspace.Issue{Line: ws.Home.Panels[0].Line, Path: "home",
			Msg: fmt.Sprintf("%d panels listed — max %d (one query each)", n, catalog.MaxHomePanels)})
	}
	var out []tui.HomePanelConfig
	for _, p := range ws.Home.Panels {
		if catalog.LookupHomePanel(p.Panel) == nil {
			issues = append(issues, workspace.Issue{Line: p.Line, Path: p.Path(),
				Msg: fmt.Sprintf("unknown panel %q — available: %s", p.Panel, strings.Join(catalog.HomePanelIDs(), ", "))})
			continue
		}
		out = append(out, tui.HomePanelConfig{ID: p.Panel, Timeframe: p.Timeframe, Right: p.Column == workspace.ColumnRight})
	}
	if len(issues) > 0 {
		sort.SliceStable(issues, func(i, j int) bool { return issues[i].Line < issues[j].Line })
		return nil, issues
	}
	return out, nil
}

// homeNotice condenses rejected-layout issues into the one line the home
// view keeps on screen.
func homeNotice(issues []workspace.Issue) string {
	if len(issues) == 0 {
		return ""
	}
	s := issues[0].Error()
	if n := len(issues) - 1; n > 0 {
		s += fmt.Sprintf(" (+%d more)", n)
	}
	return s
}

// reportWorkspace prints a validation report for one workspace file and
// reports whether dtui would apply all of it.
func reportWorkspace(w io.Writer, path string) bool {
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	fmt.Fprintf(w, "%s\n\n", path)
	ws, err := workspace.Load(path)
	if err != nil {
		fmt.Fprintf(w, "  ✗ %s\n\n", strings.TrimPrefix(err.Error(), path+": "))
		fmt.Fprintln(w, "✗ invalid — dtui ignores this whole file at launch (startup warning)")
		return false
	}

	row := func(mark, key, val string) { fmt.Fprintf(w, "  %s %-13s %s\n", mark, key, val) }
	unset := "(not set)"
	or := func(v, def string) string {
		if v == "" {
			return def
		}
		return v
	}
	row("✓", "version", or(fmt.Sprint(ws.Version), unset))
	row("✓", "environment", or(ws.Environment, unset+" — uses the current dtctl context"))
	viewOK := ws.View == "" || isBespokeView(ws.View) || catalog.Lookup(ws.View) != nil
	switch {
	case ws.View == "":
		row("✓", "view", unset+" — starts on home")
	case viewOK:
		row("✓", "view", ws.View)
	default:
		row("✗", "view", fmt.Sprintf("unknown view %q — dtui starts on home (see dtui --help for views)", ws.View))
	}
	tf := catalog.DefaultTimeframe
	if t, ok := catalog.ParseTimeframe(ws.Timeframe); ok {
		tf = t
	}
	row("✓", "timeframe", or(ws.Timeframe, unset+" — "+tf.Label))
	if len(ws.Segments) > 0 {
		refs := make([]string, len(ws.Segments))
		for i, s := range ws.Segments {
			refs[i] = s.Ref
		}
		row("✓", "segments", fmt.Sprintf("%d: %s (resolved against the tenant at launch)", len(refs), strings.Join(refs, ", ")))
	} else {
		row("✓", "segments", unset)
	}
	row("✓", "primary-tags", or(strings.Join(ws.PrimaryTags, ", "), unset))

	panels, issues := homeLayout(ws)
	switch {
	case len(issues) > 0:
		row("✗", "home", "rejected — dtui shows the default home layout with a warning")
		for _, is := range issues {
			fmt.Fprintf(w, "      line %-3d %s\n", is.Line, is.Error())
		}
	case panels == nil:
		row("✓", "home", unset+" — default layout")
	default:
		row("✓", "home", fmt.Sprintf("%d panels", len(panels)))
	}
	for _, u := range ws.UnknownKeys {
		fmt.Fprintf(w, "  ! line %-3d %s\n", u.Line, u.Error())
	}

	layout := "home layout"
	if len(issues) > 0 {
		layout = "home layout (default — the configured one is rejected)"
	}
	fmt.Fprintf(w, "\n%s, header window last %s:\n", layout, tf.Label)
	writeHomeLayout(w, panels, tf)

	errs := len(issues)
	if !viewOK {
		errs++
	}
	fmt.Fprintln(w)
	switch {
	case errs > 0:
		fmt.Fprintf(w, "✗ %s, %s — dtui applies the rest of the file\n", plural(errs, "error"), plural(len(ws.UnknownKeys), "warning"))
		return false
	case len(ws.UnknownKeys) > 0:
		fmt.Fprintf(w, "✓ valid, %s\n", plural(len(ws.UnknownKeys), "warning"))
	default:
		fmt.Fprintln(w, "✓ valid")
	}
	return true
}

// writeHomeLayout lists the panels column by column as the home view
// renders them on a wide terminal, with each one's effective window and
// whether it follows `t`.
func writeHomeLayout(w io.Writer, panels []tui.HomePanelConfig, global catalog.Timeframe) {
	if panels == nil {
		panels = tui.DefaultHomePanels()
	}
	cols := tui.HomeColumns(panels, true)
	names := []string{"left", "right"}
	if len(cols) == 1 {
		names = []string{"full"}
	}
	for c, col := range cols {
		for r, i := range col {
			p := panels[i]
			def := catalog.LookupHomePanel(p.ID)
			tf, fixed := catalog.HomeWindow(def, p.Timeframe, global)
			note := "follows t"
			if fixed {
				note = "fixed"
				if tf.DQL() != global.DQL() {
					note += " · ◷ differs from header"
				}
			}
			label := ""
			if r == 0 {
				label = names[c]
			}
			fmt.Fprintf(w, "  %-5s  %d. %-18s %-5s %s\n", label, r+1, def.ID, tf.Label, note)
		}
	}
	if len(cols) == 1 && len(panels) > 1 {
		fmt.Fprintln(w, "  (one full-width column — add column: right to an entry for a second column)")
	}
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
