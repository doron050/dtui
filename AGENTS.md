# AGENTS.md

The entry point for AI coding agents (and humans) working on dtui. It is
short on purpose: it states the rules, then routes you to the one doc or
skill for your task. Read only what your task needs.

## What this is

`dtui` is an interactive, keyboard-driven terminal UI for Dynatrace — a
k9s-style navigator over observability primitives (problems, services, hosts,
Kubernetes, traces, logs, events, RUM, SLOs, Smartscape topology, a Grail data
explorer). It is a bubbletea TUI written in Go 1.26.

It is **interactive-only**: it refuses to launch in a detected AI-agent
environment or without a TTY (`main.go` `RunE`), so you generally cannot run
the binary yourself. Rely on the test suite, which drives the models headlessly.

## Hard constraints

A PR that trips one of these is wrong, not the check.

| Rule | Why | Enforced by |
| --- | --- | --- |
| **Strictly read-only.** Never create, edit, delete or execute anything on a tenant. A user who wants to mutate gets the equivalent `dtctl` command (`c`). | [ADR-0011](docs/adr/0011-strictly-read-only.md) | dtctl import allowlist in `TestTUIHasNoDirectTenantAccess`; review |
| **No HTTP inside `internal/tui`.** API-backed views take injected `Source` closures built in `sources.go` (and `cmd/web/sources.go` for the browser build). | [ADR-0011](docs/adr/0011-strictly-read-only.md) | `TestTUIHasNoDirectTenantAccess` |
| Only the main package and `cmd/web` import `internal/tui`. | layering | `TestOnlyEntryPointsImportTUI` |
| `internal/tui/catalog` is declarative data: no terminal framework, no module-internal imports. | [ADR-0003](docs/adr/0003-declarative-view-catalog.md) | `TestCatalogIsFrameworkFree` |
| `internal/workspace` never reaches a tenant and holds no credentials; dtctl owns configuration. | [ADR-0005](docs/adr/0005-dtctl-owns-configuration.md) | `TestWorkspaceHasNoTenantAccess` |
| **DQL is the substrate.** New views are catalog `Spec`s, not bespoke screens; a bespoke screen needs an ADR. | [ADR-0001](docs/adr/0001-dql-is-the-substrate.md) | review |
| Every feature passes the six vision gates; the anti-goals are the governance gate. Every view has both exits (`c`, `o`) and fits the query budget. | [vision.md](docs/design/vision.md), [ADR-0013](docs/adr/0013-query-budget.md) | `TestEveryViewHasBothExits`, `TestOpeningAViewFitsQueryBudget`; PR template; review |
| Public repo: synthetic identifiers only. Never tenant names, URLs or IDs, hostnames, people, customers or ticket keys — in code, tests, goldens, docs or PRs. | privacy | review |

When you find a rule that can be checked mechanically, add a test to
`invariants_test.go` and a row here in the same PR.

## Route your task

