---
title: "A PR records only the demos it tags, and carries its clip (#1180)"
date: 2026-10-25
refs: [1180, 1132]
---

`run:demo` now records exactly the demos a PR body tags in its Demo section (`demo: <id>`, or
`demo: none (<reason>)`), plus any tape or walkthrough the PR adds or changes, and nothing it did
not name ([ADR-1180](/architecture/decisions/#adr-1180-a-pr-records-only-the-demos-it-tags-carries-its-clip-in-its-body-and-main-re-records-every-demo-nightly)).
The parse and the selection are pure (`web/e2e/walkthroughpr.mjs` `demoTag` and `selectDemos`),
and `demo-tag.yml` lints the tag on every PR. Each selected demo records on a runner and a stack of
its own; one job combines them, and the publish job writes each demo's clip (GIF or animated
preview, and its scrubbable link) into the PR body between markers, keeping the comment as the full
record. The splice is an inline program of the workflow, played against fixtures by
`web/src/demo-workflow.test.ts`, which also holds the token boundary against the workflow file. A
nightly run on main records every demo and opens, updates or closes one issue. The terminal
recorder pulls its pinned base images while the stack builds and copies a pinned static `jq`
rather than installing one.
