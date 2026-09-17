---
name: file-bug
description: "Use the moment a defect, gap, or drift is noticed that the current work will not fix: capture it as a GitHub issue in the house shape (native Issue Type, area labels, repro, expected vs actual, found-during link) after a dedupe search, then return to the slice. The discipline that replaces bare TODOs, silent drive-by fixes, and knowledge that dies with the session."
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
  field, never a `prio:*` label. Labels are only `area:<subsystem>` (one or more, from
  the fixed set) and `run:<task>`.
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
2. **File it** with the shape below. In a remote session this is the GitHub MCP
   `issue_write` (Type via its `type` field); set the Priority field where the tooling
   allows, otherwise state the suggested priority in the body.
3. **Return to the slice.** The finding is captured; fixing it is a different day's
   scope decision.

## The shape

```markdown
## Symptom

<one user-observable sentence>

## Repro

<exact steps or commands, from a clean `make dev` or a named test>

## Expected / Actual

<one line each>

## Evidence

<the failing output, the code permalink (file and line at a commit SHA), or a
committed screenshot; enough that the fixer starts from proof, not from trust>

Found during #<issue or PR being worked>. <If autopilot work: the marker line.>
```

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
  re-derive the finding. The Evidence section is the point of the shape.

A fix that starts later starts right: the failing regression test first (the
test-driven doctrine), which is exactly what a well-filed Repro section makes cheap.
