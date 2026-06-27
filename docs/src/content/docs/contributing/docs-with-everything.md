---
title: Docs with everything
description: A feature is not done until the docs that teach it ship in the same PR.
---

Omniglass ships its documentation *as part of the product*. The docs are not an
afterthought in a separate wiki; they are Astro Starlight content under `docs/`, compiled
to a static site and published at docs.omniglass.hyperscaleav.com (and, in time, embedded
into the Go binary to serve at `/docs`). The architecture is
published ahead of the code, so the design is visible (and reviewable) before, or
alongside, the feature that implements it.

## The rule

**A feature is not done until the docs that teach it ship in the same PR.**

Concretely, a user-facing PR must do one of:

- change `docs/` to add or update the page(s) that explain the new behavior, or
- carry the `no-docs` label with a one-line justification (pure refactor, internal-only
  change, etc.).

CI enforces the docs-touched gate. The justification path exists so the gate never blocks
a genuine internal change, not as a routine escape hatch.

## What "the docs" means here

- **Architecture pages** (`/architecture/`) hold the model: the spine plus leaf
  documents, and the current decisions. Each official term is defined once in the
  [glossary](/architecture/glossary/) and not redefined in the leaves.
- **Concept and learning pages** teach a concept interactively (see
  [the learning-tool restriction](/contributing/learning-tool/)). When a feature introduces a concept
  an operator must understand, the teaching surface ships with it.
- **Contributor pages** (`/contributing/`) are this doctrine set.

## Style

- No em dashes. Use commas, colons, periods, or parentheses.
- No AI/assistant attribution.
- Write for someone learning the system, not someone who already built it. The same page
  serves the operator using the product and the contributor extending it.

## Publishing

Docs build in CI on every PR (so a broken docs build fails the PR) and are embedded into
the binary at release. The published site is docs.omniglass.hyperscaleav.com.
