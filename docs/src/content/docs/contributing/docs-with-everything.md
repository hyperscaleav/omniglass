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
- state a one-line justification in the PR body (pure refactor, internal-only change,
  etc.).

The docs-touched gate is enforced by the PR template checklist and the `/ship-slice`
pre-ship pass, not by a label-driven CI check (the
[label taxonomy](/contributing/labels/) is closed, and no workflow reads a docs label). The
justification path exists so the gate never blocks a genuine internal change, not as a
routine escape hatch.

A mechanical layer backs the human gate: the **docs lint suite** (`internal/docslint`, run
by `go test` and so by `make test` and CI) checks the hand-written docs against the code.
Today that is the banned-vocabulary lint (retired identifiers must not appear in
current-tense prose) and the decisions-format lint (ADR numbering, index rows, required
fields), plus the docs-command guard in `internal/cli`; more lints (routes, permissions,
make targets, env vars, file paths) land under
[#429](https://github.com/hyperscaleav/omniglass/issues/429).

## What "the docs" means here

- **Architecture pages** (`/architecture/`) hold the model: the spine plus leaf
  documents, and the current decisions. Each official term is defined once in the
  [glossary](/architecture/glossary/) and not redefined in the leaves.
- **Guides** (`/guides/`) are how-to pages for someone *using* the product, split by
  audience: the **operator guide** (running the estate from the console and the CLI), the
  **admin guide** (managing accounts, access, audit, and config), and **deployment**
  (standing the platform up). **A slice that ships or changes a user-facing surface ships or
  updates its guide in the same PR**, not just the architecture page, filed under the section
  that matches who does the task. The architecture page says how the surface is built; the
  guide says how to use it.
- **Concept and learning pages** teach a concept interactively (see
  [the learning-tool restriction](/contributing/learning-tool/)). When a feature introduces a concept
  an operator must understand, the teaching surface ships with it.
- **Contributor pages** (`/contributing/`) are this doctrine set.

So a feature that adds an operator surface usually touches two homes: the **architecture**
page (the model) and a **guide** (the how-to). A purely internal change touches neither and
states its one-line justification in the PR body.

## Status moves with the code

The architecture pages are written in the present tense as the **target design**, so build status is
carried *alongside* the prose, not woven into it, and keeping it current is part of docs-with-everything.
A slice that advances a page updates three surfaces in the **same PR**:

- the page's **status badge** moves to its new floor (`Design` to `Partial` to `Built`), which the live
  grid on [implementation status](/architecture/status/) reads directly, so the grid never lies;
- the [build log](/architecture/build-log/) gains the slice's entry; and
- if the shipped code **diverges** from a page's design, the page carries an inline note and a
  [decision-log](/architecture/decisions/) entry (an ADR) lands in the same PR.

Forward-looking intent that is not yet a slice lives in a GitHub epic and is indexed on the
[roadmap](/architecture/roadmap/); it is not written into a page as if built. This is the contract that
keeps the published design describing what exists: a built capability never sits behind a `Design` badge,
and a divergence is never silent.

## The design fence

A badge is page-granular, and a page is not one thing: most architecture pages mix built
behavior with target design. The **design fence** marks the unbuilt part structurally, at
the section where it lives, instead of a hand-written "still design" sentence at the top
that rots when a slice ships:

```markdown
:::design[Target design, tracked in #434]
The prose in here describes something that does not exist yet.
:::
```

The fence renders as a visually distinct aside (dashed, purple, labeled), and its label
**must name the issue or ADR that tracks the gap**; a fence with neither fails the docs
build, so unbuilt prose always has an owner. To nest a normal aside inside a fence, give
the fence more colons (`::::design` around a `:::note`), which is remark-directive's
nesting rule.

The fence is also the boundary the [docs lint suite](#the-rule) keys on
(`internal/docslint`, the `Regions` split): the **vocabulary lint runs everywhere**,
fenced or not, because future design must still be written in current nouns, while the
**existence lints** (routes, permissions, tables, commands; #429) run only outside a
fence, where prose claims to describe something that exists. Building a fenced section
means **deleting the fence in the same PR**, exactly like moving the badge.

## Screenshots are generated, not pasted

Screenshots embedded on a docs page are a **generated resource**, treated like the OpenAPI
spec or the CLI reference, never a static image dropped in by hand. A page declares what it
needs in `screenshots` frontmatter (the shot's `id`, the console `path`, its `alt`, and any
interaction `steps`), and embeds it in the prose with a directive:

```markdown
---
title: Secrets
screenshots:
  - id: secrets
    path: /web/secrets
    alt: "The Secrets directory: type badges, owner scope, and masked field previews."
---

::screenshot{#secrets}
```

That frontmatter is the **single source**: `make docs-shots` reads it from every page, drives
the real console (the same binary an operator runs, never a mock), and writes
`public/screenshots/<id>.png`; the directive renders the figure from the same entry. So the
capture list and the embed cannot drift, a `#id` with no frontmatter entry (or no captured
image) **fails the build**, and adding a screenshot is a frontmatter edit, not a code change.

Because the images track the live UI, they are refreshed like any generated artifact: a change
to an operator surface **re-runs `make docs-shots` and commits the new PNGs**, and
`make docs-shots-check` recaptures against the real console and fails if a shot drifts beyond a
small tolerance, the visual sibling of the `make gen` drift check. (Tolerance rather than an
exact hash because the dev seed's random UUIDs move a fraction of a percent of pixels between
captures; a real UI change moves far more.)

## Style

- No em dashes. Use commas, colons, periods, or parentheses.
- No AI/assistant attribution.
- Write for someone learning the system, not someone who already built it. The same page
  serves the operator using the product and the contributor extending it.

## Publishing

The docs site builds in CI on every PR that touches `docs/**` (the `docs-build` workflow,
`.github/workflows/docs-build.yml`, path-filtered), so a broken `.mdx` page, a dead import,
or invalid frontmatter fails the PR that introduces it. The published site is
docs.omniglass.hyperscaleav.com; in time, the same content is embedded into the Go binary
to serve at `/docs`.
