/* dtui Chrome extension — side-panel bridge.
 *
 * Wires an xterm.js terminal to the dtui wasm program:
 *   - keystrokes  -> window.dtuiSendInput  (registered by Go)
 *   - ANSI output -> window.dtuiTermWrite   (called by Go)
 *   - resize      -> window.dtuiResize      (registered by Go)
 *   - every dtctl HTTP call -> window.dtuiFetch (defined below)
 *
 * The one thing that differs from the Dynatrace-App version (which leans on the
 * AppEngine runtime for auth): HERE the extension supplies auth itself. The
 * tenant URL + token come from chrome.storage.local (set on the Options page),
 * and dtuiFetch adds the Authorization header. Extension pages carry the
 * extension's host_permissions, so this cross-origin fetch is not blocked by
 * page CORS.
 */

const { Terminal } = window;
const FitAddon = window.FitAddon?.FitAddon ?? window.FitAddon;

// xterm.js drives its own ResizeObserver; when a fit resizes the terminal inside
// the same frame the browser emits a benign, stack-less "ResizeObserver loop
// completed with undelivered notifications" error (nothing is actually dropped —
// it just deferred a callback). Swallow only that exact message so it stops
// cluttering the chrome://extensions error log; everything else propagates.
window.addEventListener("error", (e) => {
  if (e && e.message && e.message.includes("ResizeObserver loop")) {
    e.stopImmediatePropagation();
    e.preventDefault();
  }
});

// keyToBytes maps a DOM keydown to the terminal byte sequence dtui expects,
// or null to let the browser handle the key. Covers everything the TUI binds:
// printable keys, Enter/Tab/Esc/Backspace, arrows, page/home/end, and ctrl+<x>
// (e.g. ctrl+q reveal query, ctrl+p/ctrl+n history).
function keyToBytes(e) {
  if (e.metaKey || e.altKey) return null;
  const k = e.key;
  if (e.ctrlKey) {
    // Let Ctrl+V through to the browser's paste (handled by the 'paste' listener)
    // rather than sending it as a 0x16 control byte — so paste works on
    // Linux/Windows too, matching ⌘V on macOS.
    if (k.toLowerCase() === "v") return null;
    if (k.length === 1) {
      const c = k.toLowerCase().charCodeAt(0);
      if (c >= 97 && c <= 122) return String.fromCharCode(c - 96); // ctrl+a..z
    }
    return null;
  }
  switch (k) {
    case "Enter": return "\r";
    case "Backspace": return "\x7f";
    case "Tab": return "\t";
    case "Escape": return "\x1b";
    case "ArrowUp": return "\x1b[A";
    case "ArrowDown": return "\x1b[B";
    case "ArrowRight": return "\x1b[C";
    case "ArrowLeft": return "\x1b[D";
    case "Home": return "\x1b[H";
    case "End": return "\x1b[F";
    case "PageUp": return "\x1b[5~";
    case "PageDown": return "\x1b[6~";
    case "Delete": return "\x1b[3~";
    default:
      return k.length === 1 ? k : null; // printable (incl. space, ':', '/')
  }
}

const hostOf = (u) => { try { return new URL(u).host; } catch { return ""; } };

// getConfig resolves the configured environments and the active one. Falls back
// to the legacy single tenantUrl/token if no environments list is present.
async function getConfig() {
  const s = await chrome.storage.local.get(["environments", "activeEnvId", "tenantUrl", "token"]);
  let list = Array.isArray(s.environments) && s.environments.length
    ? s.environments
    : (s.tenantUrl ? [{ id: "legacy", name: "Default", url: s.tenantUrl, token: s.token }] : []);
  const envs = list
    .filter((e) => e && e.url)
    .map((e) => ({ id: e.id, name: e.name || hostOf(e.url), url: (e.url || "").replace(/\/+$/, ""), token: e.token || "" }));
  const active = envs.find((e) => e.id === s.activeEnvId) || envs[0] || null;
  return { envs, active };
}

// tokenForUrl picks the token of the environment whose host matches the request
// URL — so a mid-session :ctx switch (which points the client at another host)
// authenticates against the right environment. Falls back to the active env.
function tokenForUrl(envs, active, url) {
  const host = hostOf(url);
  const hit = envs.find((e) => hostOf(e.url) === host);
  return (hit && hit.token) || (active && active.token) || "";
}

