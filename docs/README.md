# dtui documentation

User-facing docs (install, configuration, the workspace file) live in the
top-level [README](../README.md); press `?` inside the TUI for the key
reference. This directory is for design and contributor documentation.

## Layout

- **[design/](design/)** — what dtui is and how it's built
  - [vision.md](design/vision.md) — the vision & scope charter: who dtui
    is for, the TUI/web/CLI boundary, budgets, the feature gate, anti-goals
  - [tui.md](design/tui.md) — the core design: view catalog, navigation
    model, detail pages, architecture
  - [smartscape-navigator.md](design/smartscape-navigator.md) — the
    topology navigator (`:nav`)
- **[dev/](dev/)** — contributor notes
  - [learnings.md](dev/learnings.md) — field notes: live-validated
    DQL/Grail facts, the view extension model, bubbletea patterns, how to
    verify the TUI
  - [phases.md](dev/phases.md) — the phase-by-phase shipped log
- **[adr/](adr/)** — architecture decision records: the load-bearing
  decisions and why they were made

The shared config/credential contract is dtctl's
[CONFIG_CONTRACT.md](https://github.com/dynatrace-oss/dtctl/blob/main/docs/dev/CONFIG_CONTRACT.md).
