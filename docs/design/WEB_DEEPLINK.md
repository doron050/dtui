# Web-page deep links into dtui

**Status:** implemented, as part of the Chrome-extension WASM build
(`WASM_CHROME_EXTENSION.md`).

## The idea

You are looking at *some other* web page — an internal app, a
dashboard, anything — and it shows an identifier: a session id, a task id, a
Dynatrace entity id, a trace id in a response header. You want a one-click jump
into the matching dtui screen with that id **already applied as a filter** —
e.g. from an agent app's `session-id` straight to *the pod that ran that
session* or *its GenAI span*; from a `traceresponse` header straight to the
trace waterfall.

dtui runs in the side panel as wasm. This feature adds, in the *page* the
user is looking at, an affordance that hands an id to that side-panel program
and tells it which curated view to open, scoped to the id.

## Why this is a small change to dtui itself

Navigation in dtui is already a single primitive: `pushViewMsg{spec, scope,
facets, searches, filter}`. Every drill-down builds one. Crucially:

- `catalog.Facet{Field, Value}` composes to a **server-side** DQL
  `| filter <Field> == "<Value>"` (see `catalog/facets_test.go`, which already
  covers `trace.id` and `span.id` facets). That *is* the "pre-applied filter."
- Trace ids have an even more direct target: `waterfallMsg{traceID}` opens the
  trace waterfall page directly.
- Problem ids → `problemMsg`; entity ids → the entity's catalog view scoped via
  `scope.Entity`.

So dtui needs exactly one new **exported** message, `tui.NavigateMsg`, and a
case in `app.Update` translating it into the existing internal messages. No new
navigation model, no HTTP in `internal/tui`, and it is pure navigation —
squarely inside the read-only ADRs (0011/0012).

```go
// NavigateMsg is an external deep-link request (from the wasm host / extension).
type NavigateMsg struct {
    Target string        // catalog view name, or "trace" / "problem"
    Facets []NavFacet     // server-side field==value filters to pre-apply
    Timeframe string      // optional; default session timeframe otherwise
}
type NavFacet struct{ Field, Value string }
```

`cmd/web` registers `window.dtuiNavigate(json)` (same bridge style as
`dtuiSendInput`) and does `p.Send(tui.NavigateMsg{...})`.

## The hard/interesting part: making it configurable

The mapping *"this id, on this site, means these dtui targets"* is
domain-specific and must be user-editable — we cannot hardcode any one app. Model it
as a **rule list** stored in `chrome.storage.local`, edited as JSON on the
Options page (a friendlier form UI can come later).

```jsonc
{
  "rules": [
    {
      "name": "Agent task",
      "match": {
        "urlPattern": "https://*.myapp.example/*",  // which pages this applies to
        "source": "url",                          // url | dom | header
        // task id is the last path segment of /w/<ws>/tasks/<uuid>
        "regex": "/tasks/([0-9a-f-]{36})"        // capture group 1 = the id
      },
      "actions": [
        // Validated live against a Gen3 tenant (learnings.md §1.23): the id
        // lives as a span attribute, so the target is `traces` (fetch spans),
        // NOT `genai` (a Smartscape node list — the facet would silently miss).
        // The executing pod rides along on each span (k8s.pod.name /
        // dt.smartscape.k8s_pod), so "the pod" is one drill from here.
        { "label": "GenAI spans for this task",    "target": "traces", "facet": { "field": "myapp.task_id",    "value": "{id}" } },
        { "label": "GenAI spans for this session", "target": "traces", "facet": { "field": "myapp.session_id", "value": "{id}" } }
      ]
    },
    {
      "name": "Trace response header",
      "match": {
        "urlPattern": "https://*/*",
        "source": "header",
        "header": "traceresponse",
        // W3C trace-context: 00-<32-hex trace-id>-<16-hex span-id>-<flags>
        "regex": "^\\d\\d-([0-9a-f]{32})-"        // capture group 1 = the id
      },
      "actions": [
        { "label": "Trace waterfall", "target": "trace", "idIsTrace": true }
      ]
    }
  ]
}
```

- `{id}` in a facet value is substituted with the (possibly regex-captured) id.
- `target` is a catalog view name, or the pseudo-targets `trace` / `problem`.
- One id → **multiple actions** (a session id yields both "pod" and "span").

### Two extraction surfaces, two triggers

**DOM ids** (`source: "dom"`) — a content script matches `selector`/`regex`,
and next to each occurrence injects a small **dtui-icon button**. One action
→ one button; multiple actions → a button that pops a tiny menu. Click →
message to the background → open side panel → `dtuiNavigate`.

**Header ids** (`source: "header"`) — *not in the DOM*, so there is no node to
decorate. The extension must observe network responses. Options, roughly in
order of preference:

