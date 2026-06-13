# Docs with everything

Omniglass ships its documentation *as part of the product*. The docs are not an
afterthought in a separate wiki; they are Hugo (Hextra) content under `docs/`, compiled
to a static site and embedded into the Go binary, served at `/docs`. The architecture is
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

- **Architecture pages** (`docs/architecture/`) hold the model: the spine plus leaf
  documents, the glossary, the locked decisions. Terms are defined once in the spine and
  not redefined in leaves.
- **Concept and learning pages** teach a concept interactively (see
  [the learning-tool restriction](learning-tool.md)). When a feature introduces a concept
  an operator must understand, the teaching surface ships with it.
- **Contributor pages** (`docs/contributing/`) are this doctrine set.

## Style

- No em dashes. Use commas, colons, periods, or parentheses.
- No AI/assistant attribution.
- Write for someone learning the system, not someone who already built it. The same page
  serves the operator using the product and the contributor extending it.

## Publishing

Docs build in CI on every PR (so a broken docs build fails the PR) and are embedded into
the binary at release. The published site is omniglass.hyperscaleav.com/docs.
