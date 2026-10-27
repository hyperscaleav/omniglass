---
title: "The autopilot never arms auto-merge; the architect's merge is the approval"
date: 2026-10-27
status: Accepted
summary: "The autopilot skill's step that tried `gh pr merge --auto` is removed. Agent PRs are opened under the architect's own token, and GitHub never lets an author approve their own PR, so a required approving review could not be the gate; without one, a required check alone merged a PR the moment CI went green (#857). The architect's manual merge stays the approval, as ADR-0138 intended"
refs: [859, 857, 138]
---

- **Date:** 2026-10-27 | **Status:** Accepted | **Pages:** [autopilot](/contributing/autopilot/)
- **Decision:** (#859, the architect's ruling.) The autopilot never enables auto-merge and never
  merges. It opens the PR, watches CI and reviews, and reports; the architect's merge accepts the
  slice and a close vetoes it, as
  [ADR-0138](#adr-0138-the-autopilot-ships-a-daily-slice-and-pr-review-is-its-approval-gate) set out.
- **Context:** The skill's step armed auto-merge on the theory that a required approving review
  would make the architect's approval the last click. On a `main` with no review requirement it
  merged #857 at once. Requiring one approval does not fix it here: the agents open PRs with the
  architect's token, so the architect is the author of every PR and GitHub refuses an author's
  approval, which would block every PR rather than gate it. A separate agent identity (a GitHub App
  or machine account) would make the approve-then-auto-merge flow work and stays open as an option;
  until then a manual merge is the gate, and it costs nothing, since every PR is merged by hand
  already.
