# dtui

[![ci](https://github.com/dynatrace-oss/dtui/actions/workflows/ci.yml/badge.svg)](https://github.com/dynatrace-oss/dtui/actions/workflows/ci.yml)
[![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
![Read-only](https://img.shields.io/badge/environment%20access-read--only-success.svg)
![Status: experimental](https://img.shields.io/badge/status-experimental-orange.svg)

**A keyboard-driven terminal UI for Dynatrace.**

dtui is a TUI for Dynatrace SaaS. View all your Services, Kubernetes workloads, traces, logs, SLOs right in your terminal or IDE. It is the perfect companion to [dtctl](https://github.com/dynatrace-oss/dtctl)

Problems, services, Kubernetes, traces, logs, SLOs and topology, a few keystrokes apart. Think [k9s](https://k9scli.io), for observability.

[Quick start](#tldr) ·
[Why](#why-dtui) ·
[Example](#from-symptom-to-cause-in-seven-keys) ·
[Install](#install) ·
[Keys](#keys) ·
[Configuration](#workspace-file-dtuiyaml) ·
[FAQ](#faq) ·
[Docs](docs/README.md)

![dtui demo: pods, quick search, detail lenses, span waterfall](docs/images/dtui-demo.gif)

> **Experimental, and not supported.**
>
> dtui is community project developed by Dynatrace employees and **not an official Dynatrace product**.
>
> - **No support, no SLA.** It is not covered by Dynatrace support.
> - **Provided as is,** without warranty of any kind.
> - **Expect breaking changes.** Interfaces, keys, and the workspace file format
>   may change without notice.
> - **Improvements and bugfixes** handled on a best-effort basis (see [Support](#support))
> - **Contributions are welcome,** handled on a best-effort basis (see
>   [Support](#support)).

## TL;DR

You need [dtctl](https://github.com/dynatrace-oss/dtctl) installed, only to sign in once.

```bash
# 1. Sign in once with dtctl
dtctl ctx set my-environment --environment https://<your-env>.apps.dynatrace.com
dtctl auth login

# 2. Build and install dtui
git clone https://github.com/dynatrace-oss/dtui && cd dtui
make install

# 3. Run it (DO NOT FORGET TO ADD YOUR GO BIN TO YOUR PATH)
dtui
```

Inside: `?` shows every key, `:` jumps to any view, `/` searches, `q` quits.

## Why dtui

Dashboards answer the questions someone thought of in advance. During an
incident you have different ones. dtui lets you follow the evidence by
keyboard, from a symptom to its cause, without writing a single query.

- **Investigate without leaving the terminal or IDE.** Start at a problem, hop to the
  affected service, its pods, their logs, then the failing trace's span
  waterfall. Each step is one key.
- **Scope follows you.** Drill-down keys carry the entity and the timeframe into
  the next view, so you never write a query and never run an unbounded scan.
- **Broad coverage.** Problems, services, hosts, processes, Kubernetes, AWS,
  Azure, GCP, databases, traces, logs with pattern clustering, events, RUM,
  security, SLOs with live evaluation, a Grail data explorer, and a Smartscape
  topology navigator.
- **DQL underneath, not in your way.** Every view is a curated DQL query.
  `ctrl+q` shows the generated query when curation runs out.
- **Strictly read-only.** It never creates, edits, deletes, or executes anything
  on your environment, by design ([ADR-0011](docs/adr/0011-strictly-read-only.md)).
  To act on what you found, `c` copies the equivalent
  [dtctl](https://github.com/dynatrace-oss/dtctl) command.
- **Zero new auth.** It reuses your dtctl contexts and credentials, read-only.
  No tokens to paste, nothing new to secure.

## From symptom to cause in seven keys

```text
1        problems list
enter    the critical problem: root cause, impact, timeline
l        logs, already scoped to the problem and timeframe, grouped by pattern
enter    the top error pattern
s        jump to the trace behind that log line, shown as a span waterfall
x        related entities: the failing downstream service
v        events: a deployment landed 4 minutes before the problem started
```

More flows (Kubernetes crashloop, slow frontend) are in the
[design doc](docs/design/tui.md#example-journeys).

## What you can browse

| Area | Views |
|---|---|
| **Triage** | Problems, home overview, events, costs |
| **Services and apps** | Services, databases |
| **Infrastructure** | Hosts, processes, containers and extensions |
| **Clouds** | AWS, Azure, GCP |
| **Kubernetes** | Clusters, nodes, namespaces, workloads, pods |
| **Distributed Traces** | Traces with span waterfall and logs in context |
| **Logs** | Logs with pattern clustering |
| **Metrics** | Metrics with charts |
| **Real User Monitoring** | RUM Applications, Sessions and user events |
| **Security** | Vulnerabilities, runtime attacks |
| **Reliability** | SLOs with live evaluation, detectors, workflows |
| **Explore** | Smartscape topology navigator, Grail data explorer, DQL query view |
| **Fleet** | OneAgent fleet and logs |

Press `:` in the app to see every view and its aliases.

## How it works

```text
  you ──keys──▶  dtui  ──DQL──▶  Grail (your environment)
                    │
                    └── reads ~/.config/dtctl/config + keyring (never writes)
```

Every view is a curated DQL query. Drill-down keys compose entity and timeframe
into the next query for you. dtui consumes dtctl's Go libraries for config,
auth, and DQL execution, so there is no forked business logic and no second
credential store. Details in [docs/design/tui.md](docs/design/tui.md) and the
[ADRs](docs/adr/README.md).

## Keys

| Key | Does |
|---|---|
| `:` | Command bar: jump to any view (`:pods`, `:svc`, `:logs error`, `:trace <id>`, `:ctx prod`) |
| `/` | Quick search in the current view |
| `enter` | Open the detail page |
| `l` `s` `m` `p` `v` `u` | Logs, traces, metrics, problems, events, SLOs, pre-scoped to the selection |
| `x` | Related entities (topology hop) |
| `d` / `o` | Describe as YAML / open in the browser |
| `c` | Copy the equivalent `dtctl` command |
| `S` | Pick Grail filter segments |
| `ctrl+q` | Show the generated DQL |
| `?` | Full key reference |

## Install

### 1. Prerequisites

- A Dynatrace environment (Grail-enabled) you can sign in to.
- **[dtctl](https://github.com/dynatrace-oss/dtctl) installed locally.** dtui
  uses dtctl's Go libraries and reads its config and credential store. It never
  runs the `dtctl` binary, so you need dtctl only for the one-time sign-in below.
  Tested against dtctl **v0.43.0**; keep yours reasonably close.
- Go 1.26 or newer, only if you build from source.

### 2. Sign in with dtctl (once)

You need just two dtctl commands. Skip the rest of dtctl's docs.

```bash
dtctl ctx set <name> --environment <environment-url>   # create a context
dtctl auth login                                        # sign in
```

### 3. Install dtui

Pick one.

**Homebrew** (macOS and Linux):

```bash
brew install dynatrace-oss/tap/dtui
```

**Build from source** (a clone of this repository):

```bash
git clone https://github.com/dynatrace-oss/dtui
cd dtui
make install   # installs `dtui` and the optional `dtctl-tui` alias
```

`make install` places the binary in `$GOBIN` (or `$GOPATH/bin`, typically
`~/go/bin`) and links `dtctl-tui` next to it, so `dtctl tui` also works through
dtctl's exec-plugin dispatch. To only build, run `make build`, which produces
`bin/dtui`.

**With Go:**

```bash
go install github.com/dynatrace-oss/dtui@latest
```

**Prebuilt binary:** download one for Linux, macOS, or Windows (amd64 and arm64)
from the [releases page](https://github.com/dynatrace-oss/dtui/releases),
unpack it, and put `dtui` on your `PATH`.

Release checksums are signed with [cosign](https://github.com/sigstore/cosign)
and the archives carry build-provenance attestations. To verify a download:

```bash
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/dynatrace-oss/dtui/\.github/workflows/release\.yml@' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
sha256sum --ignore-missing -c checksums.txt   # shasum -a 256 on macOS
gh attestation verify <archive> --repo dynatrace-oss/dtui
```

<details>
<summary><b><code>dtui</code>: command not found</b></summary>

`~/go/bin` is probably not on your `PATH`. Add it to your shell profile and reload:

```bash
# bash (~/.bashrc or ~/.bash_profile) and zsh (~/.zshrc)
export PATH="$PATH:$(go env GOPATH)/bin"

# fish (~/.config/fish/config.fish)
fish_add_path (go env GOPATH)/bin
```

</details>

## Usage

```bash
dtui                  # home triage view on the current context
dtui pods             # jump straight into a view
dtui --context prod   # session-local override, never persisted
```

Press `?` inside the TUI for the full key reference, `:` to browse views, and
`q` to quit. dtui refuses to start without a terminal (or inside an AI-agent
environment): it is interactive only. It keeps its own UI state (navigation
history) under `~/.local/state/dtui/`.

Contexts and credentials are managed with dtctl; a `--context` flag or
`DTCTL_CONTEXT` applies to the running session only.

## Workspace file (`.dtui.yaml`)

A `.dtui.yaml` gives dtui session-local defaults: which view to open,
the timeframe, the environment, extra tag columns and filter segments. dtui
finds it by walking up from the current directory, like `.git`, so you can
commit one to a project (everyone who runs `dtui` inside it gets the same
setup) or put one in `~` as a personal default. **Every key is optional, and
no file at all is fine.**

`.dtui.yaml` is not dtctl's `.dtctl.yaml`: it never defines a context, URL
or credentials. It only selects an existing dtctl context (by `environment`)
and narrows what the session shows. Keys use dtctl's kebab-case style.

### Minimal example

This works on any environment — it only picks the start screen and the timeframe:

```yaml
version: 1
view: home
timeframe: 2h
```

<details>
<summary><b>All parameters</b></summary>

```yaml
# .dtui.yaml
version: 1

# environment — pick the dtctl context whose environment URL matches.
# An https:// URL or a bare hostname; trailing slash and case are ignored.
# --context / DTCTL_CONTEXT win over it. No match = a startup warning and
# your current dtctl context stays active.
environment: https://abc12345.apps.dynatrace.com

# view — the screen dtui opens on: `home`, `query`, `nav`, or any catalog
# view (pods, services, logs, problems, slos, ...; press `:` in the TUI to
# browse them all). A CLI argument (`dtui logs`) wins; an unknown name
# warns and falls back to `home`. Default: home.
view: pods

# timeframe — default window: <n>m, <n>h or <n>d (30m, 2h, 7d).
timeframe: 2h

# primary-tags — Grail primary_tags.* keys shown as extra columns on every
# Smartscape and signal view (max 5, bare key without the `primary_tags.`
# prefix). A key with no values in the current view shows an empty column.
primary-tags:
  - team
  - env

# segments — Grail filter segments applied to every DQL-backed view
# (max 10, AND-combined). Each entry is a bare name/UID or a map.
segments:
  - payments-prod                  # by name (exact, case-insensitive)
  - segment: Kx9fPq2LmNa           # by UID
    variables:                     # values for segments that define variables
      environment: [production]    # list of values per variable name
    primary-tags:                   # extra columns, active only with this segment
      - payment-region
```

| Key | Type | Description |
|---|---|---|
| `version` | int | Schema version. `1` (or absent) is current; a higher number than your dtui supports is an error. |
| `environment` | string | Environment URL or bare hostname, used to select the matching dtctl context. Must not contain whitespace or credentials. |
| `view` | string | Start view (see above). The CLI argument wins. |
| `timeframe` | string | Default timeframe, `<n>m`, `<n>h` or `<n>d`. |
| `primary-tags` | list of strings | Up to 5 tag keys added as columns on every view. Duplicates are dropped; empty keys are an error. |
| `segments` | list | Up to 10 filter segments applied to every DQL-backed view. |
| `segments[].segment` | string | Segment name or UID. Required in the map form (the bare-string form sets only this). Unknown or ambiguous names warn instead of guessing. |
| `segments[].variables` | map of lists | Pre-bound values for the segment's variables; each variable needs at least one value. |
| `segments[].primary-tags` | list of strings | Extra tag columns for this segment, added to the global `primary-tags` (up to 5 per list). |

</details>

Unknown keys are ignored, so a newer committed file still works on an older
dtui. Values are used verbatim — there is no environment-variable expansion,
so a committed file behaves the same on every machine.

Guarantees: the file carries **no credentials, contexts, or executable keys**
by construction — it can only narrow what a session shows, never change what
it can do. It merges on top of your personal dtctl config (unlike a local
`.dtctl.yaml`, which replaces it), is applied session-locally, and a broken
file degrades to a startup warning — never a failed launch.

In the TUI, `S` opens the segment picker to change or clear the selection at
any time, and `alt+s` toggles the applied set off and back on in place
(selection and variable bindings kept) — handy for comparing the workspace's
scoped view against the whole environment. Segments with variables prompt for their
values in the picker (`v` edits bindings later), so `variables:` is a team
default, not a requirement.

## Chrome extension (experimental)

[`extension/`](extension/) is a Chrome side-panel extension that runs the same
TUI in the browser (WebAssembly) with deep-link rules for your own apps. It is
experimental and not published to any store: build the wasm with
`wasm/build.sh` and load `extension/` manually via
`chrome://extensions` → **Load unpacked**. See
[docs/design/WASM_CHROME_EXTENSION.md](docs/design/WASM_CHROME_EXTENSION.md).

## FAQ

**Do I need dtctl installed to run dtui?**
Only once, to create a context and sign in (`dtctl ctx set`, `dtctl auth login`).
dtui never runs the `dtctl` binary.

**Can it change anything on my environment?**
No. It is read-only by design. Press `c` to copy the equivalent `dtctl` command
and run mutations there.

**Do I need to know DQL?**
No. Views are curated and drill-downs scope themselves. `ctrl+q` shows the DQL
if you want to learn it or take it elsewhere.

**Why does it refuse to start in my script or CI job?**
dtui is interactive only. It exits without a TTY or inside an AI-agent
environment.

**A view is empty. Is it broken?**
Probably not: the timeframe may have no data, or a segment may be filtering it
out. Try `alt+s` to toggle segments off, or widen the timeframe.

## Contributing

Issues and pull requests are welcome. dtui has a deliberately narrow scope,
so read [CONTRIBUTING.md](CONTRIBUTING.md) and open an issue before writing
anything non-trivial. Tests need no environment, network, or TTY:

```bash
make fmt lint test
```

## Support

dtui is maintained on a best-effort basis: no SLA, no guaranteed response
time, and no commitment to specific features or releases. For a bug or a
feature idea, open an issue (please include no real environment data); for a code
change, read [CONTRIBUTING.md](CONTRIBUTING.md) first. Report security
problems privately as described in [SECURITY.md](SECURITY.md).

## Documentation

See [docs/](docs/README.md) for the full index:

- [docs/design/tui.md](docs/design/tui.md) — design: view catalog, navigation
  model, investigation flows, detail pages, architecture
- [docs/dev/learnings.md](docs/dev/learnings.md) — field notes and
  validated DQL/Grail facts
- [docs/adr/](docs/adr/README.md) — architecture decision records

## Acknowledgements

Built on [dtctl](https://github.com/dynatrace-oss/dtctl) and the
[Charm](https://charm.sh) stack (bubbletea, lipgloss, bubbles, glamour).
Inspired by [k9s](https://k9scli.io).

## License

[Apache-2.0](LICENSE)
