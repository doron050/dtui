package tui

import (
	"io"

	tea "github.com/charmbracelet/bubbletea"
)

// RunWithIO launches the TUI using explicit input/output instead of the
// process stdio. This is what environments without a real TTY use — notably
// the wasm/xterm.js browser bridge, which feeds keystrokes through in and
// receives ANSI output through out.
//
// ready, if non-nil, is called with the constructed program before it starts,
// so the caller can retain the handle for p.Send (e.g. to inject
// tea.WindowSizeMsg on terminal resize).
func RunWithIO(opts Options, in io.Reader, out io.Writer, ready func(*tea.Program)) error {
	app, err := newApp(opts)
	if err != nil {
		return err
	}
	p := tea.NewProgram(
		app,
		tea.WithAltScreen(),
		tea.WithInput(in),
		tea.WithOutput(out),
	)
	if ready != nil {
		ready(p)
	}
	_, err = p.Run()
	return err
}
