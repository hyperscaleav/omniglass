---
title: Slice workflow
description: "How a feature ships: one vertical slice per PR, through a fixed lifecycle of define, build test-first, document, validate, review, and a ship-review the architect approves."
---

A feature is **one vertical slice**: a thin cut through the whole stack (schema to API to docs)
that delivers a user-observable outcome, not a horizontal layer. Each slice is one PR, built
through a fixed lifecycle so quality is a process, not a hope.

## The lifecycle

| Stage | Practice | Gate |
|---|---|---|
| **Define** | a [feature issue](https://github.com/hyperscaleav/omniglass/issues/new/choose) under an epic: the outcome, the thin cut, the deferrals, the test plan, and the permission and scope it touches | scope agreed before code |
| **Design** | read the [architecture spine](/architecture/) (the docs are the spec); locate the seam; name the thin cut | the cut is explicit |
| **Branch** | a git worktree off `origin/main` under `.claude/worktrees/`, never a commit on `main` | isolated branch |
| **Build** | [test-first](/contributing/test-driven/): the failing test, then the feature, committing each increment | RED then GREEN |
| **Document** | the teaching [docs ship with it](/contributing/docs-with-everything/), plus a build-progress note on the status page | docs in the same PR |
| **Validate** | `make test` green (run fresh), `make gen` clean, no drift | green, fresh |
| **Review** | a reviewer pass over the diff, findings addressed; a security lens when it touches authz, secrets, the edge, or an invariant | findings cleared |
| **Ship** | the ship-review (below), then squash-merge | architect approves |
| **Log** | record what shipped, the decisions, and the follow-ups | logged |

The first six stages are the [five doctrines](/) in motion; the last two are how the work
becomes externally visible and approvable.

## What "validated" means

Not a vibe at each gate, a check:

- **The ticket is the contract.** The issue states the outcome, the thin cut, the deferred items
  (each its own issue), and the authorization surface (the permission checked and the scope
  injected). Written first, so the boundary is agreed before the code exists.
- **Tests are tiered and fresh.** Unit (pure, fast), integration (real Postgres via
  testcontainers, no mocking the database), and end-to-end (drive the entry point as the user).
  `make test` is the gate, run without a cache: a cached pass or a `-short` run hides the
  database-backed behavior, and a green claim is not evidence until the tier actually executed.
- **Docs ship with the feature.** The page that teaches the concept lands in the same PR, the
  architecture-of-record stays consistent, and any divergence is stated, never silent.
- **The API cannot drift.** `make gen` regenerates the OpenAPI and the clients from the Go; a
  non-empty diff fails the slice until committed.
- **Review verifies behavior to the outcome,** not just the call site.

## The thin-cut discipline

A slice ships the smallest honest increment. A **thin cut** is a deliberate simplification (the
first auth slice did bearer tokens only, and resolved the owner scope to all); a **deferral** is
work moved to a later slice. Both are explicit: a thin cut is documented in the slice, a deferral
is a filed issue. The opposite, a silent gap, is the failure this discipline prevents.

## The ship-review (the approval artifact)

At PR-ready, the slice is presented as one **ship-review**, front-loaded so the architect approves
in seconds or redirects. The `/ship-slice` skill runs the pre-ship checklist and emits it:

```
SHIP REVIEW - <type>: <slice>   (PR #N, closes #M)

Outcome:   <one user-observable line>
Verdict:   ready | ready-pending-your-call

Scope:     in / thin cut / deferred (#issues)
Proof:     make test green (fresh, N packages); the load-bearing behaviors; tiers; make gen clean
Docs:      what shipped; arch-of-record consistent or a divergence note
Review:    findings and how addressed; security note if relevant

Decisions I made (your veto window): the judgment calls that bound the design
Decisions I need from you:           open forks, or none

Diff / Risk: size, PR link; outward-facing? invariant-changing? reversible?
```

Approval means squash-merge (the conventional-commit PR title drives the release). A redirect
adjusts the slice. The two lines that matter most are **Decisions I need from you** and **Risk**.

## Lessons held

- **Commit per increment.** A slice is built as a sequence of green commits, not one batch at the
  end. Work that is not committed is work that can be lost.
- **Verify fresh.** Re-run the database-backed tests before claiming green; do not trust a cache
  or a delegated agent's report.
- **Approve at the boundary.** Scope is agreed at the ticket and again at the ship-review, so a
  surprise never lands in `main`.
