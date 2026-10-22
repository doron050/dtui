# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`dtui` is an interactive, keyboard-driven terminal UI for Dynatrace — a
k9s-style navigator over observability primitives (problems, services, hosts,
Kubernetes, traces, logs, events, RUM, SLOs, Smartscape topology, a Grail data
explorer). It is a bubbletea TUI written in Go.

Two hard, load-bearing constraints define the project:

- **Strictly read-only** (ADR-0011). dtui never creates, edits, deletes, or
  executes anything on a tenant — under any safety level, by design, not just
  "for now". When the user wants to mutate, `c` copies the equivalent `dtctl`
  command; mutations live in the CLI. Do not add write paths.
- **Interactive-only.** It refuses to launch in a detected AI-agent
  environment or when stdout is not a TTY (`main.go` `RunE`). You generally
  **cannot run the binary yourself** to observe it — rely on the test suite,
  which drives the models headlessly.

## Commands

```bash
make build          # → bin/dtui (ldflags stamp version + dtctl's pkg/version.Version)
make install        # installs dtui + a `dtctl-tui` symlink so `dtctl tui` dispatches here
make test           # go test -race ./...
make test-coverage  # adds -coverprofile and prints total
make vet            # go vet ./...
make lint           # golangci-lint run ./...  (.golangci.yml — full ruleset, must pass clean)
make fmt            # golangci-lint fmt (gofmt + goimports, config in .golangci.yml)

# Run a single test / package
go test -race ./internal/tui -run TestName
go test -race ./internal/tui/catalog -run TestName/subtest
```

Go 1.26. After changing code, run `make fmt lint test` — the standalone repo
runs the full golangci ruleset and CI expects it clean.

## Relationship to dtctl

dtui is its own module (`github.com/dynatrace-oss/dtui`) and a pure
**consumer** of the published `dtctl` modules (ADR-0004, ADR-0005):

- It reads dtctl's config (`~/.config/dtctl/config`) and keyring **without ever
  writing them**. Contexts are created/managed with `dtctl ctx set`. A
  `--context` flag or `DTCTL_CONTEXT` env override is applied in memory only
  (session-local) — an open TUI must never repoint other dtctl processes.
- It imports dtctl's `sdk/session` (config, credentials, client), `pkg/exec`
  (DQL executor), `pkg/resources/*` (SLO/detector/analyzer/segment handlers),
  and `pkg/output` renderers. **No business logic is forked** — the TUI is a
  presentation layer.
- The charmbracelet/bubbletea dependencies live in this module only; dtctl's
  root module and SDK stay TUI-free. The dtctl module versions are pinned
  to released tags in `go.mod` (plain `require`, no `replace`).

`main.go` and `sources.go` are the wiring seam: they construct the tenant
client and adapt dtctl resource handlers into `tui.Source` closures, so that
**`internal/tui` never does HTTP itself**. Follow this — new API-backed views
get a source constructor in `sources.go`, tested with an interface stub.

## Architecture

The single most important design decision: **DQL is the substrate, not the
interface** (ADR-0001). Every view is a curated DQL query under the hood; every
drill-down key composes scope (entity + timeframe) into the next query
automatically, so the user never types DQL. `ctrl+q` reveals the generated
query when curation runs out.

### The declarative view catalog (`internal/tui/catalog/`)

This is where you add or change views, and it's the part that most needs
reading before editing. Views are **data, not bespoke screens** (ADR-0003): a
`Spec` bundles a scope-aware DQL template, curated columns, metric-column
enrichment, drill-down targets, relations, and a tabbed detail spec. Adding a
cloud resource type or signal view = adding a `Spec`, not writing a screen.

The package is deliberately **free of any TUI-framework dependency** so query
composition tests as plain data. `catalog.go`'s package doc lists the
per-entity-type dispatch points (KeyFacts, MetricsFor, smartscapeField,
SpanScopable, VulnEntityFilter, …) to walk when curating a new Smartscape type
— they are separate switches on purpose because each encodes independently
live-validated semantics. Files are organized one-per-domain (`k8s`, `spans`,
`security`, `metrics`, `genai`, `cloud`, `synthetic`, …).

