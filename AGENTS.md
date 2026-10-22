# AGENTS.md

Guidance for AI coding agents working in this repository. The full project
guide is [CLAUDE.md](CLAUDE.md); read it first.

## Hard constraints

- **Strictly read-only** ([ADR-0011](docs/adr/0011-strictly-read-only.md)). Never add a path that creates, edits, deletes or executes anything on a tenant.
- **No HTTP inside `internal/tui`.** API-backed views take injected `Source` closures built in `sources.go`. Only the `dtui` main package (and `cmd/web`) may import `internal/tui`. `invariants_test.go` enforces both.
- **DQL is the substrate** ([ADR-0001](docs/adr/0001-dql-is-the-substrate.md)). New views are catalog `Spec`s, not bespoke screens.
- Check [docs/design/vision.md](docs/design/vision.md) before proposing a feature; its anti-goals are the governance gate.

## Before changing queries

Read [docs/dev/learnings.md](docs/dev/learnings.md). Grail fails silently: a wrong field returns an empty result with no error. Confirm a query returns rows before trusting it, and record new findings there by tenant *kind*, never by tenant name, URL or ID.

## Before pushing

```bash
make fmt lint test
```

Tests use synthetic identifiers only. See [CONTRIBUTING.md](CONTRIBUTING.md).
