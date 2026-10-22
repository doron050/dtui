/* dtui deep-link content script.
 *
 * Recognizes ids on a host page — from the URL or from DOM elements — and
 * injects a small dtui button that jumps the side-panel TUI to a view with
 * the id pre-applied as a server-side filter. See docs/design/WEB_DEEPLINK.md.
 *
 * Rules are NOT hard-coded here: they live in chrome.storage.local
 * ("deepLinkRules"), seeded from default-rules.js and editable on the Options
 * page. The background registers this script for exactly the hosts the rules
 * match (chrome.scripting), so adding a site is a config change, not a code
 * change.
 */

// The ▛▞▟ mark in the three brand colors (lime ▛, cyan ▞, purple ▟), inlined so
// the button needs no web-accessible resource.
const MARK_SVG = `
<svg viewBox="0 0 128 128" width="18" height="18" aria-hidden="true" style="flex:0 0 auto">
  <g fill="#B0D903"><rect x="9" y="47" width="17" height="17"/><rect x="26" y="47" width="17" height="17"/><rect x="9" y="64" width="17" height="17"/></g>
  <g fill="#06AFAA"><rect x="64" y="47" width="17" height="17"/><rect x="47" y="64" width="17" height="17"/></g>
  <g fill="#6F2CA8"><rect x="102" y="47" width="17" height="17"/><rect x="85" y="64" width="17" height="17"/><rect x="102" y="64" width="17" height="17"/></g>
</svg>`;

let rules = [];

// extAlive reports whether this content script's extension context is still
// valid. After the extension is reloaded/updated, an already-injected content
// script keeps running in the page, but every chrome.* call then throws
// "Extension context invalidated" (and chrome.runtime can read back undefined).
// Guarding turns those into no-ops instead of uncaught page errors.
function extAlive() {
  try { return !!(chrome.runtime && chrome.runtime.id); } catch { return false; }
}

// sendNav posts a deep-link navigation to the background, safely — a click can
// land right after the context was torn down.
function sendNav(payload) {
  if (!extAlive()) return;
  try { chrome.runtime.sendMessage({ type: "dtui-nav", payload }); } catch { /* context gone */ }
}

