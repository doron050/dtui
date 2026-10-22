/* dtui Options page.
 *
 * Manages two things and compiles each down to the flat storage contracts that
 * sidepanel.js / background.js / content.js already consume — so those files
 * stay unchanged:
 *
 *   environments[] + activeEnvId   →  tenantUrl, token       (active env mirror)
 *   ruleSets[] (each enabled?)     →  deepLinkRules           (enabled sets' rules)
 *
 * Credential handling: device-local tokens in extension storage. See
 * docs/design/WASM_CHROME_EXTENSION.md for the security tradeoff vs. the OS
 * keyring the native binary uses, and the OAuth path a hardened extension prefers.
 */
const $ = (id) => document.getElementById(id);
const uid = () => (crypto.randomUUID ? crypto.randomUUID() : String(Date.now() + Math.random()));

function flash(el, msg, kind = "ok") {
  el.hidden = false;
  el.textContent = msg;
  el.className = "status " + kind;
  if (kind === "ok") setTimeout(() => (el.hidden = true), 1800);
}

// ─── in-memory model ─────────────────────────────────────────────────────
let envs = []; // {id,name,url,token}
let activeEnvId = "";
let sets = []; // {id,name,description,enabled,rules[]}

// ─── load + one-time migration from the old flat schema ───────────────────
async function load() {
  const s = await chrome.storage.local.get([
    "environments", "activeEnvId", "tenantUrl", "token", "ruleSets", "deepLinkRules",
  ]);

  if (Array.isArray(s.environments) && s.environments.length) {
    // Env ids are interpolated raw into element id="…" attributes (unlike
    // name/url/token, which are escapeAttr'd), so a tampered storage id could
    // inject markup into this privileged Options page. Normal ids are UUIDs, so
    // restricting to the UUID charset is lossless for real data and neutralizes
    // anything else.
    envs = s.environments.map((e) => ({ ...e, id: sanitizeId(e.id) || uid() }));
    activeEnvId = sanitizeId(s.activeEnvId);
    if (!envs.some((e) => e.id === activeEnvId)) activeEnvId = envs[0].id;
  } else {
    // Migrate a single legacy tenant, or start with one blank environment.
    envs = [{ id: uid(), name: "Default", url: s.tenantUrl || "", token: s.token || "" }];
    activeEnvId = envs[0].id;
  }

  if (Array.isArray(s.ruleSets)) {
    sets = s.ruleSets;
  } else if (Array.isArray(s.deepLinkRules) && s.deepLinkRules.length) {
    // Wrap pre-existing flat rules into one enabled set.
    sets = [{ id: uid(), name: "Imported rules", description: "", enabled: true, rules: s.deepLinkRules }];
  } else {
    sets = [];
  }

  renderEnvs();
  renderSets();
}

// ─── environments ─────────────────────────────────────────────────────────
function activeEnv() {
  return envs.find((e) => e.id === activeEnvId) || envs[0];
}

async function persistEnvs() {
  const active = activeEnv();
  await chrome.storage.local.set({
    environments: envs,
    activeEnvId: active ? active.id : "",
    // Mirrors read by sidepanel.js (auth) and background.js (Origin-strip rule).
    tenantUrl: active ? (active.url || "").replace(/\/+$/, "") : "",
    token: active ? active.token || "" : "",
  });
}

// Read edited input values back into the model (before add/remove re-renders).
function syncEnvsFromDom() {
  for (const e of envs) {
    const name = $(`env-name-${e.id}`);
    const url = $(`env-url-${e.id}`);
    const token = $(`env-token-${e.id}`);
    if (name) e.name = name.value.trim();
    if (url) e.url = url.value.trim();
    if (token) e.token = token.value.trim();
  }
}

function renderEnvs() {
  $("env-count").textContent = envs.length ? `${envs.length} configured` : "";
  const list = $("env-list");
  list.textContent = "";
  for (const e of envs) {
    const card = document.createElement("div");
    card.className = "card" + (e.id === activeEnvId ? " active" : "");

    const top = document.createElement("div");
    top.className = "row";
    top.innerHTML = `
      <label class="radio"><input type="radio" name="active-env" ${e.id === activeEnvId ? "checked" : ""} />
        <span>${e.id === activeEnvId ? "active" : "set active"}</span></label>
      <div class="spacer"></div>
      <button class="icon" title="Remove environment">Remove</button>`;
    top.querySelector("input").addEventListener("change", () => {
      syncEnvsFromDom();
      activeEnvId = e.id;
      renderEnvs();
    });
    top.querySelector("button").addEventListener("click", () => {
      syncEnvsFromDom();
      envs = envs.filter((x) => x.id !== e.id);
      if (activeEnvId === e.id) activeEnvId = envs[0] ? envs[0].id : "";
      renderEnvs();
    });
    card.appendChild(top);

    const grid = document.createElement("div");
    grid.style.cssText = "display:grid;grid-template-columns:1fr 2fr;gap:10px;margin-top:10px";
    grid.innerHTML = `
      <label class="field"><span class="lbl">Name</span>
        <input type="text" id="env-name-${e.id}" placeholder="prod" value="${escapeAttr(e.name)}" /></label>
      <label class="field"><span class="lbl">Tenant URL</span>
        <input type="url" id="env-url-${e.id}" placeholder="https://abc12345.apps.dynatrace.com" value="${escapeAttr(e.url)}" /></label>`;
    card.appendChild(grid);

    const tok = document.createElement("label");
    tok.className = "field";
    tok.style.marginTop = "10px";
    tok.innerHTML = `<span class="lbl">Platform token (storage:*:read)</span>
      <input type="password" id="env-token-${e.id}" placeholder="dt0s16.XXXX… or an OAuth access token" value="${escapeAttr(e.token)}" />`;
    card.appendChild(tok);

    list.appendChild(card);
  }
}

