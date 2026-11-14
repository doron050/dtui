---
name: change-query
description: Write, change or debug a DQL/Grail query in dtui and record what was learned. Use whenever a Spec's Query, an enrichment, a facet, or any DQL string changes, or a view returns no rows.
---

# Write or change a DQL query

**Grail fails silently.** A wrong field, id type or data-object name returns
`{"records": []}` with exit 0. An empty result proves nothing; only rows do.

1. **Look it up first.** Search `docs/dev/learnings.md` for the data object
   and every field you use (`rg -n '<field>' docs/dev/learnings.md`). Most
   traps are already recorded there: uid vs string compares (`toUid`),
   `toSmartscapeId`, bucket tables, scan limits, field renames.
2. **Compose scope, don't hard-code it.** Build the query from `Scope`
   (`Entity`, `Timeframe`, `Arg`, `TraceID`). Signals are always entered
   through an entity or problem; a broad unscoped signal query hits the scan
   limit. A view must never claim a scope its query ignores (ADR-0007).
3. **Stay inside the query budget** (ADR-0013): batch enrichment into one
   query per refresh (`EnrichSpec`), cap lists server-side, prefer
   `summarize` over pulling raw records.
4. **Validate live** with a read-only context and confirm rows come back:

   ```bash
   dtctl query '<dql>'          # must return records, not []
   ```

   Then check a near-miss (a misspelled field) also returns `[]` — that
   shows your rows came from the field you meant. If you cannot reach a
   tenant, say so in the PR and leave the "validated against a live tenant"
   box unticked.
5. **Pin the shape** in a catalog test: render the query for the scopes it
   supports and assert substrings, so scope composition cannot regress.
6. **Record the fact** in `docs/dev/learnings.md`: a short subsection,
   attributed by tenant *kind* ("OneAgent-fed tenant", "demo tenant A"),
   pointing at the code that relies on it. Never write tenant names or short
   codes, URLs, entity/segment/app IDs, hostnames, IPs, people, customers or
   ticket keys. Correct an entry the code now contradicts.
