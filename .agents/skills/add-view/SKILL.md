---
name: add-view
description: Add or change a dtui view (a catalog Spec, its drills, columns and tests). Use for any new table/detail view or a change to an existing view's query, columns or navigation.
---

# Add or change a view

A view is data: a `catalog.Spec` literal that the generic `tableView` engine
renders. Write a bespoke screen only with an ADR (ADR-0001, ADR-0003).

1. **Gate it.** Check the six gates and the anti-goals in
   `docs/design/vision.md`. If the view fails one, stop and say which; do not
   build it. Non-trivial views need an issue first (`CONTRIBUTING.md`).
2. **Copy the nearest neighbour.** Find the closest existing `Spec` in
   `internal/tui/catalog/` (`rg -n 'Name: +"' internal/tui/catalog`) and mirror
   its shape. The field reference and the design rules (scope honesty,
   `UsesScope`, `EnrichSpec`, drill targets) are in `docs/dev/learnings.md`
   section 2 — read it before the first view you write.
3. **Write the query** with the [change-query](../change-query/SKILL.md)
   skill. It must compose `Scope`, fit the query budget (ADR-0013) and be
   validated live to return rows.
4. **Data that is not DQL** (REST lists, analyzers) comes from a `Source`
   wired in `sources.go` (and `cmd/web/sources.go` for the browser build).
   Never import HTTP or dtctl resource handlers into `internal/tui`.
5. **Reuse the shared helpers** listed in AGENTS.md "Reuse before you write":
   `SafeText` for tenant text, `severity.go`, `spark.go`, `render.go`, the
   `theme` package. A helper another view could use goes in a shared file.
6. **Register** the spec in the `specs` slice in
   `internal/tui/catalog/catalog.go` (`All`/`Lookup` read it), and wire the
   drills and `EnterTarget` it needs.
7. **Test** per `docs/design/tui.md` "Testing":
   - a catalog test that renders the query against fixture scopes and pins
     its DQL shape with substring checks;
   - a model test driving the real app with synthetic keys;
   - a layout snapshot (`<view>_snapshot_test.go`) only if this is a main
     screen; seed realistic synthetic rows and call `pinSnapshotClock(t)`.
8. **Update what the user sees in docs:** the README "What you can browse"
   and "Keys" sections, the `?` help in `internal/tui/help.go` for new keys,
   and the view catalog in `docs/design/tui.md`.
9. **Write back** new Grail facts to `docs/dev/learnings.md` (AGENTS.md
   "Write back what you learned"), then run `make fmt lint test`.