$("env-add").addEventListener("click", () => {
  syncEnvsFromDom();
  const e = { id: uid(), name: "", url: "", token: "" };
  envs.push(e);
  if (!activeEnvId) activeEnvId = e.id;
  renderEnvs();
});

$("env-save").addEventListener("click", async () => {
  syncEnvsFromDom();
  const bad = envs.find((e) => e.url && !/^https?:\/\//i.test(e.url));
  if (bad) { flash($("env-status"), "URL must start with http(s)://", "err"); return; }
  await persistEnvs();
  const a = activeEnv();
  flash($("env-status"), a && a.url ? `saved — active: ${a.name || a.url}` : "saved");
});

// ─── rule sets ──────────────────────────────────────────────────────────
async function persistSets() {
  const effective = sets.filter((s) => s.enabled).flatMap((s) => s.rules || []);
  await chrome.storage.local.set({ ruleSets: sets, deepLinkRules: effective });
}

function ruleCount(set) {
  const n = (set.rules || []).length;
  return `${n} rule${n === 1 ? "" : "s"}`;
}

function renderSets() {
  const enabled = sets.filter((s) => s.enabled).length;
  $("set-count").textContent = sets.length ? `${sets.length} set${sets.length === 1 ? "" : "s"}, ${enabled} enabled` : "";
  $("set-empty").hidden = sets.length > 0;
  const list = $("set-list");
  list.textContent = "";

  for (const set of sets) {
    const card = document.createElement("div");
    card.className = "card";

    const head = document.createElement("div");
    head.className = "set-head";
    head.innerHTML = `
      <label class="switch"><input type="checkbox" ${set.enabled ? "checked" : ""} /><span class="track"></span></label>
      <span class="title">${escapeHtml(set.name || "(unnamed)")}</span>
      <span class="meta">${ruleCount(set)}</span>
      <div class="spacer"></div>
      <button class="icon" title="Remove rule set">Remove</button>`;
    head.querySelector("input").addEventListener("change", async (ev) => {
      set.enabled = ev.target.checked;
      await persistSets();
      renderSets();
    });
    head.querySelector("button").addEventListener("click", async () => {
      sets = sets.filter((x) => x.id !== set.id);
      await persistSets();
      renderSets();
    });
    card.appendChild(head);

    if (set.description) {
      const d = document.createElement("div");
      d.className = "set-desc";
      d.textContent = set.description;
      card.appendChild(d);
    }

    const det = document.createElement("details");
    const sum = document.createElement("summary");
    sum.textContent = "View / edit rules (JSON)";
    det.appendChild(sum);
    const ta = document.createElement("textarea");
    ta.spellcheck = false;
    ta.value = JSON.stringify(set.rules || [], null, 2);
    det.appendChild(ta);
    const bar = document.createElement("div");
    bar.className = "row";
    bar.style.marginTop = "8px";
    const saveBtn = document.createElement("button");
    saveBtn.className = "primary";
    saveBtn.textContent = "Save changes";
    const st = document.createElement("span");
    st.className = "status";
    st.hidden = true;
    bar.appendChild(saveBtn);
    bar.appendChild(st);
    det.appendChild(bar);
    saveBtn.addEventListener("click", async () => {
      let rules;
      try { rules = JSON.parse(ta.value); } catch (e) { flash(st, "JSON: " + e.message, "err"); return; }
      const problem = validateRules(rules);
      if (problem) { flash(st, problem, "err"); return; }
      set.rules = rules;
      ta.value = JSON.stringify(rules, null, 2);
      await persistSets();
      renderSets();
    });
    card.appendChild(det);

    list.appendChild(card);
  }
}

// bundleRules extracts the rules array from a bundle (a bare array, or an object
// with a "rules" array).
function bundleRules(parsed) {
  if (Array.isArray(parsed)) return parsed;
  if (parsed && Array.isArray(parsed.rules)) return parsed.rules;
  throw new Error('expected a JSON array, or an object with a "rules" array');
}

// addBundle turns an imported/parsed bundle into a rule set, upserting by name.
async function addBundle(parsed, fallbackName) {
  const rules = bundleRules(parsed);
  const problem = validateRules(rules);
  if (problem) throw new Error(problem);
  const name = (parsed && !Array.isArray(parsed) && parsed.name) || fallbackName || "Imported rules";
  const description = (parsed && !Array.isArray(parsed) && parsed.description) || "";
  const existing = sets.find((s) => s.name === name);
  if (existing) {
    existing.description = description;
    existing.rules = rules;
    existing.enabled = true;
  } else {
    sets.push({ id: uid(), name, description, enabled: true, rules });
  }
  await persistSets();
  renderSets();
  return name;
}

// validateRules returns an error string, or "" if the rules array is well-formed.
function validateRules(rules) {
  if (!Array.isArray(rules)) return "rules must be an array";
  for (const [i, r] of rules.entries()) {
    const at = `rule ${i}${r && r.name ? ` (${r.name})` : ""}`;
    if (!r || typeof r !== "object") return `${at}: not an object`;
    if (!Array.isArray(r.matches) || r.matches.length === 0) return `${at}: "matches" must be a non-empty array`;
    if (!["url", "dom", "header", "request"].includes(r.source)) return `${at}: "source" must be "url", "dom", "header", or "request"`;
    if (r.source === "url" && !r.urlRegex) return `${at}: url source needs "urlRegex"`;
    if (r.source === "dom" && !r.selector) return `${at}: dom source needs "selector"`;
    if (r.source === "header" && !r.header) return `${at}: header source needs "header" (e.g. "traceresponse")`;
    if (r.source === "request" && !r.urlRegex) return `${at}: request source needs "urlRegex" (capture group 1 = the id)`;
    if (r.decode != null && r.decode !== "base62-uuid") return `${at}: unknown "decode" (only "base62-uuid")`;
    if (!Array.isArray(r.actions) || r.actions.length === 0) return `${at}: "actions" must be a non-empty array`;
    for (const [j, a] of r.actions.entries()) {
      if (!a || !a.label || !a.target) return `${at} action ${j}: needs "label" and "target"`;
      if (a.target !== "trace" && !a.field) return `${at} action ${j}: non-trace target needs "field"`;
      if (a.group != null && typeof a.group !== "string") return `${at} action ${j}: "group" must be a string (a menu section label)`;
      if (a.timeframe != null && !/^[0-9]+[mhd]$/.test(a.timeframe)) return `${at} action ${j}: "timeframe" must look like 7d / 2h / 30m`;
      if (a.environment != null && typeof a.environment !== "string") return `${at} action ${j}: "environment" must be a string (a tenant URL or context name)`;
    }
  }
  return "";
}

$("insert-example").addEventListener("click", async () => {
  try { const n = await addBundle({ name: "Example", description: "Neutral template — edit or delete.", rules: [EXAMPLE_RULE] }); flash($("import-status"), `added “${n}”`); }
  catch (e) { flash($("import-status"), e.message, "err"); }
});

$("import-url-btn").addEventListener("click", async () => {
  const url = $("import-url").value.trim();
  if (!url) { flash($("import-status"), "enter a bundle URL", "err"); return; }
  try {
    const res = await fetch(url);
    if (!res.ok) throw new Error(`fetch failed: ${res.status}`);
    const n = await addBundle(JSON.parse(await res.text()), hostOf(url));
    flash($("import-status"), `imported “${n}”`);
    $("import-url").value = "";
  } catch (e) { flash($("import-status"), "import: " + e.message, "err"); }
});

$("import-file-btn").addEventListener("click", () => $("import-file").click());
$("import-file").addEventListener("change", async (e) => {
  const file = e.target.files && e.target.files[0];
  if (!file) return;
  try {
    const n = await addBundle(JSON.parse(await file.text()), file.name.replace(/\.[^.]+$/, ""));
    flash($("import-status"), `imported “${n}”`);
  } catch (ex) { flash($("import-status"), "import: " + ex.message, "err"); }
  e.target.value = ""; // allow re-selecting the same file
});

// ─── helpers ──────────────────────────────────────────────────────────────
function hostOf(u) { try { return new URL(u).hostname; } catch { return "Imported rules"; } }
function escapeHtml(s) { return String(s).replace(/[&<>]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;" }[c])); }
function escapeAttr(s) { return String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c])); }
// sanitizeId keeps only the UUID charset so a stored id can never break out of
// the id="…" attribute it is interpolated into.
function sanitizeId(v) { return String(v == null ? "" : v).replace(/[^A-Za-z0-9_-]/g, ""); }

load();