| Task | Do this |
| --- | --- |
| Add or change a view | skill [add-view](.agents/skills/add-view/SKILL.md) |
| Write or change a DQL query | skill [change-query](.agents/skills/change-query/SKILL.md) |
| Fix a bug | skill [fix-bug](.agents/skills/fix-bug/SKILL.md) |
| Review a PR (yours or someone else's) | skill [review-pr](.agents/skills/review-pr/SKILL.md), in a fresh session |
| Infra: CI, lint, release, tooling | `makefile`, `.github/workflows/`, [releasing.md](docs/dev/releasing.md); keep `make fmt lint test` the one local gate |
| Understand how the code fits | [tui.md](docs/design/tui.md) "Architecture" |
| Write or fix tests | [tui.md](docs/design/tui.md) "Testing" |
| A decision that ripples beyond one view | new ADR per [docs/adr/README.md](docs/adr/README.md) |

Skills live in `.agents/skills/<name>/SKILL.md` (`.claude/skills` links
there). If your tool does not load skills, open the file and follow it.

## Reuse before you write

Agents duplicate helpers they did not find. Before writing a helper, check
here and grep for it. If one is close, extend it instead of copying it.
Prefer the cleaner design over the smaller diff. Something two views need
belongs in a shared file; a third copy is a bug.

| Need | Shared home |
| --- | --- |
| Show tenant-supplied text safely | `catalog.SafeText` (`internal/tui/catalog/format.go`); `sanitizeFrame` (`internal/tui/sanitize.go`) is the last guard |
| Severity colors | `internal/tui/catalog/severity.go` |
| Sparklines | `internal/tui/catalog/spark.go` |
| Render typed values (ids, timestamps, durations, JSON) | `internal/tui/render.go` |
| Colors and styles | the `internal/tui/theme` package — no ad-hoc colors |
| Cursor and viewport for a list | `internal/tui/scroller.go` |
| Modal overlays | `internal/tui/overlay.go` |
| Deep links into the web UI | `internal/tui/links.go` |
| Server-side search and facets | `internal/tui/catalog/facets.go` |
| Key facts and preview panes | `internal/tui/catalog/detail.go`, `internal/tui/catalog/preview.go` |
| Timers (tests zero them) | `internal/tui/tick.go` |
| Text inputs | `internal/tui/input.go` |
| API-backed data | a `Source` in `sources.go` |

Add a row when you create a shared helper.

## Commands

```bash
make build          # → bin/dtui (ldflags stamp version + dtctl's pkg/version.Version)
make install        # installs dtui + a `dtctl-tui` symlink so `dtctl tui` dispatches here
make test           # go test -race ./...
make lint           # golangci-lint run ./...  (.golangci.yml — must pass clean)
make fmt            # golangci-lint fmt (gofmt + goimports)
make mutation-diff  # gremlins on lines changed since origin/main (CI gate on PRs)

go test -race ./internal/tui -run TestName                 # one test
go test ./internal/tui -run Snapshot -update               # regenerate layout goldens
```

Run `make fmt lint test` before every push; CI expects it clean.

## Queries: Grail fails silently

A wrong field, id type or data object returns `{"records": []}` with exit 0.
Read [learnings.md](docs/dev/learnings.md) before touching a query, and
confirm every new query returns rows on a live tenant. Details are in the
[change-query](.agents/skills/change-query/SKILL.md) skill.

## When docs and code disagree

The code wins for what the system *does*; fix the doc in the same PR, or log
it in [tech-debt.md](docs/dev/tech-debt.md). ADRs and the vision record
*intent*: if code contradicts one, flag it rather than quietly rewriting the
ADR. A changed decision gets a new ADR that supersedes the old one.

## Write back what you learned

The repository is the memory; there is no other. Before you finish:

| You found | Record it in |
| --- | --- |
| A DQL/Grail fact or trap, validated live | [learnings.md](docs/dev/learnings.md), by tenant *kind*, with the code that relies on it |
| A decision whose reversal would ripple through more than one view | a new ADR in [docs/adr/](docs/adr/README.md) |
| A shortcut, known gap or duplication you left behind | [tech-debt.md](docs/dev/tech-debt.md) |
| A new shared helper or a new hard rule | the tables in this file (and a test for a rule) |

Do not record what `git log` or the code already says. Keep this file under
150 lines (`TestAgentDocsStayAccurate` checks it and every path it names).

## Docs map

- [docs/design/vision.md](docs/design/vision.md) — scope charter, six gates, anti-goals
- [docs/design/tui.md](docs/design/tui.md) — view catalog, navigation, detail pages, architecture, testing
- [docs/dev/learnings.md](docs/dev/learnings.md) — live-validated DQL/Grail facts and patterns
- [docs/adr/](docs/adr/README.md) — decision records; 0001, 0011 and 0012 are load-bearing
- [CONTRIBUTING.md](CONTRIBUTING.md) — PR conventions