function makeFetch(envs, active) {
  // Go sends absolute URLs already rooted at the target environment, and strips
  // its placeholder Authorization (see cmd/web/http_wasm.go) so we own auth.
  return async function dtuiFetch(req) {
    try {
      const token = tokenForUrl(envs, active, req.url);
      const headers = { ...(req.headers || {}) };
      if (token) headers["Authorization"] = "Bearer " + token;
      const res = await fetch(req.url, {
        method: req.method || "GET",
        headers,
        body: req.body ? req.body : undefined,
        // Never let the host's SSO session cookies ride along. If you're logged
        // into the tenant in another tab, Chrome would
        // otherwise attach its cookies; the gateway then prefers the cookie
        // session over our Bearer token and enforces CSRF, rejecting the call
        // with "403 Access denied". The token is the only auth we want.
        credentials: "omit",
      });
      const body = await res.text();
      const outHeaders = {};
      res.headers.forEach((v, k) => (outHeaders[k] = v));
      return { status: res.status, headers: outHeaders, body };
    } catch (e) {
      // Surface transport failure as a synthetic 5xx so the TUI shows an error
      // instead of hanging on a query poll.
      return { status: 599, headers: {}, body: String(e && e.message ? e.message : e) };
    }
  };
}

// surfaceKind reports how this page was opened. The side panel loads
// sidepanel.html with no query (manifest default_path); the pop-out controls
// open the SAME page as a tab/window with ?surface=tab|window. Everything else
// (config, auth, wasm) is identical across surfaces — the page is host-agnostic.
const surfaceKind = new URLSearchParams(location.search).get("surface"); // null => side panel

// A non-panel surface holds a keep-alive port so the background can prefer an
// already-open tab/window over popping the side panel on a deep-link. The port
// also keeps the MV3 worker alive while this surface is open, so its count of
// open surfaces stays accurate (see background.js onConnect).
if (surfaceKind) {
  try { chrome.runtime.connect({ name: "dtui-surface" }); } catch { /* worker asleep; reconnects on demand */ }
}

// wireSurfaceControls shows the pop-out buttons only in the side panel (you
// can't meaningfully pop a tab/window out of itself) and opens this same page
// in a bigger surface. Neither chrome.tabs.create nor chrome.windows.create
// needs an extra permission.
function wireSurfaceControls() {
  const controls = document.getElementById("surface-controls");
  if (surfaceKind) { controls.hidden = true; return; }
  controls.hidden = false;
  const open = (surface, extra) =>
    chrome.tabs && surface === "tab"
      ? chrome.tabs.create({ url: chrome.runtime.getURL("sidepanel.html?surface=tab") })
      : chrome.windows.create({ url: chrome.runtime.getURL("sidepanel.html?surface=window"), type: "popup", ...extra });
  document.getElementById("btn-tab").addEventListener("click", () => open("tab"));
  document.getElementById("btn-window").addEventListener("click", () => open("window", { width: 1100, height: 720 }));
}

