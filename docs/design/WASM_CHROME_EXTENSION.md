# dtui as a Chrome extension via WebAssembly

Status: **working.** The real dtui TUI runs in a Chrome side panel (and a
pop-out tab / detached window) compiled to `js/wasm`. This documents the
architecture, the build, and the remaining hardening before a public release.

## Goal

Run the real dtui TUI inside a Chrome side panel, compiled to
`GOOS=js GOARCH=wasm`, with an xterm.js terminal for I/O — reusing
`internal/tui` and `internal/tui/catalog` unchanged, and adding auth at the
extension boundary rather than reading dtctl's config + OS keyring.

## Prior art

An earlier prototype already runs dtui as WASM, but
as a **Dynatrace App on AppEngine**, not an extension. It answered the hard
feasibility question — bubbletea + dtctl *do* run in a browser via WASM — and
this work retargets that approach to a Chrome extension. The key architectural
moves are borrowed from it; the credential/HTTP boundary is where an extension
necessarily differs (no AppEngine runtime to own auth).

## Architecture

```
 xterm.js (side panel page)
   │  keystrokes / ANSI / resize      via window.dtui* globals
   ▼
 sidepanel.js  ── window.dtuiFetch (adds Authorization from chrome.storage) ──▶ tenant
   │  instantiates
   ▼
 dtui.wasm  =  cmd/web (js,wasm)  →  internal/tui.RunWithIO  →  the real TUI
                     │
                     └─ dtctl session.Client with its http.RoundTripper swapped
                        to delegate every request to window.dtuiFetch
```

Three seams make this work, and only the third differs from the native binary:

1. **TTY-free entry** — `internal/tui/run_io.go` adds `RunWithIO(opts, in, out,
   ready)`, building the Bubble Tea program with `WithInput/WithOutput/
   WithAltScreen` instead of process stdio. `internal/tui` is otherwise
   untouched; the "no HTTP in the model, every call is a tea.Cmd" layering
   (CLAUDE.md) is exactly what lets the model port with zero changes.

2. **Terminal bridge** — `cmd/web/main.go` forwards Bubble Tea's `io.Writer` to
   an xterm.js write callback and feeds a channel-backed `io.Reader` from
   xterm's keystrokes; resize arrives as an injected `tea.WindowSizeMsg`.

3. **Auth at the boundary** — `cmd/web/http_wasm.go` replaces the dtctl
   `session.Client` transport with a `RoundTripper` that hands each request to
   JS `dtuiFetch`. **Go never sees a token.** In the extension,
   `sidepanel.js` supplies the tenant base URL + token (from
   `chrome.storage.local`) and relies on the extension's `host_permissions` to
   make the cross-origin call without a CORS preflight. (The Dynatrace-App
   version instead let AppEngine's `httpClient` own auth — same seam, different
   filler.)

### Surfaces (side panel · tab · detached window)

`sidepanel.html`/`sidepanel.js` are **host-agnostic**: they read config + token
from `chrome.storage.local`, fetch straight from the extension page (which
carries `host_permissions`), and run the wasm. Nothing binds them to the side
panel, so the *same page* is reused as a full browser tab
(`chrome.tabs.create`) or a detached floating window
(`chrome.windows.create({type:"popup"})`) — the pop-out buttons in the panel's
top-right. Each surface is an independent dtui session (its own wasm
instance/scope); "pop out" opens a bigger one, it does not relocate the panel.
A detached surface holds a `dtui-surface` keep-alive port so a deep-link
prefers it (via the live `dtui-nav` broadcast) over popping the side panel on
top of the user's window (see `background.js`). A truly Chrome-independent OS
window isn't possible for an extension — the popup window is as detached as it
gets.

## The build problem and how the build solves it

Against this repo's **current** dtctl pins, `GOOS=js GOARCH=wasm go build
./cmd/web` fails to compile: three upstream modules ship no js/wasm build
support.

| Module | Gap on js/wasm | js/wasm stub |
|--------|----------------|-----------|
| `charmbracelet/bubbletea` | no `initInput`/`openInputTTY`/`suspendProcess`/`listenForResize`/`suspendSupported` | `wasm/patches/bubbletea_tty_wasm.go.txt` |
| `atotto/clipboard` | `readAll`/`writeAll` undefined (platform files exclude js) | `wasm/patches/clipboard_js.go.txt` |
| `dynatrace-oss/dtctl` (sdk) | `session/refresh_lock_unix.go` is `//go:build !windows`, so it wrongly compiles on js and needs `x/sys/unix` (Flock, …) | retag `!windows && !js` + `refresh_lock_js` stub |
| `dynatrace-oss/dtctl` (pkg/output) | `live_{unix,windows}.go` define resize hooks for no js variant | `dtctl_output_live_js.go.txt` |

