// Factory-default deep-link rules (defines self.DEFAULT_RULES).
importScripts("default-rules.js");

// Open the dtui side panel when the toolbar icon is clicked. The TUI runs
// in the side-panel PAGE (not this service worker): MV3 workers are ephemeral
// and would be killed mid-session, whereas a side panel lives as long as it is
// open.
chrome.sidePanel
  .setPanelBehavior({ openPanelOnActionClick: true })
  .catch((e) => console.error("setPanelBehavior:", e));

// Pop-out surfaces (a full tab or a detached window, both loading sidepanel.html
// with ?surface=…) each hold a "dtui-surface" port for as long as they're
// open. We count them so a deep-link prefers an already-open surface — which
// receives the live dtui-nav broadcast below — over popping the side panel on
// top of it. The port also keeps this MV3 worker alive while a surface is open,
// so the count stays accurate; if every surface closes and the worker is
// recycled, it correctly resets to 0. The side panel itself is deliberately not
// counted, so multi-window panel-open behavior is unchanged.
let openSurfaces = 0;
chrome.runtime.onConnect.addListener((port) => {
  if (port.name !== "dtui-surface") return;
  openSurfaces++;
  port.onDisconnect.addListener(() => { openSurfaces = Math.max(0, openSurfaces - 1); });
});

// The Dynatrace platform gateway rejects requests carrying a cross-origin
// Origin header ("403 Invalid CORS request"). Extension-page fetches to the
// tenant always carry `Origin: chrome-extension://<id>`, and fetch() can't
// remove it — so we strip it with a declarativeNetRequest rule scoped to the
// configured tenant host. Without the Origin header the server treats it as a
// non-CORS, token-authenticated API call and allows it.
const RULE_ID = 1;

async function syncOriginRule() {
  const { environments, tenantUrl } = await chrome.storage.local.get(["environments", "tenantUrl"]);
  const urls = Array.isArray(environments) && environments.length
    ? environments.map((e) => e && e.url)
    : (tenantUrl ? [tenantUrl] : []);
  const hosts = [...new Set(
    urls.map((u) => { try { return new URL(u).hostname; } catch { return null; } }).filter(Boolean),
  )];

  const addRules = hosts.length
    ? [
        {
          id: RULE_ID,
          priority: 1,
          action: {
            type: "modifyHeaders",
            requestHeaders: [{ header: "Origin", operation: "remove" }],
          },
          // Scoped to the configured tenant hosts only (not all browsing), and
          // excludedInitiatorDomains keeps it to *our* cross-origin calls: the
          // wasm host fetches from a chrome-extension:// page, so its initiator is
          // never the tenant host. A tenant tab making same-origin calls to its
          // own UI is excluded, so we don't strip Origin from that page's traffic
          // and weaken its CSRF/CORS posture.
          condition: {
            requestDomains: hosts,
            excludedInitiatorDomains: hosts,
            resourceTypes: ["xmlhttprequest"],
          },
        },
      ]
    : [];

  await chrome.declarativeNetRequest.updateDynamicRules({
    removeRuleIds: [RULE_ID],
    addRules,
  });
  console.log("dtui: Origin-strip rule for", hosts.length ? hosts.join(", ") : "(none)");
}

// Deep-link rules are user config (chrome.storage.local "deepLinkRules"). Seed
// the factory defaults on first install, then register this extension's content
// script for exactly the hosts those rules match — so pointing dtui at a new
// site is a config edit, not a manifest change.
const DEEPLINK_SCRIPT_ID = "dtui-deeplink";

async function seedRules() {
  const { deepLinkRules } = await chrome.storage.local.get("deepLinkRules");
  if (!Array.isArray(deepLinkRules)) {
    await chrome.storage.local.set({ deepLinkRules: self.DEFAULT_RULES });
  }
}

// registerDeepLinkScript is serialized through this promise chain. initExtension
// runs from both onStartup and the top-level call (and again on every rule edit),
// so two registrations can otherwise interleave between
// getRegisteredContentScripts and registerContentScripts and trip Chrome's
// "Duplicate script ID" error. Chaining guarantees one runs at a time; using the
// same handler for fulfilled AND rejected keeps the chain alive after a failure.
let deeplinkRegChain = Promise.resolve();
function registerDeepLinkScript() {
  deeplinkRegChain = deeplinkRegChain.then(doRegisterDeepLinkScript, doRegisterDeepLinkScript);
  return deeplinkRegChain;
}

