---
name: Feature proposal
about: Propose a new view, drill-down or improvement
labels: enhancement
---

dtui is read-only and deliberately narrow — see
[vision.md](../docs/design/vision.md). Please check the gates below.

**The question this answers** (what is the user trying to find out, in a terminal?)

**Proposed behaviour**

**Vision gates**

- [ ] Shows what exists or is happening — does not change it or report on it
- [ ] Read-only
- [ ] Fits the query budget (one scoped query per navigation action)
- [ ] Expressible as catalog data (a `Spec`)
- [ ] Hands over to the web UI or `dtctl` at its depth cap
- [ ] Works with zero configuration on an arbitrary tenant
