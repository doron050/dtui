# Primary Grail Tags: columns + filter in dtui

## Context

Dynatrace's Unified Tagging Strategy establishes **primary Grail tags** — customer-defined
tags materialized with a `primary_tags.<key>` prefix on *all* signals (logs, spans, events,
problems) and on *all* relevant Smartscape-on-Grail nodes (hosts, pods, containers, …),
before pipeline routing. They are the scalable replacement for auto-tagging/management zones:
one field convention usable everywhere for filtering, segmentation, and columns.

This change teaches dtui to treat a user-declared set of primary-tag keys as:

1. **Columns** — auto-added to every Smartscape-node view and every signal table
   (logs/traces/spans/events), so the user always sees their ownership/cost/env context.
2. **A filter** — surfaced through the **existing `f` facet picker** (no dedicated shortcut): the
   `primary_tags.*` keys **populated on the view** appear as facet candidates (only filters that
   work — decision #5), and selecting one appends the usual `| filter primary_tags.<key> ==
   "<value>"` stage. Nothing new in the filter UI.

The declared set is defined globally in `.dtui.yaml` and can be extended per segment.
When the active segment(s) change, columns and the filter's candidate keys change accordingly.

### Notation: unified flat `primary_tags.<key>` (validated on a live tenant)

Primary tags use **one notation everywhere** — a flat dotted field `primary_tags.<key>` on both
signals and Smartscape-on-Grail nodes. There is **no** `primary_tags` map field (an early
hypothesis, disproven: `` primary_tags[`service_detection`] == "sdv1-demo" `` matched 0, while
flat `primary_tags.service_detection == "sdv1-demo"` matched 16 on `smartscapeNodes "PROCESS"`).

- **Filter (both):** `| filter primary_tags.<key> == "<value>"` — 16 matches on the populated
  process node; also works on spans/`dt.davis.events`.
- **Value picker (both):** `| fieldsSummary primary_tags.<key>, topValues:250` — returns real
  `{value,count}` (e.g. `sdv1-demo`, `sdv2-demo`, …).
- **Column read (both):** populated `primary_tags.*` fields appear in the full record
  (`PROCESS-0123456789ABCDEF` → `primary_tags.service_detection = "sdv1-demo"`), so read off the
  record with `Column.Field` — no projection needed where the spec keeps the full record.

### DQL mechanics — implementation instructions (validated on a live tenant)

1. **Project primary-tag fields freely — no signal/node branching.** `| fields x =
   primary_tags.<key>` and `| fieldsKeep …, primary_tags.<key>` are null-safe on both signals
   (spans/logs/events) and `smartscapeNodes` records: an absent key yields a null cell, never an
   error. So full-record specs read the tag off the record via `Column.Field`; projection specs
   (k8s.go) carry it as `alias = primary_tags.<key>` (or `takeAny(primary_tags.<key>)` under
   `summarize`). No need to guard signals differently.
2. **Filter and value-picker use the flat field directly.** `| filter primary_tags.<key> ==
   "<value>"` and `| fieldsSummary primary_tags.<key>, topValues:N` both work as-is on signals and
   nodes (validated: spans + `smartscapeNodes "PROCESS"`).
3. **Wrap the composed field in `catalog.escapeField`** (facets.go:131) unconditionally — verified
   correct live. `escapeField`'s `bareField` regex allows dots, so `primary_tags.app` passes
   through bare (and matches); a special-char key falls through to **whole-path** backticks
   `` `primary_tags.cost-center` `` — the only form that parses (segment-backticking
   `` primary_tags.`cost-center` `` is a PARSE_ERROR) and it stays null-safe + matches real values.
   So `escapeField("primary_tags."+key)` is correct for any customer key; no special helper needed.
4. **Drop the null bucket in `ParseFieldsSummary` (shared fix — impact verified).** `fieldsSummary`
   returns a `{value:null}` entry for any partially-populated field. Today `ParseFieldsSummary`
   (facets.go:332) keeps it as `Value:""` (via `FormatValue(nil)`), `facetValueMatches` passes it,
   and the picker renders a dead blank row (selecting it commits `| filter field == ""`, which
   matches nothing — Grail null ≠ `""`). **This is pre-existing and affects every facet, not just
   primary tags** — verified on two live tenants: `k8s.namespace.name` (null 297 / 27 938),
   `http.response.status_code` (null 19 431 / 288 101) both already show the blank row today, while
   always-present fields (`loglevel`, `span.name`) have none. Fixing it once in `ParseFieldsSummary`
   — skip entries whose value is null/empty — is a **pure improvement across all facets** (removes
   dead UI, loses nothing usable). No negative impact found.
5. **Smartscape-id literal scoping.** Node `id` and the metric dimension `dt.smartscape.service` are
   the same `smartscape-id` type and value (so a keyed lookup matches field-to-field with no cast).
   When scoping by a literal, cast it — `dt.smartscape.service == toSmartscapeId("SERVICE-…")` (or
   `id == toSmartscapeId(…)`); a bare string literal silently matches nothing, and `toUid()` /
   `contains(id, …)` do not work on smartscape ids (all validated live).

### Confirmed decisions (from user)

- **Field model:** strict `primary_tags.<key>` only. No classic `tags[]` array parsing, no
  `dt.entity.*` fetches. (AWS tags / k8s labels were used only as *analog* fields to validate
  DQL mechanics on a demo tenant — not part of the feature.)
- **YAML form:** bare keys, auto-prefixed. User writes `environment`; tool queries
  `primary_tags.environment`.
- **No dedicated shortcut.** Primary tags are just facet candidates in the existing `f` picker —
  no new key, no separate picker engine or pill slice.
- **Segment combination:** segment-declared keys go **first**, then the global set (ordered
  union, deduped). A selected primary-tag facet is a normal `| filter` stage that ANDs with the
  active segment(s) and any other facets.

### Coverage caveat

The **DQL mechanics above are tenant-agnostic** — they work wherever primary tags land, including
`HOST` and any other entity type; nothing about the feature is entity-type-specific. *Which* entities
carry populated tags is purely customer/environment configuration: the `primary_tags.*` fields exist
(null-safe) everywhere, populated only where enriched. The tenants available for validation (two demo tenants) are **host-less k8s demos**, so they happen
to show tags on `PROCESS`, `SERVICE` metrics, `spans` (~62%), and `dt.davis.events`, and empty on
`CONTAINER`/`K8S_POD`/`K8S_DEPLOYMENT`/`logs` — this is an artifact of those tenants, **not** a
statement about hosts or any entity type elsewhere. Columns/facets render present-but-empty where a
key is unpopulated (never an error); the feature is inert when no keys are configured. Implication for
tests: assert the column/facet *exists* and shows values on an enriched type (process/span) — don't
hard-code which entity types are or aren't enriched. DQL facts in `docs/dev/learnings.md` §1.21.

## Design

### 1. Config surface — `internal/workspace/workspace.go`

- Add to `Workspace` struct: `PrimaryTags []string  yaml:"primary-tags,omitempty"` (global set).
- Add to `Segment` struct: `PrimaryTags []string  yaml:"primary-tags,omitempty"` (per-segment set).
  `Segment.UnmarshalYAML` already handles scalar-or-map; the map branch just gains this field.
- `validate`: trim/dedupe keys, reject empty strings, cap count at **`MaxPrimaryTags = 5`**
  (mirror `MaxSegments` style). The cap is the *global + per-segment active* count — keep the
  column row readable and let flex-width handle layout (decision: no elision/truncation logic, just
  the count cap). A bad list degrades to a startup warning (existing pattern), never a failed launch.
- Keys are stored bare; the `primary_tags.` prefix is applied at query-build time (single helper,
  §4) so the config stays convention-agnostic.

### 2. Session state — `internal/tui/`

- Thread the global list from workspace into the app (alongside how `Segments`/`Timeframe`
  defaults flow today via `opts`/`app.go`).
- Add a resolver `activePrimaryTagKeys() []string` on the app/`dataSource`: ordered union of
  (a) `PrimaryTags` from each **currently-applied** segment (segment order preserved, first),
  then (b) the global `Workspace.PrimaryTags`, deduped **keeping first occurrence**. Recomputed
  whenever `applySegments` runs (segments.go:153) — the same seam that already calls
  `refreshAllViews()`.
- **Order is user-defined — never sorted.** The returned slice preserves the exact order the user
  listed keys in `.dtui.yaml` (each segment's keys in listed order, then the global keys in
  listed order; dedupe keeps the first occurrence, never reorders). Columns (§3) render in this
  order left-to-right, so the user controls column order by ordering the YAML list. No alphabetical
  or any other implicit sort anywhere in this path.
- Map bare key → field via one helper: `primaryTagField(key) = "primary_tags." + key`
  (escape via `catalog.escapeField`, facets.go:131, only when the key has special chars).
  **Same flat notation, and same null-safe projection, for signals and `smartscapeNodes` views** —
  no per-target branching for the field expression *or* for projection safety (see the DQL-mechanics
  instructions above). Column sourcing (§3) is a convenience choice, not a correctness constraint:
  full-record specs read the tag off the record, projection specs may project it — safe on any target.

### 3. Columns (Smartscape nodes + signal tables)

Add a **primary-tag column layer** — one `catalog.Column{Field:"primary_tags.<key>"}` per active
key, read via `Column.Text` (catalog.go:195) — appended to whatever columns the view resolves. The
columns follow `activePrimaryTagKeys()` order exactly (user-defined, never sorted — §2), so the YAML
list order *is* the column order.

**Placement — after the last non-metric column, before the metric columns.** Primary-tag columns
sit between the spec's regular columns and the metric/`Enrich` sparkline columns (REQUESTS/FAILED,
etc.), so the row reads: identity/attribute columns · primary tags · metrics. Insert at the
metric-column boundary, not at the end.

**Seam — wrap `columns()`, do not edit one branch.** `tableView.columns()` (table.go:111-126)
has **early returns**: `ScopeColumns` (logs pattern drill), `Lenses` (events/db/genai), and
`Dynamic` specs each return *before* the final `return v.spec.Columns`. Appending to that last line
only would miss those views. Instead wrap the whole result — the base resolution becomes an inner
helper and the exported `columns()` splices `primaryTagColumns(v.scope)` in at the metric-column
boundary (before the first `Enrich`/metric column; at the end when a view has no metric columns), so
lens / pattern-drill / dynamic views all gain the columns uniformly in the right slot.
`primaryTagColumns` derives from `activePrimaryTagKeys()`, so a segment switch (which reruns
`refreshAllViews`) changes the visible columns automatically. **Implementation note:** confirm the
metric-column boundary is identifiable across all spec shapes (Enrich vs inline metric columns); if a
spec shape makes the boundary ambiguous, raise it rather than guessing.

Column *sourcing* is a convenience choice (both forms are null-safe on every target):

- **Full-record specs** (logs / traces-spans / events; hosts `catalog/specs.go`,
  containers/processes `catalog/infra.go` — all `fieldsRemove`, keep the whole record): the
  `primary_tags.*` fields are already on the record, so the map-lookup column works with **no DQL
  projection**. Absent keys render empty.
- **Projection-based `smartscapeNodes` specs** (pods / nodes / workloads / namespaces / clusters,
  `catalog/k8s.go`): the explicit `fields`/`summarize` tail drops most record fields, so carry each
  active key through as `` <alias> = primary_tags.<key> `` (null-safe — missing → null, no error),
  or `takeAny(primary_tags.<key>)` for `summarize` specs. `Column.Value` reads the alias.
- A catalog test pins the flat `primary_tags.<key>` notation everywhere.

### 4. Primary-tag filter — through the existing `f` facet picker (no dedicated shortcut)

Primary tags are **ordinary facets**. There is no `T` key, no separate picker, no parallel pill
slice — a primary-tag filter is a `catalog.Facet{Field:"primary_tags.<key>", Value:"<value>"}` that
flows through the exact same machinery as every other facet. `Facet.Stage()` already emits
`| filter primary_tags.<key> == "v"`, `Facet.Label()` the pill, and `ComposeQuery`/`InjectStages`
(facets.go:261, table.go:155) compose it with searches, bucket filters, other facets, and (server-
side) the active segments. The value stage (`FieldsSummaryQuery` → `ParseFieldsSummary`) and free-
typed / `*`-wildcard entry (`applyFacet`, tablefacets.go:282) all work unchanged.

**`f` shows only filters that work — this is the *existing* behavior, confirmed, not a change.**
`facetCandidates` (tablefacets.go:132) already scans fetched record keys and offers only keys
*present* on records; dtctl **omits null fields from record JSON** (verified: an all-null key is
absent from the record), so an unpopulated key is never a candidate today. There is **no declared
floor** — a declared key with no values must not be offered (selecting it would build a dead
`| filter`). Consequences:

- **Full-record views** (logs / spans / hosts / containers / processes): any *populated*
  `primary_tags.*` on the fetched records shows up as a candidate with **zero new code** — declared
  *or* undeclared (peek and facets reflect what's present; columns are the curated/declared subset,
  decision #1). This is pure confirmation of existing behavior.
- **Projection-based node views** (pods/nodes/workloads, k8s.go): §3 must *project* the declared keys
  onto the record so the columns survive the field-strip — and a projected **all-null** key lands in
  JSON as explicit `"<key>": null` (verified on live pods), which the existing `facetCandidates` *would*
  pick up as a dead candidate (empty value picker). To keep "only filters that work" here, the **new**
  primary-tag path must apply a non-null guard (skip a projected key that is null across all fetched
  records) before exposing it as a facet — the column still shows (empty is fine, decision #1), the
  facet does not. This guard lives in the new code; `facetCandidates` itself is untouched.
- **Services**: candidates = discovered metric dimensions (§6).
- **API/REST views** (dashboards etc.): faceting is already DQL-only and refused there
  (tablefacets.go:60) — primary tags inherit that guard, nothing extra.

So no edit to the existing facet-candidate scan; the only primary-tag-specific facet work is the
projection-view non-null guard above.

### 4a. Primary-tag key discovery (signals + non-service nodes)

There is **no cheap DQL way to enumerate `primary_tags.*` keys** — `fieldsSummary primary_tags.*`
is a parse error, and dtui's `CensusQuery` (smartscape.go:189) is a node-*type* census, not a
field census. Primary tags also **differ per data source** (validated on a live tenant: spans →
`app/env/version`; `smartscapeNodes "CONTAINER"` → `myPrimaryTag`/`myprimarytag`), so a single
declared set can't be the whole story. Discovery is therefore **record-key scan + declared floor**:

- **Full-record views** — logs, traces/spans, and smartscape **hosts/containers/processes**
  (`fieldsRemove`, keep whole record): the existing `facetCandidates` (tablefacets.go:129) already
  scans fetched record keys, so `primary_tags.*` present on sampled rows auto-appear as `f`-facet
  candidates. **No new code** for these.
- **Projection-based node views** — pods/nodes/workloads (k8s.go strips fields): record-scan sees
  nothing until the §3 column projection lands the **declared keys** (`primary_tags.<key>`, null-safe)
  on the record. A projected key surfaces as a candidate, but because projection materializes even a
  null value the **new** path must apply a non-null guard so only keys *with values* are offered
  (decision #5; see §4). Column shows regardless; facet only where populated. (Fully dynamic discovery
  of *undeclared* keys here would need an extra unstripped probe; not done.)
- **Services**: metric-dimension discovery (§6), not record-scan.
- **Columns vs facets split (decision #1/#5):** columns render the **declared** keys (§3, may be
  empty where unpopulated); facet candidates are the **populated** keys on the record (declared or
  undeclared for full-record views; declared-and-populated for projection views). A declared key with
  no values shows an (empty) column but is **not** offered as a facet — "f only shows filters that work."
- Cache discovered keys per view/session; refresh on context/segment switch (**ADR-0010, accepted**).

### 5. Availability guard

- No declared keys and none discovered ⇒ no primary-tag columns and no primary-tag facet
  candidates. The feature is inert — zero regression, nothing new in the UI.

### 6. Special case: SERVICE entities filter on a metric union

**Why services are different.** `smartscapeNodes "SERVICE"` nodes carry no filterable dimensions
and no primary tags — those live only on the four service metrics (`dt.service.request.count`,
`dt.service.faas_invoke.count`, `dt.service.messaging.process.count`,
`dt.service.request.service_mesh.count`). Validated on a live tenant: filtering on
`matchesValue(primary_tags.service_detection, "sdv1-demo")` → 12 services;
`AND telemetry.sdk.language == "dotnet"` → 1.

**Implementation — two-pass `timeseries` roster joined to `smartscapeNodes`.** The list source
mirrors the shape of the Dynatrace services app (validated live). See `docs/dev/learnings.md §1.21.1`
for the full DQL mechanics.

- **Pass 1 — discovery (`Spec.DiscoverQuery`).** Samples raw series descriptors via the `metrics`
  command (0 datapoints). Scans record field names minus a bookkeeping exclude set to yield:
  - **dims** = all filterable dimensions → `f` facet candidates + `/` search fields.
  - **primaryTagKeys** = declared workspace keys (order preserved) unioned with discovered
    `primary_tags.*` dims → the `by:` + `takeAny` keys in pass 2.
  Cached per segment combination; re-runs only on segment/context change (ADR-0010).

- **Pass 2 — the roster.** `timeseries {…, scalar:true}, union:true` over the 4 metrics, joined
  to `smartscapeNodes "SERVICE"` in one query:

```
timeseries {
  req = sum(dt.service.request.count, scalar: true), faas = …, msg = …, mesh = …
},
union: true,
by: { dt.smartscape.service, dt.service.name, service.name, `primary_tags.<k>`, … },
filter: { isNotNull(dt.smartscape.service) [ AND <facet / search conditions> ] }
| summarize {
    dt.service.name = takeLast(dt.service.name),
    service.name    = takeLast(service.name),
    `primary_tags.<k>` = takeAny(`primary_tags.<k>`)  // null-ignoring; consistent per service
  }, by: { dt.smartscape.service }
| lookup [ smartscapeNodes "SERVICE" | fieldsRemove references ],
    sourceField: dt.smartscape.service, lookupField: id,
    fields: { name, lifetime, dt.service_detection.version }
| fieldsAdd id = dt.smartscape.service,
            name = coalesce(name, dt.service.name, service.name, getNodeName(dt.smartscape.service))
| sort name asc | limit 500
```

**Filter and search inject into `filter:{}` (the timeseries argument), not as `| filter` pipes.**
`filter:{}` narrows pre-aggregation and may reference fields absent from `by:` (validated live:
`k8s.workload.kind == "deployment"` narrows 19 → 16 without being in `by:`). This is why every
discovered dim is filterable even though only identity/primary-tag dims survive into the collapsed
row. `| search` is not legal on `timeseries`; `/` search uses a `contains(toString(<dim>), term,
caseSensitive:false)` OR-chain (`toString()` required — absent on numeric dims `contains` is
silent-empty, validated). Compound facet predicates are parenthesized in `filter:{}` to avoid
`AND a or b` mis-binding (validated).

**`lookup [ smartscapeNodes "SERVICE" ]`** folds name, lifetime, and detection version in the same
query so peek facts and SEEN are populated without a second round-trip. `getNodeName` and
`dt.service.name`/`service.name` are fallbacks for metric-only services with no smartscape node.
`takeAny` for primary tags is null-ignoring — a service tagged on some series and not others still
surfaces real values (validated live: `takeAny` collapses 54 series → 15 distinct services with
tags filled from non-null series).

**REQUESTS/FAILED sparklines stay the shared `Enrich` path** (batched timeseries, 120-row cap) —
same mechanism as every other entity view.

**Facet candidates = pass-1 discovered dims** (`facetCandidates` branches for `MetricRoster` specs).
No null guard needed — discovered dims come from populated raw series.

**Value picker = `ServiceFieldsSummaryQuery`**: `metrics from:<tf> | filter in(metric.key,{4}) |
fieldsSummary <dim>, topValues:250` on raw series (validated: `primary_tags.app` → demo-app/
spring-demo; `telemetry.sdk.language` → python/cpp).

**List is timeframe-dependent** (confirmed acceptable) — idle services drop off; the view is
metrics-first by design.

**Scope of change:** `catalog/catalog.go` (`Spec.DiscoverQuery`, `MetricRoster`, `ScanMetricDims`,
`ServiceMetricKeys`, `ServiceDimExclude`); `catalog/specs.go` (`servicesSpec` rewrite);
`catalog/facets.go` (`Facet.Condition`, `ContainsSearchCondition`, `ServiceFieldsSummaryQuery`);
`table.go` (discovery pre-pass via `discoverOwner`, `composeRosterDQL`);
`tablefacets.go` (`facetCandidates` + value-picker roster branches).

### 7. Primary tags in the curated facts panel (peek vs. detail-top rendering)

The **curated facts panel** — the `KeyFacts`-driven block shown both in the peek preview pane
(`tablepreview.go` `previewLines`, line 132) and at the **top** of the detail page (`inspector.go`
facts block) — shows only the per-type curated facts, so `primary_tags.*` never appears there. A
user filtering by `primary_tags.app` can't see it on the selected row. This section adds the
record's primary tags to that curated panel only.

**Scope guardrails (what does NOT change):**

- The inspector's **bottom "properties" section is untouched** — `primary_tags.*` still list there
  under their full keys (`primary_tags.app: …`), and `tags:*` maps render exactly as today. This
  change touches only the curated facts (peek + detail-top).
- Regular `tags:*` maps (k8s labels, aws tags) are **not** added to the curated panel — they stay in
  the unchanged bottom properties. Only `primary_tags.*` is surfaced in the curated panel.

**A. Node rows — two renderings, differing by available vertical space** (both zero-query, read off
the record in hand; both strip the `primary_tags.` prefix; both follow `activePrimaryTagKeys()`
order — user-defined per §2, discovered-but-undeclared keys appended after):

- **Peek preview pane** (more vertical space): **one line per primary tag**, same label/value spacing
  as other facts —

  ```
   app       demo-app
   env       demo
   version   0.1.0
  ```

- **Detail-top facts panel** (condensed): **a single line**, label `primary tags`, value is the
  space-joined `key:value` list —

  ```
   primary tags   app:demo-app env:demo version:0.1.0
  ```

- Empty when the record carries no primary tags (the line/lines are skipped, like any empty fact).
- **Carry through projection lists.** Full-record lists (hosts/containers/processes, `fieldsRemove`)
  already carry `primary_tags.*`; projection lists (pods/nodes/workloads/namespaces/clusters,
  k8s.go `summarize`/`fields`) drop them — carry the declared/discovered keys via
  `takeFirst(primary_tags.<key>)` so the peek (and the §3 columns) have them. Absent → null (safe).
- Validated shape on a live tenant: PROCESS carries flat `primary_tags.app/env/version` on the row.

**B. SERVICE rows — same as other entities for peek/detail-top.** Primary tags are carried on the
roster row via `takeAny(primary_tags.<K>)`, so the peek and detail-top curated panel read them off
the row exactly like any node — no special-casing.

**Service detail page (inspector)** uses `DetailQuery` dispatched to `ServiceDetailQuery` —
a raw `metrics` descriptor fetch scoped to the specific service:

```
metrics from:now() - 2h
| filter in(metric.key, {4 keys})
| filter dt.smartscape.service == toSmartscapeId("<id>")
```

This returns one row per series (0 datapoints scanned). The inspector folds all rows into a single
merged record: for each key, collects all distinct non-null values; single-value dims stay scalar,
multi-value dims are joined with `", "`. Validated live (faas-trigger): 66 series → 37 merged
fields including `k8s.namespace.name`, `primary_tags.*`, `dt.cost.costcenter`, `messaging.*`,
`faas.trigger` — fully dynamic, no hardcoded dim list. Primary tags from the roster row survive via
merge-not-replace (detail wins on conflict; roster fields absent from the detail query are kept).

## Implementation concerns — work through each; raise issues found

The DQL/tenant mechanics are validated; these are build-time unknowns. The implementer **must work
through every item below and surface (not silently paper over) any that don't hold** — each is a
place the generic engine's contract might not fit:

1. **Column placement boundary (§3).** Splice primary-tag columns *before the first metric/`Enrich`
   column, after the regular columns*. Verify the metric-column boundary is unambiguously
   identifiable across every spec shape (Enrich sparklines vs inline metric columns, lens/dynamic/
   scope-column early-return paths). If a shape makes it ambiguous, raise it.
2. **Projection carry-through (§3, k8s.go).** Wire each `summarize`/`fields` node spec to carry the
   active keys (`takeAny(primary_tags.<key>)` / `alias = primary_tags.<key>`, null-safe). Add a
   catalog test asserting **no spec emits a bare `fieldsKeep/fields primary_tags.*`** and that the
   flat notation + `escapeField` form is used. Raise any spec where the tail can't carry them.
3. **Shared null-bucket fix (rule 4).** Change is in `ParseFieldsSummary` (all facets). Impact
   verified benign (see rule 4), but add a regression assertion on an existing partially-populated
   facet (`http.response.status_code`) so the blank-row removal is pinned and no facet relies on it.
4. **Service two-pass wiring (§6).** Pass-1 key discovery + pass-2 collapse must present as **one
   logical load** to the datasource/table (not a visible double-fetch or flicker). Confirm the
   datasource contract accommodates a discover-then-query step; raise it if it forces a bespoke
   services datasource.
5. **Segment scoping on the `metrics` service query (§6).** Verify the executor applies active
   segments to the `metrics` command the same as to `fetch`/`smartscapeNodes`; if segments don't
   narrow `metrics`, raise it (services would otherwise ignore segment scope).
6. **Service value-picker variant (§6).** New `FieldsSummaryQuery` shape
   (`metrics | filter in(metric.key,{4}) | fieldsSummary <dim>`). Build + test it composes with the
   pre-`summarize` filter injection.
7. **`getNodeName` fallback (§6).** Name = `getNodeName(dt.smartscape.service)`, fallback
   `takeAny(dt.service.name)` for a metric-only service with no smartscape node. This fallback path
   is **untested** (no such service on the validation tenants) — flag if it can't be exercised in a test.
8. **Caching/refresh seam (§4a, ADR-0010).** Discovered key/dim set cached per session, refreshed on
   the `applySegments`/context-switch seam that already reruns `refreshAllViews`. Confirm no stale
   set survives a segment change.
9. **Projection-view facet non-null guard (§4/§4a).** `facetCandidates` is untouched, but projecting
   declared keys onto k8s projection views materializes `"<key>": null`, which the scan would offer as
   a dead candidate. The new path must skip a projected primary-tag key that is null across all
   fetched records (column still shows, facet doesn't). Full-record views need no guard (dtctl omits
   null keys). Verify the guard keys off the same fetched `v.all` the candidate scan uses.

## Critical files

- `internal/tui/catalog/specs.go` — rewrite `servicesSpec`: list source = the two-pass `metrics`
  collapse (§6); columns read the collapsed one-row-per-service result.
- `internal/tui/catalog/catalog.go` — `Spec` flag marking the service spec + its pass-1/pass-2 build;
  service-metric key list; identity dim (`dt.smartscape.service`).
- `internal/tui/catalog/metrics.go` — service `FieldsSummaryQuery` variant
  (`metrics | filter in(metric.key,{4}) | fieldsSummary`); the pass-2 `summarize takeAny(...),
  by:{dt.smartscape.service}` builder taking the pass-1 key set.
- `internal/tui/metrics.go` — reuse `discoverDims` (metrics.go:330) for pass-1: scan the sampled
  series for the service dimension + `primary_tags.*` key set fed into pass-2.
- `internal/workspace/workspace.go` — config structs, validation.
- `internal/tui/app.go` — thread global primary-tag list into session (no new key).
- `internal/tui/segments.go` — recompute active keys on `applySegments`.
- `internal/tui/table.go` — primary-tag column layer in `columns()`.
- `internal/tui/tablefacets.go` — inject declared/discovered `primary_tags.*` keys into
  `facetCandidates` so they appear in the existing `f` picker.
- `internal/tui/catalog/facets.go` — reuse `Facet`, `FieldsSummaryQuery`, `escapeField`; drop the
  null bucket in `ParseFieldsSummary` (DQL-mechanics rule 4); add a `primaryTagField` helper.
- `internal/tui/tablepreview.go` — peek preview: one line per primary tag after `KeyFacts`, prefix
  stripped, `activePrimaryTagKeys()` order (§7A).
- `internal/tui/inspector.go` — detail-top facts block: one condensed `primary tags` line. **Leave
  the bottom "properties" section unchanged** (§7 guardrail).
- `internal/tui/catalog/k8s.go` — carry `primary_tags.*` through the projection specs so projection
  node rows have them in the peek + columns (§7A).
- `internal/tui/detail.go` — SERVICE detail: the merged node+dimension property set from the
  per-service metric fetch (§7B). (Peek primary tags need no code here — carried on the row via §6.)

## Verification

- `make fmt lint test` — full ruleset must pass clean.
- **Workspace tests** (`internal/workspace`): parse a `.dtui.yaml` with global
  `primary-tags` and per-segment `primary-tags` (scalar + map segment forms); assert dedupe,
  cap, and warning-on-bad-value.
- **Catalog/DQL tests:** render a Smartscape spec and a signal spec with active keys; substring-
  assert flat `primary_tags.<key>` notation everywhere; assert projection-based node specs carry
  `<alias> = primary_tags.<key>` (or `takeAny(...)`). Assert `Facet.Stage()` emits
  `| filter primary_tags.env == "prod"`. For `servicesSpec`: the list query is the `metrics` command
  over the 4 service keys; pass-2 `summarize takeAny(...), by:{dt.smartscape.service}` names only the
  pass-1-discovered `primary_tags.*` keys (assert one row per service, keys not hardcoded, exploding
  dims absent from `by:`); a facet/primary-tag filter injects as a normal `| filter dim == "v"` pipe
  **before** the `summarize`; `/` emits a `| filter (contains(<dim>, "term", caseSensitive:false) or
  …)` pipe (never `| search`, never `matchesValue`/`matchesPhrase` — those are array/token-only).
- **Model tests** (`app_test.go` style): the declared/discovered `primary_tags.*` keys appear as
  `f`-facet candidates; selecting one runs the stubbed value query and committing a value adds a
  facet pill whose composed DQL gains the `| filter primary_tags.<key> == …` stage. Apply a segment
  carrying its own `primary-tags` and assert the column set + facet-candidate list change; unapply and
  assert revert. Use synthetic identifiers only.
- **Panel tests** (§7): a synthetic node record with `primary_tags.app/env/version` renders in the
  peek as one line per tag (prefix stripped, `activePrimaryTagKeys()` order) and in the detail-top as
  the single condensed `primary tags   app:… env:… version:…` line; the inspector's bottom properties
  are unchanged (still `primary_tags.app` etc.); a projection spec (pods) carries `primary_tags.*`
  through its `summarize`. For SERVICE, pass-2 `summarize takeAny(primary_tags.<K>), by:{dt.smartscape.
  service}` carries the pass-1-discovered keys onto each row (assert one row per service, null-ignoring,
  keys not hardcoded), so the peek renders them with no panel special-casing.
- **Live tenant sanity — run against two or more live tenants** (never `dt.entity`; `smartscapeNodes` only).
  The mechanics are already tenant-verified (see learnings §1.21); the implementation must re-confirm
  end-to-end on both: flat `primary_tags.*` columns + facet filtering on a signal view and a
  `smartscapeNodes` view, and the §6 service list (`metrics`-command roster collapsed via
  `summarize takeAny(...), by:{dt.smartscape.service}`, a pre-`summarize` `| filter` pipe narrow,
  primary-tag keys scanned from pass-1, and the `contains(...,caseSensitive:false)` search).
- **Panel sources (verified live):** node tag shapes — PROCESS → flat `primary_tags.app/env/version`;
  K8S_POD → `tags:k8s.labels`/`tags:k8s.annotations` maps. SERVICE merged panel —
  `metrics | filter <MetricScopeFilter(svc)> | filter in(metric.key,{4})` for one service returned 7
  series → **26 merged dimension properties, including `primary_tags.app/env/version`**, so a
  service's primary tags surface in the panel (§7B).