1. **MAIN-world fetch/XHR shim** injected by a content script: wrap
   `window.fetch` / `XMLHttpRequest` to read `response.headers.get(header)`.
   Simple, no extra permission, but misses traffic the shim can't see
   (documents, other frames, resources issued before injection).
2. `chrome.webRequest.onHeadersReceived` (observe-only; needs host perms). Sees
   all requests, but MV3 restricts response-header *reading* and the API is
   observe-only without the blocking variant — verify current capability.
3. `chrome.debugger` — sees everything, but attaches the "being debugged"
   banner; too heavy for routine use.

Because header ids have no anchor, their trigger is a **floating badge / a
"recent trace ids" strip** (in the page or in the side panel) rather than an
inline button. This UX divergence is inherent to the source and should be
called out to the user, not hidden.

## Open questions

- **Field names for business ids.** "Pod that ran this session" requires knowing
  the Grail field that carries the session id on the pod/log/span records. That
  is tenant/instrumentation-specific and belongs in the rule config; validate a
  real example against the tenant (see `docs/dev/learnings.md` — Grail
  fails silently on a wrong field name).
- **View names.** Confirm the exact catalog view keys (`pods`, `genai`/`spans`)
  and that a bare `facet` (no entity scope) does not trip Grail's scan limit —
  broad unscoped signal queries are exactly the documented failure mode. A
  trace-id or session-id facet is narrow, so this is likely fine, but confirm.
- **Config authoring UX.** Raw JSON on the Options page to start; a rule-builder
  form later.
- **Header capture completeness.** Which of the three capture strategies is
  acceptable given the traffic that actually carries `traceresponse`.

## Status

Implemented and committed:

1. **`tui.NavigateMsg` + `app.Update` case** (`internal/tui/view.go`,
   `app.go`), routing to the existing `pushViewMsg` (facets) / `waterfallMsg`
   (trace). Model test in `app_test.go`; native build + lint + `-race` suite
   green.
2. **`window.dtuiNavigate(json)`** bridge in `cmd/web/main.go`; headless
   `wasm/smoke.mjs` drives it and asserts the view switches.
