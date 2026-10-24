---
title: "The feature-loop framework: work defined once, approved once, shipped as one rollup PR ([ADR-0074](/architecture/decisions/#adr-0074-an-approved-definition-rolls-up-to-one-pr-slices-cascade-on-an-integration-branch))"
date: 2026-08-01
refs: []
form: bullet
---

- **The feature-loop framework: work defined once, approved once, shipped as one rollup PR
  ([ADR-0074](/architecture/decisions/#adr-0074-an-approved-definition-rolls-up-to-one-pr-slices-cascade-on-an-integration-branch)).**
  The [feature loops](/contributing/feature-loops/) contract page defines how a body of work executes through an
  agent loop with exactly two human touchpoints: the architect approves a prose definition at the front and merges
  one rollup PR at the end, while sub-issue slices cascade through per-slice gates (test-first, the docs lints, an
  adversarial review) on an integration branch between them. Four skills operationalize it: `/define-work` (the
  nine-section approvable shape), `/run-feature-loop` (the executor run-book), `/add-api-route` (the
  middle-of-slice procedure, built from a code study so every named symbol resolves: `gated(...)`, the scoped-CRUD
  primitive, `writeAuditRes` in-transaction, the ADR-0062 addressing rule, the gen ripple), and
  `/adversarial-review` (refute-not-confirm, a concrete failure scenario per finding, sweeping a twelve-entry
  anti-pattern catalog seeded from the 2026-07-30 audit's failure classes). Generate-first is codified as a house
  rule with its `/ship-slice` gate, and the harness gains unattended-run plumbing (a permission allowlist and a
  SessionStart environment check pinned to the gen-drift versions). Built as the
  [AI-driven feature loops epic](https://github.com/hyperscaleav/omniglass/issues/488); the pilot loop
  ([#502](https://github.com/hyperscaleav/omniglass/issues/502)) runs one approved drift batch through the whole
  frame before any feature loop launches.
