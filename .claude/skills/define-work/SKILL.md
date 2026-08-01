---
name: define-work
description: "Use when drafting a body of work for architect approval before a feature loop: an Epic (multi-slice) or Feature (single slice) issue in the standard approvable shape (outcome, scope, thin cuts, out of scope, surfaces, acceptance, proposed sub-issues, planned PR count, open decisions). The definition half of the feature-loops contract (ADR-0074); no sub-issues and no branch until the architect approves."
---

# Define a body of work

Drafts the prose definition the architect approves once, so the loop that follows runs
without conversational touchpoints. The definition is a GitHub issue; approval is the
architect's comment on it. Until that comment exists: no sub-issues, no branch, no code.

## Choose the container

- **Epic**: several slices that ship together as one rollup PR (native Issue Type `Epic`,
  title `epic: <name>`).
- **Feature**: one slice, possibly with sub-issues for its parts (Issue Type `Feature`,
  title `feat: <name>`).

Same shape either way; an epic is just the plural case. If the work is a single small
slice with no sub-structure, skip this skill and file a normal feature issue per the
[slice workflow](../../../docs/src/content/docs/contributing/slice-workflow.md).

## The shape (every section, in this order)

1. **Outcome.** One or two sentences, user-observable, leading the body.
2. **Scope.** What ships, in prose or short bullets. Name the seam and the primitive it
   builds or consumes (primitive-first).
3. **Thin cuts.** The deliberate simplifications, stated up front so approval covers them.
4. **Out of scope.** What a reasonable reader might assume is included but is not, each
   with its home (an issue, a sibling epic, or "later, unfiled").
5. **Surfaces touched.** API routes, CLI commands, console pages, schema elements,
   docs pages; and the authorization surface (the permission checked, the scope injected).
6. **Acceptance behaviors.** The tests that must exist and pass, phrased as behaviors
   ("a viewer cannot X", "a re-run changes nothing"), not implementation.
7. **Proposed sub-issues.** The slice breakdown with conventional-commit prefixes. These
   are proposals; the loop creates them only after approval.
8. **Planned PR count.** Default **one** rollup PR (ADR-0074). More than one is the
   exception and must be justified here, never improvised mid-loop.
9. **Decisions left open.** The forks the architect may want to rule on now; anything
   unresolved here is outside what approval covers.

## House rules for the draft

- Prose-first, portfolio-quality, no em dashes, no AI attribution, head-noun-last naming
  that matches the architecture glossary.
- `area:*` labels for every subsystem touched; kind of work is the Issue Type, never a
  label.
- Cite pages of the architecture of record by link; treat unverified prose claims about
  built behavior with suspicion and prefer code or generated artifacts as sources.
- Generate-first: if the work would hand-write a fact the code can generate, the
  definition says how it will be generated instead.

## After posting

State plainly that the issue awaits the architect's approval comment, and stop. When the
approval lands, `/run-feature-loop` takes over.
