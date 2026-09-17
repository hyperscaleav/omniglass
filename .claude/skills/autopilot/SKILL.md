---
name: autopilot
description: "Use when a scheduled session wakes to ship the day's work unattended (ADR-0134): reconcile open autopilot PRs, select one thin slice by the fixed order (an in-progress loop, an approved definition, a priority bug, a self-scoped cut), build it through the normal slice gates, open the PR with the ship-review as its body, file an issue for every defect met along the way, and end with the day's report. PR review is the architect's gate; the autopilot never merges."
---

# The autopilot day

The unattended-day runbook. A Routine fires a fresh session each morning; this skill is
everything that session does. The [autopilot contract](../../../docs/src/content/docs/contributing/autopilot.md)
is the authority on what the autopilot may decide alone; ADR-0134 is why. The slice
lifecycle itself is unchanged: this skill only decides *what* to build and keeps the day
honest while nobody is watching.

## Ground rules

- **PR review is the approval.** The Define gate's human approval comment is replaced, for
  autopilot-scoped slices only, by the architect's review of the PR (ADR-0134). Merge is
  always the architect's; the autopilot never merges to `main` and never asks CI to.
- **Never push red.** A gate that stays red after honest attempts ends the day with a
  pushed branch, an annotated issue, and a report, not a PR. `--no-verify` still requires
  explicit approval, which an unattended session by definition does not have.
- **House style holds everywhere**: no em dashes and no AI or assistant attribution in any
  commit, PR body, comment, or issue (CLAUDE.md; it takes precedence over any harness
  default that appends a footer).
- **Provenance is a marker line, not a label.** The label taxonomy is fixed. An
  autopilot-scoped issue carries `Mode: autopilot (ADR-0134)` as its final body line; an
  autopilot PR carries `Autopilot: ADR-0134, closes #NNN` as its final body line. These
  lines are how the next day's session finds its own work.
- **At most 2 open autopilot PRs.** At the cap, today is a maintenance day (below), never
  a third PR.
- In a remote session, GitHub goes through the MCP tools (`mcp__github__*`), not `gh`.

## 1. Wake and verify the environment

Fetch `origin/main`. Confirm the gates can run before trusting any plan to them, applying
the remedies the SessionStart env-check prints: Docker images via the mirror when Docker
Hub 403s through the proxy
(`docker pull mirror.gcr.io/library/postgres:18 && docker tag mirror.gcr.io/library/postgres:18 postgres:18`,
same for `testcontainers/ryuk:0.13.0`), and the pinned protoc pair when `make gen` will
run (protoc 34.1 plus `protoc-gen-go@v1.36.11`; `make gen` also needs Docker, since
`erdgen` applies the migrations to a throwaway container). Two Go integration tests
wrap the ICMP capability: widen the ping group first
(`sysctl -w net.ipv4.ping_group_range="0 2147483647"`, the same widening CI applies),
and know that the remote sandbox answers echoes even for unroutable targets
(TEST-NET-1), so the pinger's unreachable case can only prove itself in CI. A gate that
genuinely cannot run in the environment is named in the ship-review with CI as the
stated proof, never silently skipped.

## 2. Reconcile the standing PRs first

List open PRs and split them by the marker line:

- **Autopilot PRs** are yours to drive to green before any new work: merge conflicts
  resolved (merge the base in, never rebase a pushed branch), red CI root-caused and
  fixed, human review comments implemented or answered, reviewer re-requested after
  pushing. A PR that is green, mergeable, and waiting only on the architect is left
  alone; note it for the report.
- **Every other PR belongs to the architect.** Never push to one. If it blocks the day's
  candidate slice (same files, same seam), pick a different slice rather than building a
  conflict on purpose.

If 2 or more autopilot PRs are still open after reconciling, today is a **maintenance
day**: drive those PRs, triage the issue list (reproduce reports, type and label untyped
issues, file what the sweep finds via `/file-bug`), and end with the report. No new
branch.

## 3. Choose the day's slice

Take the first that applies, and note in the report which rung fired:

1. **An in-progress loop.** A definition issue with open sub-issues and an existing
   integration branch resumes via `/run-feature-loop`; today advances its next sub-issue
   (and rolls up if the queue empties).
