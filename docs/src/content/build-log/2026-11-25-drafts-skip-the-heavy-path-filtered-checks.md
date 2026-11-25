---
title: "Drafts skip the heavy path-filtered checks, and run:test opts one in (#2309, #2597)"
date: 2026-11-25
refs: [2309, 2597]
---

`docs-build`, `docs-shots` verify, `migrations` and the `goreleaser` snapshot guard their job on the draft flag
(reporting skipped), run on `ready_for_review`, and take `run:test` as a draft's opt-in. `run:test` returns from
retirement with that one task. The rest of #2597 (build once, the `changes` job, unit-only drafts, arm64 and
publish off PRs, review gated on tests, agent-sync merging main only when needed) was already on main.
