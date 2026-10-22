//go:build js && wasm

// Command dtuiweb runs the real dtui TUI in the browser, compiled to
// WebAssembly. It bridges Bubble Tea's terminal I/O to an xterm.js instance in
// the page and routes every dtctl HTTP call out to JavaScript, where the host
// (a Dynatrace app runtime, or a Chrome extension service worker) supplies
// authentication — so no dtctl config, keyring, or login is involved.
//
// See docs/design/WASM_CHROME_EXTENSION.md: the full TUI compiles to js/wasm
// and boots against a JS-supplied transport.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"syscall/js"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/dynatrace-oss/dtctl/pkg/exec"
	"github.com/dynatrace-oss/dtctl/pkg/resources/segment"
	"github.com/dynatrace-oss/dtctl/sdk/session"
	"github.com/dynatrace-oss/dtui/internal/tui"
)

// version identifies this browser build in tenant-side request logs.
var version = "web-dev"

// jsWriter forwards Bubble Tea's ANSI output to the xterm.js write callback.
type jsWriter struct{ fn js.Value }

func (w jsWriter) Write(p []byte) (int, error) {
	arr := js.Global().Get("Uint8Array").New(len(p))
	js.CopyBytesToJS(arr, p)
	w.fn.Invoke(arr)
	return len(p), nil
}

// openURL backs Options.OpenURL ('o' — open in the Dynatrace UI): a browser tab
// can't shell out, so hand the intent/app URL to the host's window.dtuiOpenURL
// (which opens it in a new tab). Errors surface as a TUI status line.
func openURL(u string) error {
	fn := js.Global().Get("dtuiOpenURL")
	if !fn.Truthy() {
		return errors.New("dtuiOpenURL not available in this host")
	}
	fn.Invoke(u)
	return nil
}

// chanReader is Bubble Tea's input, fed by keystrokes from xterm.js.
type chanReader struct {
	ch  chan []byte
	buf []byte
}

