# Contributing to dtui

Thanks for your interest. dtui is an **experimental, unofficial** project:
it is not an officially supported Dynatrace product, and maintenance is
best-effort with no SLA. Issues and pull requests are welcome, but response
times are not guaranteed.

## Quick start

```bash
git clone https://github.com/dynatrace-oss/dtui
cd dtui
make build          # → bin/dtui
make fmt lint test  # run before every push; CI expects it clean
```

You need Go (version in `go.mod`) and `golangci-lint` for `make lint`. Tests
need no environment, no network and no TTY.

## Before you write code

dtui has a deliberately narrow scope. Read
[docs/design/vision.md](docs/design/vision.md) first. Every new feature must
pass all six gates:

1. It shows what exists or what is happening — it does not change it or report on it.
2. It is read-only ([ADR-0011](docs/adr/0011-strictly-read-only.md)). **Pull requests that add write paths will be declined.**
3. It fits the query budget ([ADR-0013](docs/adr/0013-query-budget.md)).
4. It is catalog data — a `Spec` in `internal/tui/catalog/`, not a bespoke screen ([ADR-0003](docs/adr/0003-declarative-view-catalog.md)).
5. At its depth cap it hands over (deep link or command echo) instead of growing ([ADR-0014](docs/adr/0014-breadth-not-depth.md)).
6. It works with zero configuration on an arbitrary environment ([ADR-0010](docs/adr/0010-curated-defaults-runtime-discovery.md)).

For anything non-trivial, **open an issue first** so the approach can be agreed
before you invest time. Overriding a gate requires an ADR, not a PR comment.

## Code conventions

- Architecture and layering: see [CLAUDE.md](CLAUDE.md) and [docs/design/tui.md](docs/design/tui.md).
  Nothing imports `internal/tui` except the main package (and `cmd/web`); no HTTP inside `internal/tui`.
- DQL: **Grail fails silently** — a wrong field returns an empty result with no
  error. Validate new queries against a live environment and confirm they return rows.
- Tests: no HTTP, no TTY, no sleeps, no golden files (see CLAUDE.md "Testing conventions").
- **Use synthetic identifiers only** in tests, fixtures and docs — never real
  environment names, URLs, entity IDs or tokens.

## Pull requests

- Keep PRs focused: one logical change.
- The PR title must be a [conventional commit](https://www.conventionalcommits.org/)
  (`feat:`, `fix:`, `perf:`, `refactor:`, `docs:`, `test:`, `build:`, `ci:`, `chore:`,
  `revert:`; optional scope, `!` for breaking changes). PRs are squash-merged and the
  title becomes the commit on `main`, which release-please uses to pick the next
  version. The `pr-title` check enforces this.
- `make fmt lint test` must pass.
- Fill in the PR template, including the gate checklist.

## Releases

Maintainers cut releases with the manually dispatched `release` workflow; see
[docs/dev/releasing.md](docs/dev/releasing.md).

## Reporting security issues

Do not open a public issue. See [SECURITY.md](SECURITY.md).

## License

By contributing you agree that your contributions are licensed under the
[Apache License 2.0](LICENSE).
