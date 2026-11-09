---
name: file-bug
description: "Use the moment a defect, gap, or drift is noticed that the current work will not fix: capture it as a GitHub issue in the house shape (native Issue Type, area labels, one tier and one risk label, the five-section ticket contract the dispatcher launches on, found-during link) after a dedupe search, then return to the slice. The discipline that replaces bare TODOs, silent drive-by fixes, and knowledge that dies with the session."
---

# File a bug

Everything worth doing later lives in an issue; nothing lives in a TODO doc, a bare
`// TODO`, or a session's memory. This skill is the capture reflex: notice, file, return
to the slice. It costs two minutes; losing the finding costs the rediscovery.

## When

- A **defect** in shipped behavior (wrong output, a broken state, a guard that does not
  fire): Issue Type `Bug`, title `fix: <the symptom>`.
- A **gap or enhancement** (a missing capability, an awkward flow): Type `Feature`,
  title `feat: <the outcome>`.
- A **chore, drift, or tooling gap** (docs contradicting code, a stale generated
  artifact, a missing flag): Type `Task`, title `chore:`/`docs:`/`test:` as fits.
- Kind of work is the **Issue Type, never a label**; priority is the Project board
  field (the architect's to set), never a `prio:*` label. Labels are `area:<subsystem>` (one
  or more, from the fixed set), exactly one `tier:{jr,mid,sr}` and one `risk:{low,high}` for
  the dispatcher, and `run:<task>`. Tier is where the work starts (`jr` mechanical, one or two
  files; `mid` a slice with tests and docs; `sr` cross-cutting, a migration, an invariant);
  risk is `high` for authz, scope, secrets, the edge, a migration or a delete.
- Titles are conventional-commit phrased with a lowercase subject, matching the PR that
  will eventually close them.

**The carve-out:** a security-sensitive finding (an authz bypass, a secret leak, an
injection path) is NOT detailed in a public issue. Report it to the architect directly
(the day report or the ship-review's Review line), with at most a vague placeholder
issue ("harden input handling on X") if tracking is needed.

## Procedure

1. **Dedupe first.** Search the issues (semantic search on the symptom, plus the key
   nouns) before creating. A hit means a comment adding your new evidence on the
   existing issue, not a duplicate.
2. **File it** with the shape below, the five-section ticket contract the dispatcher
   launches on ([labels](../../../docs/src/content/docs/contributing/labels.md#the-ticket-contract)).
   In a remote session this is the GitHub MCP `issue_write` (Type via its `type` field,
   labels in its `labels` list); on the bridge worker it is `gh api
   repos/hyperscaleav/omniglass/issues -f type=Bug -f title=... -F body=@body.md -f
   'labels[]=area:ui' -f 'labels[]=tier:mid' -f 'labels[]=risk:low'` (the REST create takes
   the Type directly). Never set the Priority field, which is the architect's alone; state the
   suggested priority in the body instead. Check the body before creating when the
   dispatcher's lint is at hand: `gh issue view <n> --json body,labels | bin/ticket-lint`
   from the agent-harness checkout, or read the five headings and the checkbox list by eye.
3. **Put it on the board** in Backlog: `scripts/board.sh add <n>`. Where the script cannot
   run (no `gh`, or a token without the `project` scope; it says which), name the missed
   move in your report ([the board](../../../docs/src/content/docs/contributing/labels.md#the-board)).
4. **Return to the slice.** The finding is captured; fixing it is a different day's
   scope decision.

## The shape

```markdown
### Goal

<the symptom in one user-observable sentence, then what should happen instead and
why it matters>

### Spec

<the reproduction: exact steps or commands, from a fresh `make lab LAB_ARGS=--fresh`
or a named test>

<the evidence: the failing output, the code permalink (file and line at a commit SHA),
or a committed screenshot; enough that the fixer starts from proof, not from trust>

<the suspected cause and the fix, if known>

Found during #<issue or PR being worked>. <If autopilot work: the marker line.>

### Acceptance criteria

- [ ] a regression test fails before the fix and passes after
- [ ] <the user-observable outcome, one line>

### Out of scope

<the neighbouring defects this fix must not touch, each with its own issue>

### Files likely touched

- <path as it is in the tree>
```

The headings are the issue forms' labels exactly (`### Goal`, not `## Goal`), because the
dispatcher's lint matches them by name. Expected and actual live in Goal; repro and evidence
live in Spec. A gap or a chore takes the same five sections with the feature or chore form's
questions (surfaces, permission and scope, the thin cut and what it defers) folded into Spec.

## What this replaces (the anti-patterns)

- **The bare TODO.** `// TODO: handle X` is a finding with no home; write
  `// TODO(#NNN): handle X` only after #NNN exists, or do not write it.
- **The drive-by fix.** An out-of-scope fix riding the current diff widens the PR,
  dodges test-first, and hides from review. File it; let it be its own tested slice.
  The one exception: a defect the current slice's own new tests already cover may be
  fixed in place, named in the ship-review's Scope block.
- **The silent downgrade.** Deciding mid-slice that a planned behavior is "later" is a
  deferral, and a deferral is a filed issue named in the ship-review, never a quiet
  omission.
- **The trust-me report.** An issue whose body is one sentence makes the next session
  re-derive the finding. The evidence in Spec is the point of the shape.

A fix that starts later starts right: the failing regression test first (the
test-driven doctrine), which is exactly what a well-filed Spec, with its reproduction, makes
cheap.