async function doRegisterDeepLinkScript() {
  const { deepLinkRules } = await chrome.storage.local.get("deepLinkRules");
  const rules = Array.isArray(deepLinkRules) ? deepLinkRules : [];
  const matches = [...new Set(rules.flatMap((r) => r.matches || []))];

  // Clear any prior registration first (idempotent across reloads/updates).
  try {
    const existing = await chrome.scripting.getRegisteredContentScripts({ ids: [DEEPLINK_SCRIPT_ID] });
    if (existing.length) await chrome.scripting.unregisterContentScripts({ ids: [DEEPLINK_SCRIPT_ID] });
  } catch { /* nothing registered yet */ }

  if (matches.length === 0) return;
  try {
    await chrome.scripting.registerContentScripts([
      { id: DEEPLINK_SCRIPT_ID, js: ["content.js"], matches, runAt: "document_idle", allFrames: false },
    ]);
    console.log("dtui: deep-link script registered for", matches);
  } catch (e) {
    console.error("dtui: registerContentScripts failed", e);
  }
}

// --- header source (e.g. traceresponse) -----------------------------------
//
// Response-header ids (like the W3C `traceresponse: 00-<trace-id>-…`) never
// touch the page DOM, so we read them at the network layer with webRequest —
// which also sees them regardless of page CORS. The background extracts the id
// and tells the page's content script to show a floating button; content.js
// owns the decode/payload/UI (so all of that lives in one place).

// matchPatternToRegExp mirrors the one in content.js (kept compact here) so the
// header listener can decide which rule applies to a response URL.
function matchPatternToRegExp(pattern) {
  const m = /^(\*|https?|file):\/\/(\*|(?:\*\.)?[^/*]+|)(\/.*)$/.exec(pattern);
  if (!m) return /$.^/;
  const [, scheme, host, path] = m;
  const esc = (s) => s.replace(/[.+?^${}()|[\]\\*]/g, "\\$&");
  const schemeRe = scheme === "*" ? "https?" : scheme;
  let hostRe;
  if (host === "*") hostRe = "[^/]+";
  else if (host.startsWith("*.")) hostRe = "(?:[^/]+\\.)?" + esc(host.slice(2));
  else hostRe = esc(host);
  return new RegExp("^" + schemeRe + "://" + hostRe + esc(path).replace(/\\\*/g, ".*") + "$");
}
const urlMatchesAny = (url, patterns) => (patterns || []).some((p) => matchPatternToRegExp(p).test(url));

// In-memory snapshot of the header/request rules. onHeadersReceived and
// onBeforeRequest are hot — hitting chrome.storage.local on every event adds
// latency and keeps the MV3 worker awake — so the listeners read this cache and
// we refresh it whenever the rules change (initExtension / storage.onChanged).
let headerRules = [];
let requestRules = [];
async function refreshRuleCache() {
  const { deepLinkRules } = await chrome.storage.local.get("deepLinkRules");
  const rules = Array.isArray(deepLinkRules) ? deepLinkRules : [];
  headerRules = rules.filter((r) => r.source === "header");
  requestRules = rules.filter((r) => r.source === "request");
}

// onHeaders is a stable reference so it can be removed/re-added when rules change.
function onHeaders(details) {
  if (details.tabId < 0) return;
  const rules = headerRules;
  const hits = [];
  for (const rule of rules) {
    if (!rule.header || !urlMatchesAny(details.url, rule.matches)) continue;
    const h = (details.responseHeaders || []).find((x) => x.name.toLowerCase() === rule.header.toLowerCase());
    if (!h || !h.value) continue;
    let id = h.value;
    if (rule.regex) {
      let m;
      try { m = h.value.match(new RegExp(rule.regex, "i")); } catch { continue; }
      if (!m) continue;
      id = m[1] != null ? m[1] : m[0];
    }
    for (const action of rule.actions || []) hits.push({ action, id, decode: rule.decode });
  }
  if (hits.length) chrome.tabs.sendMessage(details.tabId, { type: "dtui-ambient-hit", hits }).catch(() => {});
}

