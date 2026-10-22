# ADR-0015: Provider-adaptive cloud columns & session-cached ID→name enrichment

**Status:** Accepted (enrichment mechanism amended by [ADR-0016](0016-detail-panes-show-real-grail-keys.md)) · **Date:** 2026-07-24

## Context

Cloud providers reach the TUI through the same generic entity browser
(`resourcesSpec`), reached from a per-provider census (`:aws`, `:azure`, …).
The types themselves are discovered, not hardcoded (ADR-0010): a census is
`smartscapeNodes "*" | filter startsWith(type, "<PREFIX>_") | summarize
by:{type}`, and any type browses via `Scope.Arg` with no bespoke screen.

But providers diverge in two ways that a single static column set can't serve:

1. **The account/region fields are named differently.** AWS carries
   `aws.region` / `aws.account.id`; Azure carries `azure.location` /
   `azure.subscription`. A fixed AWS column set leaves Azure rows blank.
2. **The account identifier is sometimes an opaque id, not a name.** Azure's
   `azure.subscription` is a 36-char GUID. The friendly name lives on a
   *different* node type (`AZURE_MICROSOFT_RESOURCES_SUBSCRIPTIONS`:
   `azure.subscription` → `name`), so a row can't render its own subscription
   name from its own fields.

Constraints: the catalog package is framework-free and its column `Value` funcs
and detail `KeyFacts` are **pure reads over one record** — they cannot reach
session state or issue queries. And Grail scan cost is real on large tenants
(ADR-0013), so re-reading a lookup table per browse is not free.

## Decision

**Provider-adaptive columns via the existing `Spec.ScopeColumns` seam.** One
browser, one spec. `ScopeColumns(scope)` picks the column set from the
type-prefix of `Scope.Arg` (AWS → REGION/ACCOUNT, Azure → REGION/SUBSCRIPTION),
falling through to the static `Columns` for unmatched providers. No duplicate
per-provider spec, no table-engine change — every column consumer already
routes through `ScopeColumns` before the static columns.

**ID→name resolution via a session-cached map, enriched at the `dataSource`
boundary — through a reusable `catalog.DerivedFieldResolver`.** A resolver
bundles a one-shot lookup query, a parse, and the source→dest key mapping;
`azureSubscriptionResolver` is the first entry in `DerivedFieldResolvers`.
Mirroring the semantic-dictionary cache (`dataSource.dict` +
`ensureDict`/`dictOwner`): the app fires every resolver's fetch once
(`ensureDerived`, at startup and on context switch — **centralized**, no
per-view trigger), fills `dataSource.derived[name]` (id→value) on the
`derivedOwner` result, and resets it on `switchContext`. Records are enriched
generically by `enrichRecords`, keyed on the resolver's `SourceKey` presence
(no provider string tests) — it writes a **`__`-prefixed synthetic field**
(`__azure.subscription.name`) that the pure catalog column and detail fact read
like any other field, but which is hidden from the raw record dump and surfaced
only through a deliberately non-Grail display label (`Azure subscription name`)
per [ADR-0016](0016-detail-panes-show-real-grail-keys.md). A late cache fill
re-broadcasts so live views re-enrich in place. All of this runs on bubbletea's
update goroutine (never the query goroutine), so the shared cache needs no
locking.

Corollaries:

- **Opaque ids stay out of list columns.** An unresolved/absent id renders a
  **blank** cell, never the raw GUID; a late cache fill re-renders. The detail
  page keeps both the resolved name and the raw id.
- **Known id-less types drop the column** rather than show a permanently blank
  one — a small hardcoded exception set (Azure geography/tenant metadata that
  sits above any subscription). This is the *only* place specific type strings
  are baked in; everything else is prefix-generic.

## Rejected alternatives

- **Per-row DQL `lookup` join** to resolve the name inline. Re-scans the lookup
  table on every browse; unbounded on large enterprise tenants. The cache pays
  once per session instead.
- **A duplicate `azureResourcesSpec`.** ~40 lines duplicated and it leaves the
  mixed (unfiltered) census showing the wrong provider's columns for a row.
- **Resolving in the catalog layer.** Would force session state or a query
  capability into the framework-free catalog, breaking its purity (ADR-0003).

## Consequences

- Adding a cloud provider = one prefix branch + a column set (+ an optional
  id→name cache if the account id is opaque). No per-type screens; new types
  appear automatically via discovery (ADR-0010).
- Enrichment is a generic, reusable **mechanism** (parallels metric
  enrichment): any future opaque-id→name mapping is one more
  `catalog.DerivedFieldResolver` entry — no TUI-shell changes — surfaced through
  a labeled display fact (ADR-0016).
- The tenant-validated field facts behind this (provider field mapping,
  name-rich vs name-poor inventories, the subscriptions-node join source, the
  silent-empty traps) live in [../dev/learnings.md](../dev/learnings.md) §1.9 /
  §1.9b — not duplicated here.