// base62ToUuid decodes a short id (the 22-char base62 form some apps use in
// URLs like …/chat?id=conv_<short>) back to the canonical UUID that appears on
// spans as the business-id attribute. Alphabet 0-9A-Za-z, big-endian over the
// 16 UUID bytes — validated round-trip against a live Gen3 tenant (see
// docs/dev/learnings.md §1.23). Returns null on bad input.
const B62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz";
function base62ToUuid(s) {
  let n = 0n;
  for (const c of s) {
    const i = B62.indexOf(c);
    if (i < 0) return null;
    n = n * 62n + BigInt(i);
  }
  let h = n.toString(16);
  if (h.length > 32) return null; // overflows 128 bits — not a UUID
  h = h.padStart(32, "0");
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20, 32)}`;
}

// decodeId applies a rule's optional `decode` transform to a captured id.
function decodeId(raw, mode) {
  if (mode === "base62-uuid") return base62ToUuid(raw);
  return raw;
}

// matchPatternToRegExp converts a Chrome match pattern (scheme/host/path) to a
// RegExp, so a rule's `matches` can be tested against the current URL.
function matchPatternToRegExp(pattern) {
  const m = /^(\*|https?|file):\/\/(\*|(?:\*\.)?[^/*]+|)(\/.*)$/.exec(pattern);
  if (!m) return /$.^/; // never matches
  const [, scheme, host, path] = m;
  // Escape regex metacharacters, including `*` — the path's `*` is turned into
  // `.*` below, so it must first be escaped to `\*` for that replace to catch it.
  const esc = (s) => s.replace(/[.+?^${}()|[\]\\*]/g, "\\$&");
  const schemeRe = scheme === "*" ? "https?" : scheme;
  let hostRe;
  if (host === "*") hostRe = "[^/]+";
  else if (host.startsWith("*.")) hostRe = "(?:[^/]+\\.)?" + esc(host.slice(2));
  else hostRe = esc(host);
  const pathRe = esc(path).replace(/\\\*/g, ".*");
  return new RegExp("^" + schemeRe + "://" + hostRe + pathRe + "$");
}

function ruleAppliesHere(rule) {
  return (rule.matches || []).some((p) => matchPatternToRegExp(p).test(location.href));
}

// payloadFor turns an action + extracted id into a dtuiNavigate payload.
// Optional action.lens / action.timeframe are passed through to the TUI.
function payloadFor(action, id) {
  const base = action.target === "trace"
    ? { target: "trace", id }
    : { target: action.target, facets: [{ field: action.field, value: id }] };
  if (action.lens) base.lens = action.lens;
  if (action.timeframe) base.timeframe = action.timeframe;
  // environment is the target tenant URL (portable across users); dtui
  // auto-selects the configured environment whose host matches.
  if (action.environment) base.environment = action.environment;
  return base;
}

function makeButton(label, payload) {
  const btn = document.createElement("button");
  btn.className = "fab";
  // MARK_SVG is static/trusted; the label comes from user-imported rule bundles,
  // so it goes in as text (never HTML) to keep a bundle from injecting into the
  // host page DOM.
  btn.innerHTML = MARK_SVG;
  const labelSpan = document.createElement("span");
  labelSpan.textContent = label;
  btn.appendChild(labelSpan);
  btn.addEventListener("click", (e) => {
    e.preventDefault();
    e.stopPropagation();
    sendNav(payload);
  });
  return btn;
}

const SHARED_STYLE = `
  .fab { display:inline-flex; align-items:center; gap:6px; padding:5px 10px;
    background:#1b1e26; color:#cfe0ff; border:1px solid rgba(122,183,255,0.5);
    border-radius:18px; box-shadow:0 3px 12px rgba(0,0,0,0.35); cursor:pointer;
    font:12px/1.2 -apple-system,Segoe UI,Roboto,sans-serif; white-space:nowrap;
    vertical-align:middle; }
  .fab:hover { border-color:#7ab7ff; background:#222634; }`;

// Styling for the launcher pill + its grouped popover menu.
const MENU_STYLE = `
  .launcher .badge { background:#2f4b7a; color:#cfe0ff; border-radius:10px;
    padding:1px 7px; font-size:11px; margin-left:2px; }
  .menu { background:#1b1e26; border:1px solid rgba(122,183,255,0.5);
    border-radius:10px; box-shadow:0 6px 22px rgba(0,0,0,0.45); padding:6px;
    min-width:220px; max-height:60vh; overflow:auto;
    font:12px/1.3 -apple-system,Segoe UI,Roboto,sans-serif; }
  .menu[hidden] { display:none; }
  .menu .grp { color:#7f8aa3; font-size:10px; text-transform:uppercase;
    letter-spacing:0.6px; padding:6px 8px 2px; }
  .menu .item { display:flex; width:100%; text-align:left; background:transparent;
    color:#cfe0ff; border:0; border-radius:6px; padding:7px 8px; cursor:pointer;
    font:inherit; }
  .menu .item:hover { background:#222634; }`;

// --- floating launcher for ambient ids (no DOM anchor) ---------------------
//
// URL-sourced ids and network-sourced hits have no element to anchor to, so
// they collect into one launcher at bottom-right. A single jump renders as a
// lone pill; several jumps collapse into a dtui launcher whose click opens a
// small menu, sectioned by each action's optional `group` (e.g. Trace / RUM /
// GenAI) — so multiple signals on one page stay selectable without a wall of
// pills.

// ambientHits are ids captured from the network by the background — response
// headers (source: "header") and request URLs (source: "request", e.g. the RUM
// beacon's vi= param) — pushed over a runtime message. Newest first, capped:
// beacons and headers stream.
let ambientHits = [];

// collectActions gathers every ambient jump available on the current page:
// url-sourced rules evaluated here, plus the network hits from the background.
function collectActions() {
  const actions = [];
  for (const rule of rules) {
    if (rule.source !== "url" || !rule.urlRegex || !ruleAppliesHere(rule)) continue;
    let m;
    try { m = location.href.match(new RegExp(rule.urlRegex, "i")); } catch { continue; }
    if (!m || !m[1]) continue;
    const id = decodeId(m[1], rule.decode);
    if (!id) continue;
    for (const a of rule.actions || []) actions.push({ label: a.label, group: a.group, payload: payloadFor(a, id) });
  }
  for (const h of ambientHits) actions.push({ label: h.label, group: h.group, payload: h.payload });
  return actions;
}

let fabHost, menuOpen = false;
function renderFabs() {
  const actions = collectActions();
  if (actions.length === 0) { if (fabHost) fabHost.style.display = "none"; menuOpen = false; return; }
  if (!fabHost) {
    fabHost = document.createElement("div");
    fabHost.style.cssText = "position:fixed;right:16px;bottom:16px;z-index:2147483647;";
    document.documentElement.appendChild(fabHost);
    fabHost.attachShadow({ mode: "open" });
  }
  fabHost.style.display = "block";
  const root = fabHost.shadowRoot;

  // One jump: a single pill, no menu (the common case — no regression).
  if (actions.length === 1) {
    menuOpen = false;
    root.innerHTML = `<style>${SHARED_STYLE}
      .wrap{display:flex;flex-direction:column;align-items:flex-end;gap:8px}</style><div class="wrap"></div>`;
    root.querySelector(".wrap").appendChild(makeButton(actions[0].label, actions[0].payload));
    return;
  }

  // Several jumps: a launcher pill with a count and a grouped popover menu.
  root.innerHTML = `<style>${SHARED_STYLE}${MENU_STYLE}
    .wrap{display:flex;flex-direction:column;align-items:flex-end;gap:8px}</style>
    <div class="wrap">
      <div class="menu" hidden></div>
      <button class="fab launcher">${MARK_SVG}<span>dtui</span><span class="badge">${actions.length}</span></button>
    </div>`;
  const menu = root.querySelector(".menu");
  const launcher = root.querySelector(".launcher");
  buildMenu(menu, actions);
  menu.hidden = !menuOpen;
  launcher.addEventListener("click", (e) => {
    e.preventDefault();
    e.stopPropagation();
    menuOpen = !menuOpen;
    menu.hidden = !menuOpen;
  });
}

// buildMenu fills the popover, one section per distinct `group` (first-seen
// order); ungrouped actions fall under no heading. Each item is one navigate.
function buildMenu(menu, actions) {
  menu.innerHTML = "";
  const order = [];
  const byGroup = new Map();
  for (const a of actions) {
    const g = a.group || "";
    if (!byGroup.has(g)) { byGroup.set(g, []); order.push(g); }
    byGroup.get(g).push(a);
  }
  for (const g of order) {
    if (g) {
      const h = document.createElement("div");
      h.className = "grp";
      h.textContent = g;
      menu.appendChild(h);
    }
    for (const a of byGroup.get(g)) {
      const item = document.createElement("button");
      item.className = "item";
      // Label is user-imported rule data — set as text, not HTML (XSS).
      const itemLabel = document.createElement("span");
      itemLabel.textContent = a.label;
      item.appendChild(itemLabel);
      item.addEventListener("click", (e) => {
        e.preventDefault();
        e.stopPropagation();
        sendNav(a.payload);
        menuOpen = false;
        menu.hidden = true;
      });
      menu.appendChild(item);
    }
  }
}

// --- inline buttons for DOM-sourced ids ------------------------------------

let decorated = new WeakSet(); // elements we've already tagged
// resetDecorations removes every inline button we injected and forgets which
// elements were tagged — used when the rules change so stale buttons don't
// linger and elements can be re-evaluated against the new rules.
function resetDecorations() {
  document.querySelectorAll("span[data-dtui-holder]").forEach((el) => el.remove());
  decorated = new WeakSet();
}
function renderDom() {
  for (const rule of rules) {
    if (rule.source !== "dom" || !rule.selector || !ruleAppliesHere(rule)) continue;
    let els;
    try { els = document.querySelectorAll(rule.selector); } catch { continue; }
    for (const el of els) {
      if (decorated.has(el)) continue;
      let raw = rule.attribute ? el.getAttribute(rule.attribute) : el.textContent;
      raw = (raw || "").trim();
      if (!raw) continue;
      if (rule.textRegex) {
        let m;
        try { m = raw.match(new RegExp(rule.textRegex, "i")); } catch { m = null; }
        if (!m) continue;
        raw = m[1] != null ? m[1] : m[0];
      }
      raw = decodeId(raw, rule.decode);
      if (!raw) continue;
      decorated.add(el);
      const holder = document.createElement("span");
      holder.setAttribute("data-dtui-holder", "");
      holder.style.cssText = "margin-left:6px;display:inline-flex;gap:6px;vertical-align:middle;";
      holder.attachShadow({ mode: "open" });
      holder.shadowRoot.innerHTML = `<style>${SHARED_STYLE}
        .wrap{display:inline-flex;gap:6px}</style><span class="wrap"></span>`;
      const wrap = holder.shadowRoot.querySelector(".wrap");
      for (const a of rule.actions || []) wrap.appendChild(makeButton(a.label, payloadFor(a, raw)));
      el.insertAdjacentElement("afterend", holder);
    }
  }
}

function renderAll() { renderFabs(); renderDom(); }

// --- lifecycle -------------------------------------------------------------

function loadRules() {
  if (!extAlive()) return;
  chrome.storage.local
    .get("deepLinkRules")
    .then(({ deepLinkRules }) => {
      rules = Array.isArray(deepLinkRules) ? deepLinkRules : [];
      renderAll();
    })
    .catch(() => { /* context torn down between the guard and the call */ });
}

// Many target apps are SPAs: the URL and DOM change without a document
// load. Re-evaluate on history navigation and on DOM mutations (debounced).
function watch() {
  let last = location.href;
  const tick = () => { if (location.href !== last) { last = location.href; renderAll(); } };
  for (const m of ["pushState", "replaceState"]) {
    const orig = history[m];
    history[m] = function (...args) { const r = orig.apply(this, args); tick(); return r; };
  }
  window.addEventListener("popstate", tick);
  let raf = 0;
  const obs = new MutationObserver(() => {
    tick();
    if (raf) return;
    raf = requestAnimationFrame(() => { raf = 0; renderDom(); });
  });
  obs.observe(document.documentElement, { childList: true, subtree: true });
  // Live-update when the rules are edited on the Options page.
  chrome.storage.onChanged.addListener((changes, area) => {
    if (area === "local" && changes.deepLinkRules) { resetDecorations(); loadRules(); }
  });

  // Ambient network hits arrive from the background (webRequest), which can't
  // render UI: response-header ids (source:"header") and request-URL ids
  // (source:"request", e.g. the RUM beacon's vi= param). Decode + build the
  // payload here (reusing the same helpers), then add to the launcher; dedupe
  // by payload, keep the newest few.
  chrome.runtime.onMessage.addListener((msg) => {
    if (!msg || msg.type !== "dtui-ambient-hit") return;
    let changed = false;
    for (const hit of msg.hits || []) {
      const id = decodeId(hit.id, hit.decode);
      if (!id) continue;
      const payload = payloadFor(hit.action, id);
      const key = JSON.stringify(payload);
      if (ambientHits.some((h) => h.key === key)) continue;
      const short = id.length > 12 ? id.slice(0, 12) + "…" : id;
      ambientHits.unshift({ label: `${hit.action.label} · ${short}`, group: hit.action.group, payload, key });
      if (ambientHits.length > 8) ambientHits.length = 8;
      changed = true;
    }
    if (changed) renderFabs();
  });

  // Clicking anywhere outside the launcher closes an open menu. Shadow-DOM
  // clicks retarget to fabHost, so composedPath tells inside from outside.
  document.addEventListener("click", (e) => {
    if (!menuOpen || !fabHost) return;
    if (e.composedPath && e.composedPath().includes(fabHost)) return;
    menuOpen = false;
    const menu = fabHost.shadowRoot && fabHost.shadowRoot.querySelector(".menu");
    if (menu) menu.hidden = true;
  }, true);
}

loadRules();
watch();