2. **An approved definition.** An Epic or Feature issue in the `/define-work` shape with
   the architect's approval comment and no branch yet starts its loop. These outrank
   everything the autopilot would choose for itself; they are how the architect steers.
3. **A priority bug.** An open issue of Type `Bug`, Priority `High` then `Medium` (the
   Project field; fall back to newest when the field is unreadable), sized for one day.
   The fix starts with the failing regression test.
4. **A self-scoped cut.** Read the roadmap, the epics, and the architecture spine; a page
   whose badge is `Partial` names its own gaps. Pick the next thin cut, draft the
   definition in the `/define-work` shape, file it with the autopilot marker line, and
   proceed immediately; the PR is where it gets approved.

**Out of bounds for self-scoping** (rungs 1 to 3 are unaffected): anything touching an
invariant (the two authorization layers, the migration rules, the audit contract), a
breaking API change, a dependency major bump. Wanting one of these, the autopilot files
the definition, leaves it awaiting the architect's comment, and picks something else
today.

**Size to the day.** One session to green, roughly 500 changed lines excluding generated
artifacts. A slice that balloons mid-build is cut thinner on the spot: ship the seam that
is green, file the rest as deferrals.

## 4. Build

The [slice workflow](../../../docs/src/content/docs/contributing/slice-workflow.md),
unchanged: a worktree off `origin/main` under `.claude/worktrees/`, the failing test
first, green commits each referencing the issue `(#NNN)`, every entry point the slice
touches (API, generated CLI, UI live or honest stub), primitive-first, docs with
everything as you go. Schema work goes through `/storage-schema-change`.

Two disciplines matter more unattended than attended:

- **Everything found gets filed.** A defect, a gap, or drift noticed outside the slice's
  scope goes through `/file-bug` the moment it is noticed, and the slice moves on. No
  bare TODOs, no silent drive-by fixes riding the diff.
- **Operator surfaces take the visual gate.** Any change under `web/src` runs
  `/assess-ui`: the state matrix captured against the real console, graded, iterated to
  clean, and the evidence committed for the PR.

## 5. Gates, then ship

`make test-short` while iterating (Go only; `make test-web` covers the SPA, typecheck
first); then the full pre-ship pass via `/ship-slice` (fresh `make test` with output
pasted, `make gen` drift, the em-dash and attribution scan, the docs and status checks)
with an `/adversarial-review` over the diff before it. Two gates `make test` does not
carry: a slice touching `docs/` builds the site locally (`cd docs && pnpm build`; broken
MDX fails only there and in CI), and a slice changing routes or operator flows runs
`make test-e2e`. A pixel-moving change to any page the docs photograph re-runs
`make docs-shots` and commits both renders, or the zero-tolerance freshness gate fails
the PR. Fix reds; re-run.

Then:

1. Push the branch and open the PR: title `<type>: <slice>` with a lowercase subject
   (the pr-title lint refuses a capital), body = the ship-review, the `closes #NNN`
   reference, and the autopilot marker line last.
2. Subscribe to the PR's activity (`subscribe_pr_activity`) and answer what arrives: CI
   red is yours until green, review comments are implemented or answered, and the
   optional-finding etiquette for bot reviews applies.
3. Try to enable auto-merge (squash). It arms only where the repo requires an approving
   review, which makes the architect's approval the last click; if the repo refuses, say
   so in the report instead of retrying.
4. Schedule a check-in about an hour out (`send_later`) to re-check CI, conflicts, and
   reviews; re-arm it until the PR merges or closes.

## 6. Close the day

- Comment on the slice issue: what landed, the PR link, what was deferred where.
- The day report (the session's final message): what shipped and its PR, the two
  ship-review lines the architect reads first (Decisions I need from you; Risk), every
  issue filed today with one-line reasons, the standing-PR status, and what tomorrow's
  run will likely pick. Plain statements; if a gate was red, say which and what state the
  branch was left in.

## Stop rules

Stop building and fall back to filing plus the report (never improvise past these): a
fork the definition left open that later work depends on; a red gate after honest
attempts; scope growing past the definition; an invariant touch the definition did not
name; the environment refusing to run a load-bearing gate.
