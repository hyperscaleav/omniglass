---
title: Feature loops
description: "How a body of work ships through an agent loop: a prose definition approved once, slices cascading on an integration branch, and one rollup PR the architect merges."
---

A **feature loop** is how a multi-slice body of work ships when an agent executes it
end to end: the architect approves a prose definition once at the start and makes a merge
decision once at the end, and everything between runs against gates that are mechanical,
not conversational. The [slice workflow](/contributing/slice-workflow/) is unchanged
*inside* each slice; what a loop changes is the PR granularity (one PR per approved
definition, not per slice) and who drives the lifecycle between the two human touchpoints
([ADR-0074](/architecture/decisions/#adr-0074-an-approved-definition-rolls-up-to-one-pr-slices-cascade-on-an-integration-branch)).

## The lifecycle

**Define.** Work is defined in prose on an `Epic` (multi-slice) or `Feature`
(single-slice) issue, in the standard approvable shape the `/define-work` skill drafts:
outcome, scope, thin cuts, out of scope, surfaces touched, acceptance behaviors, proposed
sub-issues, planned PR count (default one), and the decisions the definition leaves open.
No sub-issues exist yet and no branch exists yet.

**Approve.** The architect reads the definition and approves or redirects it, as a comment
on the definition issue. This is the existing hard gate (no branch until the issue exists
and its scope is approved), paid once per body of work instead of once per conversation.
Approval covers everything the definition states; it does not cover what the definition
leaves open, which stays on the issue as a question.

**Loop.** On approval the loop creates the sub-issues under the parent and opens one
**integration branch** for the definition. Each sub-issue is one slice, built test-first
through the normal lifecycle, and merged **inward** to the integration branch only when
its gates are green. Progress is readable from the sub-issue list itself: a sub-issue is
closed when its slice merges inward, with the commit named in the closing comment. The
sub-issue list is the status; there is no parallel checklist to drift.

**Roll up.** One PR from the integration branch to `main`, carrying the full
[`/ship-slice`](/contributing/slice-workflow/#the-ship-review-the-approval-artifact)
ship-review for the whole diff: fresh `make test` output, `make gen` clean, live
screenshots for any operator surface, and the docs that teach it. The loop never merges to
`main`; merge is the architect's call, always.

## Branch topology

```
main
 └── <type>/<definition-name>            the integration branch, from origin/main
      ├── <type>/<definition-name>--<slice-a>   cascade branch, merges inward
      ├── <type>/<definition-name>--<slice-b>   cascade branch, merges inward
      └── ...                                   one rollup PR to main at the end
```

- The integration branch follows the normal naming rule (`<type>/<short-name>`, the
  conventional-commit type of the eventual rollup PR title).
- A **cascade branch** per sub-issue is the default for parallel work or a slice large
  enough to want its own local history. A serial loop working slices one at a time may
  commit a slice directly to the integration branch instead; the per-slice gates apply
  either way, and each commit references its sub-issue.
- Per-slice gates, before a slice merges inward: the failing test first, `make test-short`
  green while iterating, the docs lint suite green, and an `/adversarial-review` pass over
  the slice diff with findings addressed.
- The rollup gate is `/ship-slice` over the whole integration diff, which re-runs the full
  fresh `make test`. A red rollup gate is fixed on the integration branch before the PR is
  proposed.

## Decision authority

The loop decides alone, within the approved scope:

- implementation choices a reviewer would accept either way (naming within conventions,
  file placement, test structure);
- thin cuts, documented in the ship-review's Scope block as always;
- the sub-issue breakdown itself, provided it adds up to the approved definition and no
  more.

The loop **stops and asks** instead of proceeding when:

- a genuine fork appears that the definition left open (it goes to the ship-review's
  "Decisions I need from you" line if it can wait, or stops the loop if later slices
  depend on it);
- a gate stays red after honest attempts to fix it (a red gate is never bypassed;
  `--no-verify` still requires explicit approval);
- the work grows beyond the approved definition (new scope means a new or amended
  definition, not silent expansion);
- a change would touch an invariant (the authorization layers, the migration rules, the
  audit contract) in a way the definition did not name.

## Limits and the exception

- One integration branch and at most one open rollup PR per definition.
- Sub-issues are worked serially by default; parallel cascade branches are for genuinely
  independent slices.
- The default is **one** rollup PR. A definition too large for one reviewable diff names
  its planned PR count in the Define stage, so the granularity is approved with the scope,
  never improvised mid-loop.
- After the rollup PR opens, the loop may push review-response fixes within the approved
  scope without a fresh approval. Anything beyond that scope goes back through Define.
