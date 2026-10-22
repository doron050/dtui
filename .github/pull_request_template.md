## What and why

<!-- One or two sentences. Link the issue/discussion for anything non-trivial. -->

Closes #

## Vision gates ([vision.md](../docs/design/vision.md))

- [ ] Shows what exists or is happening — does not change or report on it
- [ ] Read-only ([ADR-0011](../docs/adr/0011-strictly-read-only.md)); no write paths added
- [ ] Fits the query budget ([ADR-0013](../docs/adr/0013-query-budget.md))
- [ ] Catalog data (a `Spec`), not a bespoke screen — or an ADR is included
- [ ] Hands over (deep link / command echo) at its depth cap
- [ ] Works with zero configuration on an arbitrary tenant

## Checks

- [ ] `make fmt lint test` passes
- [ ] New DQL was validated against a live tenant and returns rows
- [ ] Tests use synthetic identifiers only (no real tenant names, URLs, or IDs)
