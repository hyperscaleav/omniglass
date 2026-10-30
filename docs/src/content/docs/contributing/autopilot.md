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
   TODO or a drive-by fix. The slice goes In Progress on [the board](/contributing/labels/#the-board)
   as its worktree is cut.
4. **Ship.** The full `/ship-slice` pass, then the PR: conventional title, the
   ship-review as the body, `closes #NNN`, and the marker line last. The session
   subscribes to the PR's activity and arms an hourly check-in until merge or close. It
   never enables auto-merge: the architect's merge is the approval
   ([ADR-0859](/architecture/decisions/#adr-0859-the-autopilot-never-arms-auto-merge-the-architects-merge-is-the-approval)).
   The slice's issue goes In Review on the board as the PR opens.
5. **Report.** A closing comment on the slice issue and a day report: what shipped,
   what needs a decision, every issue filed, the day's board moves, what tomorrow will
   likely pick.

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
(early morning Mountain time), each firing a **fresh session** with completion
notifications on. The session runs on the architect's own worker machine, reached through
a Remote Control bridge environment, rather than in a cloud sandbox: the gates (Docker for
the testcontainers, the pinned protoc pair, the Playwright browser for `/assess-ui`) run
on a host the architect provisions once, and the session works in the machine's standing
clone of the repo. The worker has to be up, with the bridge connected, when the Routine
fires; a day it is not is a day the autopilot does not wake.

The worker needs, beyond Go, Node, and Docker: `gh` with the `gh image` extension (a
bridge session reaches GitHub through `gh`, not the cloud sandbox's MCP tools), wired to
act as the agent App (below), the
`d2` binary for any docs diagram, and the Playwright Chromium
(`cd web && npx playwright install chromium`).

The prompt is intentionally thin, so the behavior lives in the versioned skill rather
than in trigger config:

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

## Who acts as whom

Agent work is authored by its own GitHub identity, the `hyperscale-agent` GitHub App, never
by the architect's account
([#1281](https://github.com/hyperscaleav/omniglass/issues/1281)). Its commits, pushes, PRs,
comments and board moves read as `hyperscale-agent[bot]`, so the architect reviews and
approves agent PRs as a real second party, and a leaked agent credential reaches this one
repo for an hour instead of everything an org owner can touch.

| Surface | Acts as | Why |
|---|---|---|
| Autopilot and other agent sessions on the worker | `hyperscale-agent[bot]` | The worker reaches GitHub directly, so the App's token is the one GitHub sees. |
| Interactive sessions in the cloud sandbox | the architect | The sandbox's egress proxy replaces any GitHub credential with the connected account's (tested on #1281). The architect is driving, so the attribution is honest. |
| The claude.ai GitHub connector | the architect | It is the architect's approval channel, not an agent. |

So any work that must not act as the architect runs on the worker, never in the sandbox.

The identity tooling is shared by every Hyperscale AV product and lives in
[hyperscaleav/agent-harness](https://github.com/hyperscaleav/agent-harness): its
`agent-token` mints the App's token narrowed to this repo and is the clone's only git
credential helper, and its `gh` shim gives every `gh` call (the skills' and
`scripts/board.sh`'s included) a fresh App token. Its
[identity guide](https://github.com/hyperscaleav/agent-harness/blob/main/docs/identity.md)
covers creating the App, wiring the worker and proving it. With `AGENT_APP_ID` set, this
repo's SessionStart check proves at minute zero that a token mints, that `gh` resolves to
the shim, and that the clone's author is the bot, and names what is missing if not.

The architect merges agent PRs. With a separate author, the `main` ruleset can require one
approving review from the code owners, and the architect stays on the bypass list (pull
requests only) to merge their own work; the bot never is. That tightening lands after the
first bot-authored PR proves the wiring, and whether auto-merge returns is a new decision,
not this one ([ADR-0859](/architecture/decisions/#adr-0859-the-autopilot-never-arms-auto-merge-the-architects-merge-is-the-approval)).