function setupHeaderListener() {
  try { chrome.webRequest.onHeadersReceived.removeListener(onHeaders); } catch { /* not added yet */ }
  const urls = [...new Set(headerRules.flatMap((r) => r.matches || []))];
  if (urls.length === 0) return;
  chrome.webRequest.onHeadersReceived.addListener(onHeaders, { urls }, ["responseHeaders"]);
  console.log("dtui: header listener watching", urls);
}

// --- request source (e.g. the RUM beacon's vi= session id) -----------------
//
// Some ids ride in a REQUEST URL rather than the DOM or a response header — the
// clearest case is Dynatrace RUM: the agent's beacon POSTs to
// <app>/rb_...?...&vi=<SESSION_ID>&..., and that `vi` equals dt.rum.session.id
// in Grail (validated against a live astroshop demo session). We read it at onBeforeRequest
// (URL only — no body, no snappy), extract it with the rule's urlRegex, and
// hand it to the content script over the same ambient-hit channel as headers.

// onRequest is a stable reference so it can be removed/re-added when rules change.
function onRequest(details) {
  if (details.tabId < 0) return;
  const rules = requestRules;
  const hits = [];
  for (const rule of rules) {
    if (!rule.urlRegex || !urlMatchesAny(details.url, rule.matches)) continue;
    let m;
    try { m = details.url.match(new RegExp(rule.urlRegex, "i")); } catch { continue; }
    if (!m || m[1] == null) continue;
    const id = m[1];
    for (const action of rule.actions || []) hits.push({ action, id, decode: rule.decode });
  }
  if (hits.length) chrome.tabs.sendMessage(details.tabId, { type: "dtui-ambient-hit", hits }).catch(() => {});
}

function setupRequestListener() {
  try { chrome.webRequest.onBeforeRequest.removeListener(onRequest); } catch { /* not added yet */ }
  const urls = [...new Set(requestRules.flatMap((r) => r.matches || []))];
  if (urls.length === 0) return;
  // Observe-only (no extraInfoSpec): we read details.url, never a body.
  chrome.webRequest.onBeforeRequest.addListener(onRequest, { urls });
  console.log("dtui: request listener watching", urls);
}

async function initExtension() {
  await syncOriginRule();
  await seedRules();
  await registerDeepLinkScript();
  await refreshRuleCache();
  setupHeaderListener();
  setupRequestListener();
}

chrome.runtime.onInstalled.addListener(initExtension);
chrome.runtime.onStartup.addListener(initExtension);
chrome.storage.onChanged.addListener((changes, area) => {
  if (area !== "local") return;
  if (changes.tenantUrl || changes.environments) syncOriginRule();
  if (changes.deepLinkRules) {
    registerDeepLinkScript();
    refreshRuleCache().then(() => { setupHeaderListener(); setupRequestListener(); });
  }
});
// Also run once when the worker first spins up.
initExtension();

// Deep-link handoff: a content-script button posts {type:'dtui-nav',
// payload}. We stash the request (so the side panel can apply it once its wasm
// is ready, even if it isn't open yet) and open the panel in the sender's
// window. The click is a user gesture, which sidePanel.open() requires.
chrome.runtime.onMessage.addListener((msg, sender) => {
  if (!msg || msg.type !== "dtui-nav") return;
  // Deliver to any surface that's already open (panel, popped-out tab/window).
  chrome.runtime.sendMessage({ type: "dtui-nav", payload: msg.payload }).catch(() => {});
  // A detached surface is open and just received the broadcast — don't also pop
  // the side panel on top of the user's working window.
  if (openSurfaces > 0) return;
  // Nothing detached: stash so a freshly opened panel applies it once its wasm
  // is ready, then open the side panel in the sender's window (the click is the
  // user gesture sidePanel.open() requires). Opening an already-open panel is a
  // no-op; it still gets the live message above.
  chrome.storage.session.set({ pendingNav: msg.payload });
  const windowId = sender.tab && sender.tab.windowId;
  if (windowId != null && chrome.sidePanel.open) {
    chrome.sidePanel.open({ windowId }).catch((e) => console.error("sidePanel.open:", e));
  }
});
