---
name: adversarial-review
description: "Use for the per-slice review pass on any omniglass diff, and always before a feature-loop slice merges inward: a refute-not-confirm review that sweeps the repo's anti-pattern catalog (references/anti-patterns.md) against every hunk and admits only findings that carry a concrete failure scenario traced through the actual code. Generic praise and style nits are not output; catalog updates ride the same PR as the change that makes them."
---

# Adversarial review

A reviewer that tries to prove the diff wrong, and tries equally hard to prove its own
findings wrong. The output is a short list of findings that survived refutation, each with
a concrete failure scenario, or an explicit "swept, nothing survived". Anything else
(compliments, hedges, style preferences) is noise and is not emitted.

## Posture

- **Refute the code.** For each candidate finding, trace the failure through the real
  code: name the inputs and state, follow the calls, and state the wrong outcome. A
  finding you cannot drive to a wrong outcome is dropped, not hedged.
- **Refute the finding.** Before emitting, attempt the counter-argument (a guard upstream,
  a constraint that makes the state unreachable, a test that pins the behavior). A finding
  that dies under its own cross-examination is dropped.
- **Verify against code, not memory or docs.** Architecture prose can lag or lie
  (the 2026-07-30 audit is the proof); when a doc claim and the code disagree, the code is
  the fact and the disagreement is itself a finding (the neighbor check).
- **Load the `solidjs` skill before judging anything under `web/src`.** The single most
  common false finding in this repo is Solid code reviewed with React intuitions; a
  finding that assumes React semantics is invalid, not merely weak.

## Procedure

1. Read the slice's sub-issue (the scope contract) and the full diff.
2. Sweep every hunk against the **anti-pattern catalog**
   ([references/anti-patterns.md](references/anti-patterns.md)): each entry has a
   detection cue; a cue that fires makes a candidate finding.
3. Beyond the catalog, interrogate the diff against the invariants: every route gated,
   every applicable query scoped, every privileged mutation audited in-transaction,
   migrations idempotent and never edited, no fixed test ports, tests prove behavior
   fresh (no mocking the system under test).
4. Refute in both directions (posture above). Keep survivors only.
5. **Neighbor check:** if the diff flips a behavior, grep the docs tree for the claim it
   falsifies and add a finding per stale claim.
6. Emit the report:

   ```
   ADVERSARIAL REVIEW - <slice> (<commit or branch>)
   Verdict: merge-blocking findings: N / advisory: M / swept clean
   1. [blocking|advisory] <file:line> <catalog-id or invariant>
      Failure: <inputs and state, then the wrong outcome, concretely>
      Fix: <the pattern, with an exemplar path>
   Catalog: <entries added, retired, or updated by this slice | none>
   ```

   Merge-blocking: an invariant violation, a data-loss or authz failure scenario, a
   catalog entry with a live failure. Advisory: real but survivable (filed or fixed at
   the author's choice, stated which).
7. **Maintain the catalog.** A slice that fixes an anti-pattern class flips that entry's
   status in the same PR; a review that discovers a new class appends an entry. The
   catalog is a living artifact and its staleness is itself a defect.

## In a feature loop

This pass is a per-slice gate ([feature loops](../../../docs/src/content/docs/contributing/feature-loops.md)):
it runs over the slice diff before the slice merges inward, findings fixed or refuted
with evidence in the sub-issue's closing comment. `/ship-slice` gate 7 then covers the
rolled-up diff before the PR.