async function main() {
  wireSurfaceControls();
  const { envs, active } = await getConfig();
  if (!active || !active.url || !active.token) {
    document.getElementById("hint").hidden = false;
    return;
  }

  const term = new Terminal({
    fontFamily: "'JetBrains Mono', Menlo, 'Cascadia Mono', Consolas, monospace",
    fontSize: 13,
    cursorBlink: true,
    scrollback: 0, // dtui uses the alternate screen and repaints fully
    allowProposedApi: true,
    theme: { background: "#12141a", foreground: "#e6e6e6" },
  });
  const fit = new FitAddon();
  term.loadAddon(fit);
  const host = document.getElementById("term");
  term.open(host);

  // FitAddon can propose one row/column too many (sub-pixel cell-size rounding
  // that accumulates across the DOM cells); in a fixed-size side panel the extra
  // row then clips at the bottom (dtui's status/footer at half height) and the
  // extra column clips at the right (the last column's final glyph cut — e.g.
  // "DURATIO[N]", "0µ[s]"). clampTerm shrinks the terminal on whichever axis
  // overflows until the rendered .xterm-screen fits the container's content box,
  // trading a possible sliver of blank space for a never-clipped edge.
  function clampTerm() {
    const screen = host.querySelector(".xterm-screen") || term.element;
    if (!screen) return;
    const cs = getComputedStyle(host);
    const availW = host.clientWidth - parseFloat(cs.paddingLeft || 0) - parseFloat(cs.paddingRight || 0);
    const availH = host.clientHeight - parseFloat(cs.paddingTop || 0) - parseFloat(cs.paddingBottom || 0);
    if (!(availW > 0) || !(availH > 0)) return; // not laid out yet; a later pushResize clamps
    let guard = 6;
    while (term.cols > 2 && screen.getBoundingClientRect().width > availW + 0.5 && guard-- > 0) {
      term.resize(term.cols - 1, term.rows);
    }
    guard = 6;
    while (term.rows > 2 && screen.getBoundingClientRect().height > availH + 0.5 && guard-- > 0) {
      term.resize(term.cols, term.rows - 1);
    }
  }

  fit.fit();
  clampTerm();
  term.focus();

  // Deep-link tester overlay (Alt+G). A dev affordance to fire dtuiNavigate
  // by hand; the real trigger is per-site injected buttons (WEB_DEEPLINK.md).
  const navbox = document.getElementById("navbox");
  const toggleNavbox = () => {
    navbox.hidden = !navbox.hidden;
    if (!navbox.hidden) document.getElementById("nav-value").focus();
  };
  const submitNav = () => {
    const target = document.getElementById("nav-target").value.trim();
    const field = document.getElementById("nav-field").value.trim();
    const value = document.getElementById("nav-value").value.trim();
    const lens = document.getElementById("nav-lens").value.trim();
    const tf = document.getElementById("nav-tf").value.trim();
    const env = document.getElementById("nav-env").value.trim();
    if (!target || !value) return;
    const payload = target === "trace"
      ? { target, id: value }
      : { target, facets: [{ field, value }] };
    if (lens) payload.lens = lens;
    if (tf) payload.timeframe = tf;
    if (env) payload.environment = env;
    if (window.dtuiNavigate) window.dtuiNavigate(JSON.stringify(payload));
    navbox.hidden = true;
  };
  document.getElementById("nav-go").addEventListener("click", submitNav);
  document.getElementById("nav-cancel").addEventListener("click", () => { navbox.hidden = true; });

  // Input: xterm's own onData only fires when its hidden textarea has focus,
  // which is unreliable in a side panel. Instead we translate key events at the
  // window level (capture phase, so nothing else swallows them first) and send
  // the terminal bytes straight to Go — focus-independent and deterministic.
  window.addEventListener(
    "keydown",
    (e) => {
      // Keys aimed at our own chrome (the pop-out buttons) belong to the button,
      // not the terminal — otherwise Enter/Space would be swallowed as input.
      if (e.target && e.target.closest && e.target.closest("#surface-controls")) return;
      // Alt+G toggles the deep-link tester; when it's open, keys belong to its
      // inputs, not the terminal.
      if (e.altKey && (e.key === "g" || e.key === "G")) {
        e.preventDefault();
        toggleNavbox();
        return;
      }
      if (!navbox.hidden) {
        if (e.key === "Escape") { e.preventDefault(); navbox.hidden = true; }
        else if (e.key === "Enter") { e.preventDefault(); submitNav(); }
        return; // let the overlay's inputs handle everything else
      }
      const bytes = keyToBytes(e);
      if (bytes == null) return; // let the browser handle it (meta/alt/etc.)
      e.preventDefault();
      e.stopPropagation();
      if (window.dtuiSendInput) window.dtuiSendInput(bytes);
    },
    true,
  );

  // Paste: because input is routed at the keydown level (above), ⌘/Ctrl+V is
  // filtered out by keyToBytes and never reaches Go — the browser fires a
  // separate 'paste' event instead, which xterm's own paste path can't hand us
  // (we don't use term.onData). Forward the clipboard text straight to Go so
  // pasting works in facet search, the command bar, and every other input.
  // Reading clipboardData from a user-initiated paste needs no extra permission.
  window.addEventListener(
    "paste",
    (e) => {
      if (!navbox.hidden) return; // the deep-link tester's inputs own their paste
      const cd = e.clipboardData || window.clipboardData;
      let text = cd && cd.getData("text");
      if (!text) return;
      e.preventDefault();
      e.stopPropagation();
      // Drop a trailing newline (copying a whole line is common) so a pasted id
      // doesn't immediately submit the single-line input it lands in.
      text = text.replace(/\r?\n+$/, "");
      if (text && window.dtuiSendInput) window.dtuiSendInput(text);
    },
    true,
  );

  // Reflect keyboard focus in a subtle ring (see #focusring in the HTML) so it
  // is obvious when input is being routed to the terminal.
  const setFocused = (on) => document.body.classList.toggle("focused", on);
  window.addEventListener("focus", () => setFocused(true));
  window.addEventListener("blur", () => setFocused(false));
  setFocused(document.hasFocus());

  // Deep-link navigations can arrive before the wasm program has registered
  // window.dtuiNavigate (panel just opened, still booting). Queue them and
  // flush once Go signals ready.
  let navQueue = [];
  const applyNav = (payload) => {
    if (!payload) return;
    if (window.dtuiNavigate) window.dtuiNavigate(JSON.stringify(payload));
    else navQueue.push(payload);
  };
  const flushNav = () => {
    const q = navQueue;
    navQueue = [];
    for (const p of q) applyNav(p);
  };
  // A request stashed by the background before we opened.
  chrome.storage.session.get("pendingNav").then(({ pendingNav }) => {
    if (pendingNav) { applyNav(pendingNav); chrome.storage.session.remove("pendingNav"); }
  });
  // Live requests while the panel is already open.
  chrome.runtime.onMessage.addListener((msg) => {
    if (msg && msg.type === "dtui-nav") applyNav(msg.payload);
  });

  const pushResize = () => {
    try {
      fit.fit();
      clampTerm();
      window.dtuiResize && window.dtuiResize(term.cols, term.rows);
    } catch { /* element detached mid-teardown */ }
  };

  // The bridge globals the Go program reads before it registers its callbacks.
  // dtui gets the full environment list (names + urls, no tokens) so :ctx can
  // switch between them; tokens stay here and are applied per request host.
  window.dtuiConfig = {
    environment: active.url,
    context: active.name,
    environments: envs.map((e) => ({ name: e.name, url: e.url })),
  };
  window.dtuiFetch = makeFetch(envs, active);
  // 'o' (open in the Dynatrace UI) — the wasm program hands us a Dynatrace
  // intent/app URL; open it in a new browser tab (a side panel can't shell out
  // like the native binary does). chrome.tabs.create needs no "tabs" permission
  // to create a tab; fall back to window.open if it's somehow unavailable.
  window.dtuiOpenURL = (url) => {
    try {
      if (chrome.tabs && chrome.tabs.create) chrome.tabs.create({ url });
      else window.open(url, "_blank", "noopener");
    } catch { window.open(url, "_blank", "noopener"); }
  };
  window.dtuiTermWrite = (data) => term.write(data);
  window.dtuiReady = () => setTimeout(() => { pushResize(); term.focus(); flushNav(); }, 0); // see Terminal.tsx note
  window.dtuiExit = () =>
    term.write("\r\n\x1b[90m[dtui session ended — reload to restart]\x1b[0m\r\n");

  // Defer the resize to the next frame: fit/clampTerm resize the terminal
  // synchronously, which the observer then re-reports as a change — Chrome logs
  // that self-trigger as "ResizeObserver loop completed with undelivered
  // notifications". A rAF hop breaks the loop (and coalesces bursts).
  let resizeRaf = 0;
  const ro = new ResizeObserver(() => {
    if (resizeRaf) return;
    resizeRaf = requestAnimationFrame(() => { resizeRaf = 0; pushResize(); });
  });
  ro.observe(document.getElementById("term"));

  const go = new window.Go();
  const wasmUrl = chrome.runtime.getURL("dtui.wasm");
  let instance;
  try {
    ({ instance } = await WebAssembly.instantiateStreaming(fetch(wasmUrl), go.importObject));
  } catch {
    const bytes = await (await fetch(wasmUrl)).arrayBuffer();
    ({ instance } = await WebAssembly.instantiate(bytes, go.importObject));
  }
  // go.run resolves only when the program exits; it blocks on select{}.
  go.run(instance);
}

main().catch((e) => console.error("dtui boot failed:", e));
