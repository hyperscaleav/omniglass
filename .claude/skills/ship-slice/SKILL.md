---
name: ship-slice
description: Use when a vertical-slice PR is built and about to be proposed for merge. Runs the pre-ship validation (fresh make test, make gen drift check, em-dash and attribution scan, a reviewer pass, the docs-with-everything check) and emits the standard ship-review report the architect approves from. Invoke at PR-ready, before asking for approval; re-run after addressing review findings.
---

# Ship a slice

A slice is shippable only after a fixed validation pass, presented as one **ship-review** the
architect can approve in seconds. This skill is the gate between "code green" and "request
merge." It does not merge; it produces the approval artifact.

## When

- A vertical-slice PR is built, committed, and pushed.
- Before asking the architect to merge.
- Again after addressing review findings.

## Pre-ship checklist (run it, do not assume)

Each is a gate; a red one blocks the ship.

1. **Tests, fresh.** `go test -count=1 ./...` (no cache). Never trust a cached pass or a
   subagent's "it's green": the integration and e2e tiers must actually execute, and `-short`
   or a cache hides the DB-backed behavior. Note *which* behaviors passed, not just `ok`.
2. **API-first drift.** `make gen`, then `git diff --exit-code` on the generated artifacts. A
   non-empty diff means the committed spec or clients drifted from the Go; commit the regen.
3. **House style.** No em dashes and no AI/assistant attribution in any changed file (grep the
   diff; scan the commit messages and the PR body).
4. **Docs with everything.** The teaching docs ship in this PR, the `status.mdx` build-progress
   entry is added, and the architecture-of-record is consistent or a divergence is stated
   explicitly. Never silent.
5. **Review.** A reviewer pass over the diff (`code-review` or `cavecrew-reviewer`), findings
   addressed. Add a `security-review` lens if the slice touches authz, secrets, the edge, or an
   invariant. Verify behavior to the outcome line, not just call sites.
6. **Scope honesty.** Every thin cut is documented; every deferral is a filed issue.
7. **Evidence in the PR.** Paste the *actual* fresh test output (the tail of `make test`, plus
   web tests if touched) into the PR body, not a "they pass" claim. For any operator-facing
   change, include **screenshots driven live** (e.g. against `make dev`): upload via the
   `gh image` extension when `GH_SESSION_TOKEN` is set, otherwise commit them under
   `.github/screenshots/` and embed by **immutable commit SHA**
   (`https://raw.githubusercontent.com/<owner>/<repo>/<sha>/.github/screenshots/...`), so the
   link survives the branch being deleted on squash-merge.

## The ship-review (emit this, in chat and as the PR body)

```
SHIP REVIEW - <type>: <slice>   (PR #N, closes #M)

Outcome:   <one user-observable line>
Verdict:   ready | ready-pending-your-call

Scope
  In:        <bullets>
  Thin cut:  <deliberate simplifications>
  Deferred:  <#issue refs>

Surfaces:  API <ops> / CLI <commands, generated> / UI <view, live | stub>
Proof (ran fresh)
  make test: green, N packages   <pasted output in the PR body>
  Behaviors: <the RED->GREEN and the load-bearing assertions>
  Tiers:     unit X / integration Y / e2e Z
  make gen:  clean
Visual:    <screenshots in the PR for any UI surface | n/a>

Docs:      <what shipped; arch-of-record consistent | divergence note>
Review:    <reviewer findings + how addressed; security: n/a | note>

Decisions I made (your veto window): <judgment calls that bound the design>
Decisions I need from you:           <open forks | none>

Diff:      N files, ~M LOC   <PR link>
Risk:      <outward-facing? invariant-changing? reversible?>
```

The two lines the architect reads first are **Decisions I need from you** and **Risk**.
Approval means squash-merge; a redirect adjusts the slice.

## After approval

Squash-merge (the conventional-commit PR title drives the release), remove the worktree, then
`logwork`.