> Note: the two dtctl gaps are **newer than the prior prototype's snapshot** — its older
> vendored dtctl predated the OAuth refresh file-lock and the live-printer
> resize signal, so it only needed the bubbletea + clipboard patches. This is
> exactly the version-skew risk of forking dtctl.

`go build -overlay` can't help (it refuses replacements under `GOMODCACHE`), and
editing go.mod `replace` directives would pollute the native build/CI. So
`wasm/build.sh`:

1. copies just these four modules into `wasm/.deps/` (gitignored, ~11 MB),
2. applies the stubs there,
3. wires them via a **scoped workspace** `wasm/go.work.wasm` selected with
   `GOWORK=…` **only** for the wasm build.

The repo's `go.mod`, `go build ./...`, `make test`, and CI never set `GOWORK`
and never see `wasm/.deps`, so they are completely unaffected.

## What works

- **Runs in Chrome.** The extension loads unpacked, the side panel renders the
  full TUI in xterm.js, keyboard input / resize / clipboard paste work, and live
  DQL queries round-trip against a real tenant using the `chrome.storage.local`
  token. The panel also pops out into a full tab or a detached window.
- **Configurable web deep links.** Rule bundles imported on the Options page add
  dtui jumps to any page (url/dom/header/request sources); handled end-to-end
  into the panel (see `WEB_DEEPLINK.md`).
- **Compiles** to `js/wasm`. Artifact **~51 MB raw / ~10 MB gzipped** (what the
  browser downloads). No size trims applied yet — the prior prototype's glamour/chroma
  skip + CLI-formatter trim get the raw build to ~18 MB, and `wasm-opt -Oz` to
  ~17 MB; both are drop-in follow-ups.
- **Boots headlessly** (`node wasm/smoke.mjs`, `SMOKE: PASS`): instantiates,
  starts Bubble Tea, fires `dtuiReady`, registers `dtuiSendInput` /
  `dtuiResize`, drives `dtuiFetch` for real DQL `query:execute` calls
  against a stubbed tenant, and renders the home triage view.

## Remaining hardening before a public release

1. **Credentials.** The extension currently stores a bearer token in `chrome.storage.local` —
   device-local but weaker than the OS keyring the native binary uses. A real
   extension should prefer an OAuth authorization-code (PKCE) flow via
   `chrome.identity`, keeping only short-lived access tokens, and scope the
   token read-only (aligns with ADR-0011 — dtui is read-only, so no write
   scopes are ever needed; a genuinely reduced blast radius).
2. **Binary size.** Apply the two size trims; decide whether ~7–10 MB gzipped is
   acceptable for a first load, and whether to lazy-load the wasm.
3. **Upstreaming vs. forking.** The clean fix for the four gaps is `//go:build
   js` variants **upstream** — trivial for dtctl (Dynatrace-owned) and worth a
   PR to bubbletea/clipboard. That would delete `wasm/.deps` + the scoped
   workspace entirely and let `cmd/web` build with a plain overlay-free command.
   Keep upstream changes shared with any other WASM consumer (ideally `run_io.go` + the build-tagged trims land in
   this repo rather than being forked twice).
4. **Duplicated sources.** `cmd/web/sources.go` copies the repo-root
   `sources.go`. Extract the source constructors into an internal package shared
   by the native main and `cmd/web`.
5. **Copy-out (`c` / `y`).** `o` (open in Dynatrace UI) is wired to the host via
   `window.dtuiOpenURL` → `chrome.tabs.create`, and clipboard *paste* works.
   But copy-*out* (`c` copy dtctl command / `y` yank) is still a no-op in the
   browser build — the OS clipboard is unavailable and no page-clipboard bridge
   (`navigator.clipboard.writeText`) is wired yet. Add a `window.dtuiCopy`
   seam so `c`/`y` reach the page clipboard.

## Files added

Committed (in the main module, all behind `//go:build js && wasm` where Go):

- `internal/tui/run_io.go` — the TTY-free entrypoint (native-safe, no build tag).
- `cmd/web/{main,http_wasm,sources}.go` — the wasm entrypoint + JS transport.
- `wasm/build.sh`, `wasm/smoke.mjs`, `wasm/patches/*.go.txt` — build + stubs + smoke test.
- `extension/*` — the MV3 extension shell + staging scripts.

Gitignored (regenerated): `wasm/.deps/`, `wasm/go.work.wasm`, `wasm/ui/*`,
`extension/vendor/`, `extension/dtui.wasm`, `extension/wasm_exec.js`.