`Scope` (entity + `Timeframe`) is threaded through every query template.
`Timeframe.DQL()` handles both relative (`now() - 2h`) and absolute
(`toTimestamp(...)`) windows through the same `from:` slot.

### The TUI shell (`internal/tui/`)

Elm-style bubbletea: `app.go` is the root `tea.Model` holding the view stack,
command bar, routing, and global keys. `table.go` / `detail.go` are generic
engines driven by a `Spec`; `problem.go`, `vulnerability.go`, `waterfall.go`,
`timeline.go`, `navigator.go` are bespoke pages. `datasource.go` turns
`pkg/exec` and resource calls into `tea.Cmd`/`tea.Msg` with `context.Context`
cancellation tied to view lifetime. `theme/` is a lipgloss adapter over
dtctl's `ColorEnabled()`.

**Layering rule:** nothing imports `internal/tui` except the `dtui` main
package; no HTTP inside `internal/tui`; every API call is a `tea.Cmd` goroutine.

### Runtime discovery

Shapes are hardcoded only as *defaults*; the rest is discovered per environment
and cached per session (refreshed on context switch): node-type census, edge
catalog, field census, metric catalog, and the semantic dictionary (ADR-0010).
This keeps the catalog small — curated defaults where curation adds value,
introspection everywhere else.

### Workspace file (`internal/workspace/`)

A committed `.dtui.yaml` (found by walking up like `.git`) supplies
session-local defaults: matching context by environment URL, initial view,
timeframe, and Grail filter segments. It carries **no credentials or executable
keys by construction** — it can only narrow what a session shows. A broken file
degrades to a startup warning, never a failed launch.

## Working with DQL / Grail (critical)

`docs/dev/learnings.md` records DQL facts validated against a live tenant — read
it before touching queries. The trap that costs the most time: **Grail fails
silently.** A wrong field, id type, or data-object name returns
`{"records": []}` with exit 0 — no error. Always confirm a query returns
non-empty before trusting a field name. Broad unscoped signal queries hit
Grail's scan limit, which is why signals are always entered through an entity or
problem that supplies scope.

**Update `docs/dev/learnings.md` when you validate a new DQL/Grail fact, tenant
behavior, or dictionary/schema detail** — a confirmed finding, a silent-failure
trap, or a source-of-truth you had to go discover. Add it as a subsection in the
style of the existing ones: tersely stated, attributed by tenant *kind* (for
example "OneAgent-fed tenant", "demo tenant A"), and pointing at the code that
relies on it. The file is public, so it is the controlled memory that keeps the
next person (or agent) from re-deriving a fact — and it must stay free of
internal information: never write tenant names or short codes, environment
URLs or IDs, entity/segment/app IDs, hostnames or IPs, people, customer names,
internal repo URLs, or ticket keys. Use synthetic placeholders.

## Testing conventions

No HTTP and no TTY anywhere in tests (see `docs/design/tui.md` "Testing").

- **Model tests** (`app_test.go` and friends): drive the real app with
  synthetic `tea.Msg` key events and assert navigation, scope composition, and
  read-only behavior. Timers run at zero delay — no test ever sleeps.
- **Catalog tests**: render each `Spec`'s query against fixture scopes and pin
  the DQL shape with substring checks (catches scope-composition regressions).
- **View assertions**: check `View(w,h)` output via `ansi.Strip` + substring
  matching — **no golden files**, so layout tweaks don't churn tests.
- **Datasource tests**: replace the executor via the `runFn` seam; API-backed
  views stub the injected `Source` closures; rows via `seedRows`.
- Use **synthetic identifiers only** — never real tenant/entity IDs, per the
  privacy rule in `docs/dev/learnings.md`.

## Docs map

- `docs/design/tui.md` — the design doc: view catalog, navigation model, detail
  pages, full architecture.
- `docs/dev/learnings.md` — tenant-validated DQL/Grail facts and traps.
- `docs/adr/` — decision records; the read-only (0011), navigate-vs-analyze-vs-
  mutate (0012), and DQL-substrate (0001) ADRs are load-bearing.
