---
title: "An approved definition rolls up to one PR; slices cascade on an integration branch"
date: 2026-08-01
status: "Accepted"
summary: "loop-executed work rolls up to one PR per approved definition; slices cascade through per-slice gates on an integration branch"
---

- **Date:** 2026-08-01 | **Status:** Accepted | **Pages:** [slice workflow](/contributing/slice-workflow/), [feature loops](/contributing/feature-loops/)
- **Decision:** For loop-executed work (a body of slices defined and approved as one Epic or Feature issue), the
  PR granularity moves from one-slice-per-PR to **one-PR-per-approved-definition**: sub-issue slices are built
  test-first on cascade branches (or serial commits) merged into one integration branch, each passing the
  per-slice gates before merging inward, and the definition ships as a single rollup PR whose ship-review covers
  the whole diff. The slice lifecycle inside each sub-issue is unchanged, and merge to `main` remains the
  architect's call. Hand-driven single slices keep the original one-slice-per-PR shape.
- **Context:** Long agent loops ship several related slices per body of work. Per-slice PRs would put the
  architect back in the approval loop once per slice (the touchpoint cost the loop exists to remove), while
  letting unreviewed slices land on `main` would gut the ship gate. The cascade keeps every per-slice gate and
  gives the architect one reviewable boundary: approve the prose definition at the front, merge one rollup PR at
  the end. The [feature loops](/contributing/feature-loops/) page is the contract; built for the
  [AI-driven feature loops epic](https://github.com/hyperscaleav/omniglass/issues/488).
