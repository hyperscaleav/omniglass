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
- **Operator guides** (`/guides/`) are how-to pages for someone *operating* the product:
  the steps and the mental model for using a surface (the CLI, and now the console). **A
  slice that ships or changes an operator-facing surface ships or updates its operator
  guide in the same PR**, not just the architecture page. The architecture page says how
  the surface is built; the guide says how to use it.
- **Concept and learning pages** teach a concept interactively (see
  [the learning-tool restriction](/contributing/learning-tool/)). When a feature introduces a concept
  an operator must understand, the teaching surface ships with it.
- **Contributor pages** (`/contributing/`) are this doctrine set.

So a feature that adds an operator surface usually touches two homes: the **architecture**
page (the model) and a **guide** (the how-to). A purely internal change touches neither and
takes the `no-docs` label.

## Status moves with the code

The architecture pages are written in the present tense as the **target design**, so build status is
carried *alongside* the prose, not woven into it, and keeping it current is part of docs-with-everything.
A slice that advances a page updates three surfaces in the **same PR**:

- the page's **status badge** moves to its new floor (`Design` to `Partial` to `Built`), which the live
  grid on [implementation status](/architecture/status/) reads directly, so the grid never lies;
- the **build-progress note** on `status.mdx` gains the slice's entry; and
- if the shipped code **diverges** from a page's design, the page carries an inline note and a
  [decision-log](/architecture/decisions/) entry (an ADR) lands in the same PR.

Forward-looking intent that is not yet a slice lives in a GitHub epic and is indexed on the
[roadmap](/architecture/roadmap/); it is not written into a page as if built. This is the contract that
keeps the published design describing what exists: a built capability never sits behind a `Design` badge,
and a divergence is never silent.

## Style

- No em dashes. Use commas, colons, periods, or parentheses.
- No AI/assistant attribution.
- Write for someone learning the system, not someone who already built it. The same page
  serves the operator using the product and the contributor extending it.

## Publishing

Docs build in CI on every PR (so a broken docs build fails the PR) and are embedded into
the binary at release. The published site is docs.omniglass.hyperscaleav.com.
