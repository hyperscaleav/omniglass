---
title: "The perf gate runs on a test VM, not in CI"
date: 2026-11-12
status: "Accepted"
summary: "Amends ADR-1640's CI half: the `perf` workflow and the `run:perf` label are removed, and every rung's baseline, 300 included, is generated on a test VM of 8 cores and 16 GiB. The gate is run by hand on that host class, where every figure gates, and is still never a required check"
refs: [1787, 1640, 1780, 1783, 1772]
---

- **Date:** 2026-11-12 | **Status:** Accepted | **Pages:** [lab](/guides/lab/#checking-against-the-baseline), [test-driven](/contributing/test-driven/), [labels](/contributing/labels/)
- **Decision:** (#1787; amends [ADR-1640](#adr-1640-a-scale-lab-measured-end-to-end-gates-against-a-committed-baseline), item 1's CI job and item 8's `ci` baseline.) No workflow runs `lab perf check` or `lab perf baseline`. `corporate-300.json` is regenerated on a test VM of the same host class as `corporate-1000.json` (8 cores, 16 GiB), so one host class carries every rung, and a check run by hand on that class gates every figure.
- **Consequences:** `.github/workflows/perf.yml` and the `run:perf` label are gone. A change that could move a figure is checked by hand on a test VM, and its PR body says so; a PR that changes a baseline still says why (the ship-slice checklist holds it). Off that host class the host-dependent times and usage read `reported` and the counts and exact figures still gate, as before.
- **Context:** The 1000 rung moved to a test VM (ADR-1780, #1783), so the two rungs gated on two host classes, and a check of both gated every figure on neither. Each `run:perf` run held a GitHub runner for the better part of an hour, and a hosted runner's CPU model is not fixed, so a check there gated its times only when the runner happened to match the one the baseline came from.
- **Not chosen:**
  - **Keep the CI job for the 300 rung.** It costs Actions minutes on every labeled PR for a check whose host-dependent half gates by luck, and it keeps a second host class to regenerate on.
  - **A self-hosted runner on a test VM.** It would put a merge-adjacent job on infrastructure outside the repo, for a gate that is not required; running it by hand is the same measurement with no new trust boundary.