func (r *chanReader) Read(p []byte) (int, error) {
	if len(r.buf) == 0 {
		b, ok := <-r.ch
		if !ok {
			return 0, io.EOF
		}
		r.buf = b
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

func main() {
	// No real TTY to detect a color profile from — the browser terminal is
	// truecolor.
	lipgloss.SetColorProfile(termenv.TrueColor)

	cfg := js.Global().Get("dtuiConfig")
	environment := cfg.Get("environment").String()

	// Multi-environment: the host may pass a list of {name,url}. dtui lists
	// them for :ctx and switches between them in-session. Tokens stay in JS —
	// the fetch hook picks the right one per request host (see http_wasm.go and
	// sidepanel.js) so Go still never sees a token.
	contextName := "app"
	if c := cfg.Get("context"); c.Truthy() && c.String() != "" {
		contextName = c.String()
	}
	type webEnv struct{ name, url string }
	var envs []webEnv
	if list := cfg.Get("environments"); list.Truthy() {
		for i := 0; i < list.Length(); i++ {
			o := list.Index(i)
			envs = append(envs, webEnv{name: o.Get("name").String(), url: o.Get("url").String()})
		}
	}
	if len(envs) == 0 { // single-environment host: synthesize one entry
		envs = []webEnv{{name: contextName, url: environment}}
	}
	urlFor := func(name string) string {
		for _, e := range envs {
			if e.name == name {
				return e.url
			}
		}
		return ""
	}
	// resolveEnv maps a deep link's target environment — given as a tenant URL
	// (the portable, committed-in-the-app-repo form), a bare host, or a local
	// context name — to the configured context name to switch to. Matching a
	// tenant URL by host means a shared rule bundle never has to know the local
	// nickname a user gave that environment. Returns "" when the target is empty
	// (stay put), or the raw target when nothing matches (so the TUI can report
	// it as not configured rather than silently using the wrong tenant).
	hostOf := func(s string) string {
		if u, err := url.Parse(strings.TrimSpace(s)); err == nil && u.Hostname() != "" {
			return u.Hostname()
		}
		return strings.TrimSpace(s)
	}
	resolveEnv := func(target string) string {
		target = strings.TrimSpace(target)
		if target == "" {
			return ""
		}
		for _, e := range envs { // exact local name (dev tester / back-compat)
			if e.name == target {
				return e.name
			}
		}
		th := hostOf(target)
		for _, e := range envs { // by tenant host — the portable path
			if hostOf(e.url) == th {
				return e.name
			}
		}
		return target
	}

	// newClient builds a JS-transported client for an environment URL. The host
	// runtime authenticates platform calls for us; Go never sees a token.
	// NewClient still requires a non-empty one, so pass a placeholder — the JS
	// fetch hook (see http_wasm.go) overrides auth.
	newClient := func(url string) (*session.Client, error) {
		c, err := session.NewClient(url, "app-managed",
			session.WithUserAgentProduct("dtui-web", version))
		if err != nil {
			return nil, err
		}
		installJSTransport(c)
		return c, nil
	}

	client, err := newClient(environment)
	if err != nil {
		// No panic in the browser host: a Go panic aborts the wasm runtime and
		// leaves the panel blank with no explanation. Surface the failure on the
		// terminal and exit cleanly through the same hooks the normal shutdown
		// path uses, so the host can react.
		if w := js.Global().Get("dtuiTermWrite"); w.Truthy() {
			w.Invoke("dtui: failed to start: " + err.Error() + "\r\n")
		}
		if fn := js.Global().Get("dtuiExit"); fn.Truthy() {
			fn.Invoke()
		}
		return
	}

	reader := &chanReader{ch: make(chan []byte, 256)}
	writeFn := js.Global().Get("dtuiTermWrite")

	// SwitchContext rebuilds the tenant wiring for another named environment
	// (the :ctx command). Session-local by contract — nothing is persisted; the
	// active-environment choice lives on the Options page.
	names := make([]string, len(envs))
	for i, e := range envs {
		names[i] = e.name
	}
	switchContext := func(name string) (*tui.ContextWiring, error) {
		envURL := urlFor(name)
		if envURL == "" {
			return nil, fmt.Errorf("no configured environment matches %q — add it on the extension Options page", name)
		}
		c, err := newClient(envURL)
		if err != nil {
			return nil, err
		}
		return &tui.ContextWiring{
			ContextName:   name,
			Environment:   envURL,
			SafetyLevel:   "readonly",
			Executor:      exec.NewDQLExecutor(c),
			Sources:       tuiSources(c),
			SegmentSource: segmentSource(segment.NewHandler(c)),
		}, nil
	}

	opts := tui.Options{
		ContextName:   contextName,
		Environment:   environment,
		SafetyLevel:   "readonly",
		Executor:      exec.NewDQLExecutor(client),
		Sources:       tuiSources(client),
		SegmentSource: segmentSource(segment.NewHandler(client)),
		SwitchContext: switchContext,
		Contexts:      names,
		OpenURL:       openURL,
		InitialView:   "home",
		// No filesystem in the browser: history stays in memory this session.
		HistoryPath:      "",
		QueryHistoryPath: "",
	}

	reader2 := reader
	ready := func(p *tea.Program) {
		js.Global().Set("dtuiSendInput", js.FuncOf(func(_ js.Value, args []js.Value) any {
			// A malformed host call must not panic the callback and abort the wasm
			// runtime (which freezes the panel). The channel is buffered and always
			// drained by the reader goroutine, so the send doesn't block in practice.
			if len(args) == 0 {
				return nil
			}
			reader2.ch <- []byte(args[0].String())
			return nil
		}))
		js.Global().Set("dtuiResize", js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) < 2 {
				return nil
			}
			p.Send(tea.WindowSizeMsg{Width: args[0].Int(), Height: args[1].Int()})
			return nil
		}))
		// dtuiNavigate is the deep-link entry point: the host hands us a
		// JSON request (target view + server-side facets, or a trace id) and we
		// drive the TUI to it. See docs/design/WEB_DEEPLINK.md.
		js.Global().Set("dtuiNavigate", js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 {
				return nil
			}
			var req struct {
				Target    string `json:"target"`
				ID        string `json:"id"`
				Lens      string `json:"lens"`
				Timeframe string `json:"timeframe"`
				// Environment is the target tenant as a URL/host (portable) or a
				// local context name; resolved to a context name below.
				Environment string `json:"environment"`
				Facets      []struct {
					Field string `json:"field"`
					Value string `json:"value"`
				} `json:"facets"`
			}
			if err := json.Unmarshal([]byte(args[0].String()), &req); err != nil {
				return nil
			}
			facets := make([]tui.NavFacet, 0, len(req.Facets))
			for _, f := range req.Facets {
				facets = append(facets, tui.NavFacet{Field: f.Field, Value: f.Value})
			}
			p.Send(tui.NavigateMsg{
				Target: req.Target, ID: req.ID, Lens: req.Lens,
				Timeframe: req.Timeframe, Context: resolveEnv(req.Environment), Facets: facets,
			})
			return nil
		}))
		if fn := js.Global().Get("dtuiReady"); fn.Truthy() {
			fn.Invoke()
		}
	}

	go func() {
		_ = tui.RunWithIO(opts, reader, jsWriter{fn: writeFn}, ready)
		if fn := js.Global().Get("dtuiExit"); fn.Truthy() {
			fn.Invoke()
		}
	}()

	select {} // keep the Go runtime alive for the JS callbacks
}
