---
title: Autopilot
description: "A scheduled session ships one thin slice per day unattended: how it chooses work, what it may decide alone, and why PR review is its approval gate (ADR-0138)."
---

The **autopilot** is the repo shipping without its architect in the room: a scheduled
Routine fires a fresh agent session each morning, the session runs the
[`/autopilot` skill](https://github.com/hyperscaleav/omniglass/tree/main/.claude/skills/autopilot)
end to end, and the day ends as one reviewable PR plus a set of filed issues. The
[slice workflow](/contributing/slice-workflow/) is unchanged inside the day; what changes
is where its one human gate is paid
([ADR-0138](/architecture/decisions/#adr-0138-the-autopilot-ships-a-daily-slice-and-pr-review-is-its-approval-gate)).

## The contract

The slice workflow's Define stage is a hard gate: no branch until the issue exists and
the architect has approved its scope. That gate assumed a present architect; unattended,
it deadlocks. ADR-0138 resolves it without weakening the boundary:

- For a slice the autopilot **scopes itself**, it files the definition issue in the
  [`/define-work`](/contributing/feature-loops/) shape, marks it
  `Mode: autopilot (ADR-0138)`, and proceeds. The architect's approval moves to the PR:
  **approving and merging is the accept, closing is the veto, a review comment is the
  redirect** (the autopilot answers and pushes).
- The ship-review stays the approval artifact, front-loading the two lines the architect
  reads first (Decisions I need from you; Risk), so the veto window is real, not
  ceremonial.
- **The autopilot never merges.** Merge is the architect's, always.

## The day, in order

1. **Reconcile.** Open autopilot PRs (found by their marker line) are driven back to
   green first: conflicts merged out, red CI root-caused, review comments implemented or
   answered. A green PR waiting only on the architect is left alone. PRs without the
   marker belong to the architect and are never touched, with one exception: a PR or
   epic the architect has explicitly handed over (the direction recorded as a marker
   comment on it) is **adopted**, and the autopilot drives it like its own from then on.
   Two or more autopilot PRs still open means today is a **maintenance day**: drive
   them, triage the issue list, file what the sweep finds, no new branch.
2. **Choose.** The first rung that applies: an in-progress
   [feature loop](/contributing/feature-loops/) resumes; an architect-approved
   definition starts its loop; an open `Bug` by board priority gets its fix, regression
   test first; else the autopilot scopes the next thin cut itself and files the marked
   definition. Approved definitions always outrank self-scoping, which is the main
   steering lever.
3. **Build.** The normal lifecycle: worktree off `origin/main`, the failing test first,
   green commits, docs with everything, primitive first. Any operator surface takes the
   [`/assess-ui`](https://github.com/hyperscaleav/omniglass/tree/main/.claude/skills/assess-ui)
   visual gate (the state matrix captured against the real console and graded, not
   glanced at). Anything found out of scope becomes an issue via `/file-bug`, never a
   TODO or a drive-by fix.
4. **Ship.** The full `/ship-slice` pass, then the PR: conventional title, the
   ship-review as the body, `closes #NNN`, and the marker line last. The session
   subscribes to the PR's activity, arms an hourly check-in until merge or close, and
   attempts to enable auto-merge (squash) so that where the repo requires an approving
   review, the architect's approval is the last click.
5. **Report.** A closing comment on the slice issue and a day report: what shipped,
   what needs a decision, every issue filed, what tomorrow will likely pick.

## What it may not decide alone

Self-scoping excludes the sharp edges: the two authorization layers, the migration
rules, the audit contract, breaking API changes, dependency majors. Wanting one of
those, the autopilot files the definition, leaves it awaiting a human approval comment,
and picks something else for the day. Red gates are never bypassed and never shipped: a
day that cannot go green ends as a pushed branch, an annotated issue, and an honest
report.

## Steering it

The architect steers without attending:

- **Pre-approve definitions.** An approval comment on a `/define-work` issue makes it
  tomorrow's work, ahead of anything the autopilot would choose.
- **Set board priority** on `Bug` issues; the autopilot takes `High` before `Medium`.
- **Review the PRs.** Comments are implemented or answered; a close is a respected veto.
- **Hand it existing work.** A direction on a PR or epic (given in session and recorded
  there as a marker comment) makes that work autopilot-driven until it merges or closes.
- **Find its work** by searching issues and PRs for `ADR-0138` (the marker line).
- **Pause or retime it** by disabling or editing the Routine; delete it and the
  autopilot simply never wakes again.

## The Routine

One scheduled trigger, owned by the architect's Claude account: daily at 11:00 UTC
(early morning Mountain time), each firing a **fresh session** in the repo's remote
environment, with completion notifications on. The prompt is intentionally thin, so the
behavior lives in the versioned skill rather than in trigger config:

```text
Omniglass autopilot: ship today's slice. Work in the hyperscaleav/omniglass clone in
this environment. Read CLAUDE.md, then invoke the /autopilot skill and follow it end
to end: reconcile the standing autopilot PRs, select one thin slice by the ADR-0138
order, build it through the full gates (test-first, docs-with-everything, /assess-ui
for any operator surface, /adversarial-review, /ship-slice), open the PR with the
ship-review as its body, subscribe to its activity, file an issue for every defect or
deferral met along the way, and end with the day's report. Never merge to main; the
architect approves PRs.
```

Changing what the autopilot does is a PR against the skill; changing when it runs is a
Routine edit. That split is deliberate: the contract is reviewable history, the schedule
is a dial.
