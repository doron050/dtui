# ADR-0016: Detail panes show real Grail keys; app-derived values are labeled, not disguised

**Status:** Accepted · **Date:** 2026-07-24

## Context

The detail surfaces — the row **peek**, the `d`/**describe** record inspector,
and the `enter` **detail page** — render an entity's record. The `d` inspector
and the detail page's "properties" section dump the record's fields keyed by
their names. Users read these as the ground-truth Grail record: the exact keys a
DQL query would return, and could reproduce.

The app sometimes derives values Grail never returned for that record: an opaque
id resolved to a friendly name (ADR-0015), metric series stitched in from a
second query, a parsed manifest. Written under a Grail-looking key, such a value
masquerades as a real field in the raw dump — indistinguishable from what Grail
actually returns, and misleading (the user cannot reproduce it in DQL).

A convention already existed but was unwritten and applied inconsistently:
internal synthetic keys use a `__` prefix and the inspector hides them
(`inspector.go` skips `__`-prefixed keys in both render modes). Metric
enrichment (`__enrich.*`), the parsed manifest (`__k8s.object.parsed`), and the
vitals/signals action rows follow it — but `azure.subscription.name` leaked in
*without* the prefix and so appeared as a fake Grail key.

## Decision

In every detail pane (peek, `d`, `enter`), a field shown **by its key** must be
a **real Grail key**. Only two deviations are allowed, and only these:

1. **Curated summaries.** The KeyFacts block, the per-domain facts, and column
   titles use prose labels (`subscription`, `region`, `os`, `SUBSCRIPTION`).
   These are labels, not keys; each reads a real Grail field (or an explicitly
   app-derived value per the rule below).
2. **Grouping strips.** A tag map's context prefix (`tags:azure` → a group
   header with bare keys beneath) and the `primary_tags.` prefix may be stripped
   for grouping. The underlying key stays real.

Any value the app **derives** (not returned by the record's own query) must:

- be stored under a **`__`-prefixed synthetic key**, so it never appears as a
  Grail key in the raw dump; and
- be surfaced only through the **curated summaries** (KeyFacts, used by the peek
  and the `enter` detail page) under an explicit **display label deliberately
  unlike a Grail key** — e.g. `Azure subscription name` (spaces and capitals) —
  so the reader can tell at a glance that it is app-derived, not tenant data.

Corollary: the raw `d` record dump and the `enter` "properties" section contain
**only** real Grail keys, plus query-authored aliases the DQL itself names (e.g.
`display` from a `fieldsAdd`, which Grail does return). An app-derived value has
no place in the raw dump: its `__` key is hidden and it has no real twin, so `d`
simply does not show it — it lives only in the curated summaries. **No provider
key is ever hidden** — GCP / AWS / Azure raw keys always survive to describe.

The `__` keys are also excluded wherever the user *picks* a field, not only
where fields are read: the facet manager's "add by attribute" candidates skip
`__`-prefixed keys (`tableView.facetCandidates`). A synthetic field is not a
Grail field, so faceting one would compose a filter on a non-existent field and
silently return empty — the exact Grail trap. So the single rule "`__` = not a
Grail field, never shown as one" is enforced in both the inspector's field
groups and the facet candidate list.

## Rejected alternatives

- **Resolve app-derived values at render only, never writing the record.**
  Breaks catalog purity; the pure column/fact readers cannot reach session state
  (ADR-0015). Writing a hidden `__` field keeps the readers pure.
- **Write derived values under Grail-looking keys and expect readers to "just
  know."** The exact leak this ADR closes: unreproducible in DQL, misleading in
  a pane whose contract is "this is the record."

## Consequences

- The `__`-hide convention becomes a rule. New specs that stitch app-derived
  values follow the store-under-`__` + curated-label pattern: a KeyFacts entry
  surfaces the value in the peek and `enter` detail page; the `d` record
  inspector renders no KeyFacts, so the value does not appear there at all.
- ADR-0015's `azure.subscription.name` becomes `__azure.subscription.name`,
  surfaced as the `Azure subscription name` KeyFact (peek + `enter` only).
- Enforcement is by review plus the two `__` skips — the inspector's field
  groups and the facet candidate list; there is no schema check, so spec authors
  must uphold it.
