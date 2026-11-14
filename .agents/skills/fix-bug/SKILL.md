---
name: fix-bug
description: Fix a dtui bug test-first and at the right layer, then record the cause. Use for any reported defect, wrong rendering, missing rows, navigation or stale-data issue.
---

# Fix a bug

1. **Reproduce headlessly first.** Write the failing test before the fix:
   a model test (synthetic `tea.Msg` keys against the real app), a catalog
   test (the rendered DQL), or a source test (stub the injected `Source` or
   the `runFn` seam). See `docs/design/tui.md` "Testing". If the bug only shows on a
   live tenant, find the record shape that triggers it and seed it as
   synthetic rows.
2. **Check the known bug classes** in `docs/dev/learnings.md` before
   guessing:
   - empty view → a silent Grail failure (wrong field or id type); use the
     [change-query](../change-query/SKILL.md) skill;
   - stale or overwritten data → a missing `(owner, seq)` guard (section 3);
   - wrong scope in the breadcrumb → a view claiming a scope it did not
     apply (section 3, ADR-0007);
   - garbled screen → tenant text not passed through `SafeText`.
3. **Fix the cause where it lives.** If the bug is in a shared helper, fix it
   there and check every caller (`rg -n '<helper>\('`). If the same buggy
   pattern was copied elsewhere, fix every copy or list the rest in
   `docs/dev/tech-debt.md`. No unrelated refactors in a fix PR.
4. **Make it stay fixed.** The test from step 1 must fail without the fix.
   If the bug belongs to a class a test can catch everywhere (an import, a
   forbidden call), add it to `invariants_test.go`.
5. **Write back.** A new Grail trap goes into `docs/dev/learnings.md`; a doc
   that described the buggy behaviour gets corrected. Run
   `make fmt lint test`; title the PR `fix(<area>): …`.
