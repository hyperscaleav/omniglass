---
title: "Walkthroughs as PR evidence: the run:walkthrough comment and the gates (#884, slice 4: #925)"
date: 2026-10-19
refs: [884, 925]
---

Applying the `run:walkthrough` label runs `walkthrough.yml`: it records the PR's walkthroughs on the
walkthrough stack (every one, or just the scripts a PR that changes nothing else under `web/` adds or
changes), exactly as `make walkthroughs` does locally (the simulator's `NET_ADMIN` is a container
capability, which a hosted runner grants as it already does for `make test-netadmin`), uploads the mp4s as
a workflow artifact, replaces the PR's stills on its branch with each passing walkthrough's poster and
per-step stills under `.github/screenshots/<pr>/walkthroughs/<id>/`, and upserts one PR comment, found
again by its marker and its bot author as the preview comment now is too, embedding them by commit SHA
and naming any walkthrough that failed (a failure also fails the job; a stack that never came up is
reported as that). The choices it makes (which walkthroughs, where stills go, the SHA-pinned URLs, the
comment body, with script text escaped onto one line) are a dependency-free module
(`web/e2e/walkthroughpr.mjs`) unit-tested in `make test-web`, and `walkthroughs.sh record` now takes
several ids, lists the failed ones in `out/failed.txt` and marks a stack that came up with `out/stack-up`.
It cannot loop: it runs only on `labeled`, and GitHub starts no workflow for the workflow token's push.
Fork PRs are skipped, checkout persists no credential, and only the push step holds the write token.
`/ship-slice` gained the gate the architect ruled (a PR that adds or changes an operator flow is not
`ready` without the walkthrough comment), `/assess-ui` grades the per-step stills with a walkthrough
rubric (the agent cannot watch video), and the PR template's Visual confirmation carries a Walkthrough
line. The workflow's run against a real PR is proven on this epic's rollup PR, not by a local test.
