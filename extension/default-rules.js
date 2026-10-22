/* Deep-link rule engine — GENERIC. This extension ships with NO
 * customer-specific rules. Rules live in chrome.storage.local ("deepLinkRules"),
 * authored on the Options page or imported as a bundle.
 *
 * A customer application provides its own rule bundle — ideally
 * hosted in that app's own repository — and users import it by URL or file on
 * the Options page. See extension/examples/ for a sample bundle and README.
 *
 * Rule schema
 * -----------
 *   name     : label shown in the editor.
 *   matches  : Chrome match patterns — WHERE the rule applies (content script
 *              runs there; header rules also filter webRequest by these).
 *   source   : "url" | "dom" | "header" | "request".
 *   actions  : one or more jumps for the extracted id. Each:
 *                label     : button text / menu item.
 *                target    : a dtui view name/alias ("traces", "userevents",
 *                            …), or "trace" to open the waterfall for a trace id.
 *                field     : the Grail field to filter on (ignored for "trace";
 *                            the id becomes  | filter <field> == "<id>").
 *                lens      : optional lens name on the target view (e.g.
 *                            "genai" on traces).
 *                group     : optional menu-section label. When a page offers
 *                            several ambient jumps (url/header/request), they
 *                            collapse into one launcher whose menu is sectioned
 *                            by group (e.g. "Trace", "RUM", "GenAI").
 *                timeframe : optional window label ("7d", "30d", "2h");
 *                            defaults to the live session timeframe.
 *                environment : optional target tenant, given as its URL (e.g.
 *                            "https://abc12345.apps.dynatrace.com"). dtui
 *                            auto-selects the configured environment whose host
 *                            matches and switches to it before the jump. Using
 *                            the URL (not a local nickname) keeps a shared,
 *                            committed rule bundle portable across users. Omit
 *                            to use the active environment.
 *
 * source = "url":
 *   urlRegex : regex tested against location.href; capture group 1 is the id.
 *              URL-sourced ids have no DOM anchor, so their buttons appear as a
 *              floating stack (bottom-right).
 *   decode   : optional transform on the captured id before it becomes the
 *              filter value. "base62-uuid" decodes a base62-encoded UUID (the
 *              0-9A-Za-z alphabet) back to canonical UUID form.
 *
 * source = "dom":
 *   selector  : CSS selector for elements that carry/label the id.
 *   attribute : read the id from this attribute; omit to use the element text.
 *   textRegex : optional — capture group 1 from the attribute/text value.
 *              DOM-sourced buttons are injected inline, next to each match.
 *
 * source = "header":
 *   header    : response header name to read (e.g. "traceresponse"). Captured
 *               at the network layer (webRequest), so it works even when page
 *               CORS would hide the header from scripts.
 *   regex     : optional — capture group 1 from the header value (e.g.
 *               "^\\d\\d-([0-9a-f]{32})-" pulls the trace-id out of a W3C
 *               traceresponse). Header hits have no DOM anchor, so they show in
 *               the floating launcher (newest few).
 *
 * source = "request":
 *   urlRegex  : regex tested against each REQUEST url the page issues; capture
 *               group 1 is the id. Read at the network layer (webRequest,
 *               URL only — no body), so it catches ids that live in a request
 *               query param. The canonical case is the Dynatrace RUM beacon:
 *               "[?&]vi=([A-Za-z0-9]+-\\d+)" captures the visit id, which equals
 *               dt.rum.session.id in Grail. Like header hits, these have no DOM
 *               anchor and show in the floating launcher.
 */

// Generic engine: no rules by default.
var DEFAULT_RULES = [];

// A neutral template offered by the Options "Insert example" button, showing
// every knob (url + decode, facet field, lens, timeframe). Not a real site.
var EXAMPLE_RULE = {
  name: "Example — app URL → its spans",
  matches: ["https://app.example.com/*"],
  source: "url",
  urlRegex: "/orders/([0-9a-f-]{36})",
  // decode: "base62-uuid",  // uncomment if the URL id is a base62 UUID
  actions: [
    {
      label: "Spans for this order",
      target: "traces",
      lens: "genai",
      field: "my.order_id",
      timeframe: "30d",
      // environment: "https://abc12345.apps.dynatrace.com", // target tenant by URL
    },
  ],
};

// Usable both as an importScripts target in the service worker (self.*) and as
// a <script> in the Options page (window.*).
if (typeof self !== "undefined") {
  self.DEFAULT_RULES = DEFAULT_RULES;
  self.EXAMPLE_RULE = EXAMPLE_RULE;
}