3. **In-panel deep-link tester** (Alt+G) to exercise it live with no page.
4. **Configurable, generic content script** (`extension/content.js`): rules
   live in `chrome.storage.local` ("deepLinkRules"), edited on the Options page
   (validation, insert-example, import from URL/file). Supports **url** sources
   (id from `urlRegex`, optional `decode`, floating button) and **dom** sources
   (id from `selector`/`attribute`/`textRegex`, inline button). Each action can
   set a `lens` and a `timeframe`. The background registers the content script
   **dynamically** (`chrome.scripting`) for exactly the hosts the rules match.
   Handoff: content script → background (`chrome.storage.session` +
   `sidePanel.open`) → sidepanel applies once wasm is ready. Buttons are
   Shadow-DOM isolated; the match-pattern→regex conversion is unit-checked.
   **The extension ships with no customer rules** — an app provides its own
   bundle (see `extension/examples/` for a worked example, ideally hosted in the
   app's repo) imported by URL/file. `NavigateMsg` carries `Lens`/`Timeframe`; a
   GenAI bundle targets the `traces` **genai** lens over e.g. a 30d window.
   Fields + base62 codec validated against a live Gen3 tenant (learnings.md §1.23).

5. **Short-id decode.** Some apps' URLs carry base62-encoded UUIDs
   (`…/chat?id=conv_<22ch>`, `…/investigations/task_<22ch>`). A rule
   `decode: "base62-uuid"` turns the captured short id back into the UUID stored
   on spans, so the chat/investigation URL maps straight to a `traces` facet on
   the app's business-id field — **no DOM scraping** (when the canonical UUIDs
   aren't in the static DOM). Codec + fields validated round-trip against a live
   Gen3 tenant (learnings §1.23). Note: on tenants where span retention is
   ~10 days, only recent conversations resolve.

6. **`traceresponse` header source** (`source: "header"`). Implemented with
   `webRequest.onHeadersReceived` in the background — **not** a fetch/XHR shim:
   it reads the response header at the network layer, so it works even when page
   CORS would hide the header from scripts, and catches all traffic. The
   background extracts the id (`regex`, e.g. `^\d\d-([0-9a-f]{32})-` for the
   trace-id) and posts it to the tab's content script, which builds the payload
   (reusing `decodeId`/`payloadFor`) and adds a button to the floating stack
   (header ids have no DOM anchor). A bundle can map `traceresponse` →
   the `trace` waterfall.

7. **Options page: environments + rule sets.** The Options page manages a list
   of **environments** (`{name,url,token}` + an active one) and a list of named
   **rule sets**, each with an enable/disable toggle. These are management layers
   that compile down to the flat storage contracts the runtime already reads, so
   `sidepanel.js` / `background.js` / `content.js` are untouched:
   `environments[] + activeEnvId → tenantUrl/token` (the active env's mirror,
   which `background` already re-syncs on), and `ruleSets[] (enabled) →
   deepLinkRules` (only enabled sets' rules take effect). An imported bundle
   becomes one enabled rule set (upsert by name); the old single-tenant /
   flat-rules storage migrates on first load.

8. **Multi-environment + context-aware deep links.** dtui already has a
   session-local context switcher (`:ctx`, `Options.SwitchContext`/`Contexts`,
   the context picker) — the native binary wires it to dtctl contexts. The wasm
   host now wires the *same* seam to the Options page's environment list:
   `cmd/web` reads `dtuiConfig.environments` (`[{name,url}]`) + `context`,
   builds `Options.Contexts` and a `SwitchContext` that constructs a fresh
   JS-transported client for the named environment. So `:ctx` switches tenants
   in-session, no panel reload. **Tokens never reach Go**: each context's client
   roots its URLs at that environment's host, and `sidepanel.js`'s
   `dtuiFetch` picks the token whose configured host matches the request URL.

   A deep link targets an environment by its **tenant URL**, not a local
   nickname — a rule bundle committed in an app repo must not depend on what a
   given user named that environment. The rule action carries
   `environment: "https://<tenant>"`; `cmd/web` resolves that URL to the local
   context by **host match** against the configured environments (falling back
   to an exact name for the dev tester) and sends the resolved name as
   `NavigateMsg.Context`. `navigateExternal` switches first (stashing the nav in
   `pendingNav`) and applies it when the switch lands; if no configured
   environment matches the host, it reports "no configured environment matches …
   — add it on the Options page" rather than silently querying the wrong tenant.
   The Origin-strip `declarativeNetRequest` rule covers every configured host.
   Model tests (`TestNavigateMsg` context subtests) + the headless `smoke.mjs`
   (two environments; asserts both a keyboard `:ctx` switch and a deep link
   carrying a tenant URL route their queries to the intended host).

9. **Multiple selectable actions per page — the grouped launcher.** Several
   ambient signals can be live on one page at once (a RUM session, a trace, a
   GenAI id). The floating stack already aggregated `url` + `header` hits; it now
   collapses into **one dtui launcher** with a count badge whose click opens a
   popover **menu sectioned by an optional `action.group`** (e.g. "Trace", "RUM",
   "GenAI"). A single available jump still renders as a lone pill (no regression);
   an outside click closes the menu. Each menu item is one `dtuiNavigate` — the
   Go side is untouched (the menu is pure `content.js`). One additive schema field
   (`group`, a string) validated on the Options page.

10. **RUM via the beacon (`source:"request"`).** RUM session ids ride in a
    **request URL**, not the DOM/response-headers/cookies: the Dynatrace RUM
    agent's beacon posts to `<app>/rb_...?...&vi=<SESSION_ID>&...`, and that `vi`
    **equals `dt.rum.session.id`** in Grail (validated against a live
    astroshop demo session — see `learnings.md` §1.15b). A new
    `source:"request"` reads it at `webRequest.onBeforeRequest` (URL only — no
    body, no snappy decompression, unlike the Gen3 `bhv.*` beacons internal
    devtooling parses), extracts it with the rule's `urlRegex`, and
    feeds the launcher over the same ambient-hit channel as headers. The RUM deep
    link needs **zero Go changes**: `target:"userevents"` (or `sessions`) with a
    `dt.rum.session.id` facet rides the existing facet path (the facet injects
    before the query's `sort`/`limit`). Example bundle:
    `extension/examples/astroshop.rules.json` (two RUM jumps, grouped, targeting
    a tenant by URL). It ships **no trace action on purpose**: astroshop's browser
    trace id (`server-timing dtTrId` / W3C `traceparent`) does not resolve to
    backend spans on that deployment, so a trace deep link would silently open an
    empty waterfall (§1.15b).

Remaining:

- **Gen3 RUM beacon extraction.** The `source:"request"` URL-param path covers
  Gen2 (`vi=`). Gen3 (`ty=js`, `bhv.*`) carries ids in a **snappy-compressed
  POST body**, which `onBeforeRequest` can't read cleanly — that needs the
  devtools-network or `debugger` capture the internal devtool uses. Deferred
  until a Gen3 RUM deep link is actually wanted.
- **Confirm `sidePanel.open` from a content-script click** survives the
  messaging hop in practice (fallback: the request is stashed, so opening the
  panel manually still applies it).
- **Per-rule form editor** (rules *within* a set are still edited as raw JSON;
  environments and rule sets themselves now have a card/toggle UI).
- **In-panel environment switcher UI** — switching works via `:ctx` inside the
  TUI; a chrome affordance (dropdown) in the panel header could mirror it.
  Changing the *active* env on the Options page still needs a panel reopen to
  take effect as the initial context.
