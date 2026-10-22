# dtui Chrome extension

Runs the real dtui TUI in a Chrome side panel, compiled to WebAssembly — plus
a pop-out tab / detached window and configurable web-page deep links. See
`docs/design/WASM_CHROME_EXTENSION.md` for the architecture, build, and the
remaining hardening before a public release.

## Build & load

```bash
# 1. Build the wasm (produces wasm/ui/dtui.wasm + wasm_exec.js)
./wasm/build.sh

# 2. Stage the wasm, loader, and xterm into the extension
./extension/wire.sh

# 3. Load unpacked in Chrome
#    chrome://extensions → Developer mode → Load unpacked → select extension/
```

Then right-click the extension icon → **Options**, set your tenant URL and a
read-only platform token, and click the icon to open the side panel.

## Deep links from a web page

The extension can jump the side-panel TUI to a specific view with an id
pre-applied as a filter (see `docs/design/WEB_DEEPLINK.md`):

- **In-panel tester (Alt+G):** a small overlay to fire a jump by hand — pick a
  view (e.g. `traces`), a field (`dt.rum.session.id`), and paste a value.
- **Page buttons (configurable, generic):** rules in **Options → Deep-link
  rules** (stored in `chrome.storage.local`) decide which sites get a dtui
  button and where it jumps. A rule matches by Chrome match pattern and reads
  the id from the **URL** (`urlRegex`, optional `decode`) or the **DOM**
  (`selector`/`attribute`). Each action opens a `target` view, optionally with a
  `lens` and a wider `timeframe`, filtered by `field == id`. The background
  registers `content.js` for exactly the matched hosts, so adding a site is a
  config edit — no manifest change.
- **No customer specifics baked in.** The extension ships with **no** rules.
  Each app provides its own bundle — import it on the Options page (URL or file).
  See [`examples/`](./examples/) for the schema and a worked example bundle
  (`examples/astroshop.rules.json`), which an application should ultimately host
  in its own repo and edit to point at its own tenant URL.

## Status

- **Runs in Chrome**: the side panel renders the full TUI, keyboard/resize/paste
  work, and live DQL queries round-trip against a real tenant via the
  `chrome.storage` token. Pop-out tab/window and web deep links work.
- **Also covered headlessly** (`node wasm/smoke.mjs`): the wasm links,
  instantiates, boots Bubble Tea, registers input/resize callbacks, drives the
  JS HTTP transport for real DQL queries, and renders the home view.
- **Remaining hardening** (see the design doc): OAuth/PKCE instead of a stored
  bearer token, binary-size trims, upstreaming the js/wasm build stubs, and a
  page-clipboard bridge for copy-out.

## Files

| File | Role |
|------|------|
| `manifest.json` | MV3 manifest: side panel, `storage`, host permissions |
| `background.js` | Opens the side panel on toolbar click; deep-link handoff |
| `content.js` | Injects dtui deep-link buttons on matching pages (config-driven) |
| `default-rules.js` | Generic engine seed (no customer rules) + the example template |
| `examples/` | Importable rule bundles (e.g. `astroshop.rules.json`) + schema README |
| `icons/` | The `▛▞▟` extension logo (svg source + rasterized png sizes) |
| `sidepanel.{html,js}` | xterm terminal + the wasm/JS bridge + authenticated fetch |
| `settings.{html,js}` | Stores tenant URL + token in `chrome.storage.local` |
| `fetch-xterm.sh` | Vendors xterm.js into `vendor/` (gitignored) |
| `wire.sh` | Copies the built wasm + loader in, runs `fetch-xterm.sh` |
| `dtui.wasm`, `wasm_exec.js`, `vendor/` | Build artifacts (gitignored) |
