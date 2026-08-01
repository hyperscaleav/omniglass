---
name: run-feature-loop
description: "Use when an approved definition (an Epic or Feature issue in the /define-work shape, with the architect's approval comment) is ready to execute as a feature loop: creates the sub-issues, opens the integration branch, works each slice test-first through the per-slice gates, merges inward, and rolls the whole body of work up into one PR via /ship-slice. The executor half of the feature-loops contract (ADR-0074)."
---

# Run a feature loop

The executor procedure for the [feature loops contract](../../../docs/src/content/docs/contributing/feature-loops.md).
The contract page is the authority on the lifecycle, decision authority, and limits; this
skill is the run-book. The loop never merges to `main`; merge is the architect's call.

## Preconditions (verify, do not assume)

1. The definition issue exists, is typed `Epic` (multi-slice) or `Feature` (single slice),
   and is written in the `/define-work` shape.
2. **The architect has approved it in a comment on the issue.** No approval comment, no
   loop: post nothing, branch nothing, ask instead. Approval covers what the definition
   states, not what it leaves open.
3. `origin/main` is fetched and the working tree is clean.

## Setup

1. **Create the sub-issues** under the definition, one per slice: conventional-commit
   prefix in the title (`feat:`/`fix:`/`docs:`/`chore:`/...), the matching native Issue
   Type (`Task`/`Bug`/`Feature`), the `area:*` labels, a short body with scope and
   acceptance, linked as GitHub sub-issues of the parent. The breakdown must add up to the
   approved definition and no more.
2. **Open the integration branch** from `origin/main`, named `<type>/<short-name>` for the
   eventual rollup PR title, in a worktree under `.claude/worktrees/` when working locally.
3. Order the queue by dependency, then by risk (the slice most likely to invalidate the
   plan runs first).

## Per slice

1. Branch a cascade branch `<type>/<short-name>--<slice>` off the integration branch, or
   work serial commits directly on the integration branch when slices run one at a time
   (either way every commit references its sub-issue, `(#NNN)`).
2. Build test-first: the failing test, then the code, committing each green increment.
3. Gates before the slice merges inward, every one green:
   - `make test-short` (and the full affected packages fresh where the slice touches the
     DB path);
   - the docs lint suite (`go test ./internal/docslint/ -count=1`);
   - the docs that teach the slice, in the same slice (docs-with-everything does not wait
     for the rollup);
   - an `/adversarial-review` pass over the slice diff, findings fixed or refuted with
     evidence.
4. Merge inward and **close the sub-issue** with the commit named in the closing comment.
   The sub-issue list is the status; update nothing else.

## Stopping

Stop the loop and surface to the architect (do not improvise past these):

- a fork the definition left open that later slices depend on;
- a gate that stays red after honest attempts (never bypass; `--no-verify` still requires
  explicit approval);
- scope growth beyond the approved definition (new scope goes back through `/define-work`);
- an invariant touch (authz layers, migration rules, audit contract) the definition did
  not name.

A fork that can wait goes to the ship-review's "Decisions I need from you" line instead.

## Rollup

1. When the queue is empty, run `/ship-slice` over the **whole integration diff** against
   `main`: fresh full `make test` with output pasted, `make gen` drift, the em-dash and
   attribution scan, docs-with-everything across every slice, screenshots for any operator
   surface. Fix reds on the integration branch.
2. Open **one PR** from the integration branch to `main`, titled `<type>: <definition>`,
   body = the ship-review, `closes #<definition>`.
3. After the PR opens: review-response fixes within the approved scope may be pushed
   without fresh approval; anything beyond goes back through Define. The loop ends when
   the architect merges or closes the PR.
