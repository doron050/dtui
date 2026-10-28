// Smoke test for the dtui wasm build: boots the artifact under Node with the
// browser globals stubbed, feeds it a fake tenant + a canned DQL response, and
// checks the Bubble Tea program starts, registers its input/resize callbacks,
// and renders a home view. This is not the browser (no xterm.js) — it only
// proves the wasm links, instantiates, and drives the real TUI headlessly.
//
//   node wasm/smoke.mjs
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const uiDir = path.join(path.dirname(fileURLToPath(import.meta.url)), 'ui');

// Node lacks a couple of syscalls the Go wasm runtime probes for.
for (const f of ['getuid', 'getgid', 'geteuid', 'getegid']) {
  if (typeof process[f] !== 'function') process[f] = () => -1;
}
if (!process.env.HOME) process.env.HOME = '/home/web';

await import(path.join(uiDir, 'wasm_exec.js'));
const g = globalThis;

let captured = '';
// Multi-environment: two named environments; the session starts on 'primary'.
// A context-targeted deep link (below) must switch to 'secondary' and issue its
// queries against that host.
g.dtuiConfig = {
  environment: 'https://tenant.example.com',
  context: 'primary',
  environments: [
    { name: 'primary', url: 'https://tenant.example.com' },
    { name: 'secondary', url: 'https://second.example.com' },
  ],
};
const fetched = [];
g.dtuiTermWrite = (arr) => { captured += Buffer.from(arr).toString('latin1'); };
g.dtuiReady = () => console.log('[dtuiReady fired]');
g.dtuiExit = () => console.log('[dtuiExit fired]');
// Every dtctl HTTP call lands here; hand back an empty-but-successful DQL result.
g.dtuiFetch = (req) => {
  console.log(`[dtuiFetch] ${req.method} ${req.url}`);
  fetched.push(req.url);
  return Promise.resolve({
    status: 200,
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ result: { records: [], metadata: {} }, state: 'SUCCEEDED' }),
  });
};

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const strip = (s) =>
  s.replace(/\x1b\][^\x07]*\x07/g, '')
    .replace(/\x1b\[[0-9;?]*[A-Za-z]/g, '')
    .replace(/\x1b[>=()][A-Za-z0-9]?/g, '');

const go = new g.Go();
const wasm = fs.readFileSync(path.join(uiDir, 'dtui.wasm'));
const { instance } = await WebAssembly.instantiate(wasm, go.importObject);
console.log('[instantiated, running...]');
go.run(instance);

await sleep(300);
if (typeof g.dtuiResize === 'function') {
  g.dtuiResize(120, 32);
} else {
  console.log('!! dtuiResize not registered — Bubble Tea never reached ready()');
}
await sleep(800);

const beforeInput = strip(captured);

// --- input path: does a keystroke pushed through dtuiSendInput reach the
// Bubble Tea model? ':' opens the command bar; assert the view changes.
let inputOk = false;
if (typeof g.dtuiSendInput === 'function') {
  captured = '';
  g.dtuiSendInput(':');
  await sleep(300);
  const afterColon = strip(captured);
  captured = '';
  g.dtuiSendInput('p');
  g.dtuiSendInput('o');
  g.dtuiSendInput('d');
  g.dtuiSendInput('s');
  await sleep(300);
  const afterTyping = strip(captured);
  console.log('=== after ":pods" (last 400 chars) ===');
  console.log(afterTyping.slice(-400));
  // The command bar should now be visible / echoing input.
  inputOk = (afterColon + afterTyping).length > 0 &&
    /pods|:/.test(afterColon + afterTyping);
  console.log(`[input] dtuiSendInput registered, view reacted: ${inputOk}`);
} else {
  console.log('!! dtuiSendInput not registered — Go never reached ready()');
}

// --- deep-link path: does dtuiNavigate open the requested view? Send a
// genai navigate with a session.id facet and assert the view switches.
let navOk = false;
if (typeof g.dtuiNavigate === 'function') {
  captured = '';
  g.dtuiNavigate(JSON.stringify({
    target: 'traces',
    lens: 'genai',
    timeframe: '30d',
    facets: [{ field: 'acme.conversation_id', value: 'sess-smoke-1' }],
  }));
  await sleep(400);
  const afterNav = strip(captured);
  console.log('=== after dtuiNavigate(traces·genai) (last 400 chars) ===');
  console.log(afterNav.slice(-400));
  // The traces view with the genai lens should now be on screen (crumb shows
  // "traces·genai").
  navOk = /genai|gen_ai|GenAI/i.test(afterNav);
  console.log(`[navigate] dtuiNavigate registered, view switched: ${navOk}`);
} else {
  console.log('!! dtuiNavigate not registered — Go never reached ready()');
}

// --- :ctx switcher: typing ":ctx secondary" must switch the session to the
// second environment and route subsequent queries to its host.
let ctxKeyOk = false;
if (typeof g.dtuiSendInput === 'function') {
  g.dtuiSendInput('\x1b'); // close any command bar left open by earlier steps
  await sleep(100);
  const before = fetched.length;
  captured = '';
  for (const ch of ':ctx secondary') g.dtuiSendInput(ch);
  await sleep(200);
  g.dtuiSendInput('\r');
  await sleep(500);
  console.log('=== screen after ":ctx secondary" + enter (last 200) ===');
  console.log(strip(captured).slice(-200));
  const after = fetched.slice(before);
  console.log('=== requests after ":ctx secondary" ===');
  console.log(after.slice(-4).join('\n'));
  ctxKeyOk = after.some((u) => new URL(u).hostname === 'second.example.com');
  console.log(`[:ctx] keyboard switch routed queries to secondary: ${ctxKeyOk}`);
}

// --- context-targeted deep link: a navigate carrying another environment's
// URL must auto-select the matching context and route its queries there.
// (We are on 'secondary' now; target the primary URL and expect the switch
// back — proving URL→context resolution, not name matching.)
let ctxNavOk = false;
if (typeof g.dtuiNavigate === 'function') {
  const before = fetched.length;
  g.dtuiNavigate(JSON.stringify({
    target: 'traces',
    lens: 'genai',
    environment: 'https://tenant.example.com', // a URL, not a nickname
    facets: [{ field: 'acme.conversation_id', value: 'sess-smoke-2' }],
  }));
  await sleep(500);
  const after = fetched.slice(before);
  console.log('=== requests after environment-URL deep link ===');
  console.log(after.slice(-6).join('\n'));
  ctxNavOk = after.some((u) => new URL(u).hostname === 'tenant.example.com');
  console.log(`[context] deep-link URL auto-selected the matching env, queries hit primary: ${ctxNavOk}`);
} else {
  console.log('!! dtuiNavigate not registered — cannot test context switch');
}

console.log('=== initial render (last 800 chars, ANSI stripped) ===');
console.log(beforeInput.slice(-800));

const ok = typeof g.dtuiResize === 'function' && beforeInput.length > 0 &&
  inputOk && navOk && ctxKeyOk && ctxNavOk;
console.log(ok ? 'SMOKE: PASS' : 'SMOKE: FAIL');
process.exit(ok ? 0 : 1);
