# ADR-0017: A configurable home layout is scope, not customization

**Status:** Accepted · **Date:** 2026-11-14

## Context

The home view is the first screen of every session. Projects want it to
land on the signals they own — a frontend team on frontend errors, a
platform team on Kubernetes warnings — over a lookback that fits them
([#27](https://github.com/dynatrace-oss/dtui/issues/27)). The vision's
gates rule out a customization surface (no skins, no plugins, no
keybinding remapping) and require zero configuration on any tenant, so a
configurable home has to pass as *scope*: narrowing what a session shows,
the way every other `.dtui.yaml` key does.

## Decision

- **Ids, not queries.** `home.panels` lists ids from a curated panel
  catalog (`catalog.HomePanel`, `internal/tui/catalog/home.go`). Each
  entry may set one thing: `timeframe` — a pinned `<n>m|h|d` window, or
  `global` to follow `t`. No DQL, no titles, no placement or styling keys;
  order alone decides placement (panels alternate columns) and `tab`
  order. The catalog stays small enough that every panel fits the query
  budget ([ADR-0013](0013-query-budget.md)): at most six panels, one query
  each.
- **`HomePanel`, not `Spec`.** A panel is a top-N summary with one jump
  into a full view; a `Spec` brings columns, facets, relations and detail
  tabs a panel cannot use. Both are declarative data in `catalog`, so
  [ADR-0003](0003-declarative-view-catalog.md) holds.
- **Strict inside, lenient outside.** Unknown keys inside `home` are
  errors (a typo would otherwise show a layout nobody wrote); unknown
  top-level keys stay warnings for forward compatibility. Any finding
  rejects the whole layout: the default home renders under a persistent
  banner, and the launch never fails. `dtui workspace validate` lists
  every finding offline, with its line.
- **Windows are honest** ([ADR-0007](0007-scope-honesty.md)). Every panel
  title names its window. One the workspace pinned away from the header's
  wears a `◷ 7d` pill and the header counts those; a curated default
  window stays dim, as it was before, so the pill keeps meaning "someone
  configured this". `t` names the panels that do not follow it, and a
  drill-down carries the panel's window, widened to the global one when
  that is longer.
- **Windows have no ceiling** beyond what parses: a pinned panel window
  follows the same rule as the top-level `timeframe`, and one too large
  for a duration is a validation error. A long pinned window is visible
  in its pill and in `ctrl+q`; capping it would be a query-budget
  decision for [ADR-0013](0013-query-budget.md), not a layout rule.

## Consequences

- Zero config is unchanged: no `home` block means the six curated panels,
  rendered as before (no pills, no header counter). A configured list
  whose ids all fail to resolve also falls back to them.
- New panels are catalog entries reviewed like views; a user cannot add
  one from the workspace file.
- Requests for per-panel presentation (columns, row counts, titles,
  colors) are customization and default to no. Per-panel segments or a
  user-global layout file would be new scope and need their own decision.
