---
title: Contributing
description: "How Omniglass is built: the five doctrines, the slice workflow, and the pages that teach each part of it."
sidebar:
  order: 6
  badge:
    text: Built
    variant: success
---

Omniglass is public from its first commit and built one vertical slice per pull request. Work
starts as an approved GitHub issue, is built test-first in a worktree on its own branch, ships its
docs in the same PR, and merges through the queue. A change you want to make starts at the issue, not
the code.

## The five doctrines

1. **API first**: the Go API is the source of truth and everything else is generated from it
   ([API first](/contributing/api-first/)).
2. **Test first**: a behavior change carries a test that failed before it and passes after
   ([Test-driven](/contributing/test-driven/)).
3. **Docs with everything**: a feature is not done until the docs that teach it ship with it
   ([Docs with everything](/contributing/docs-with-everything/)).
4. **Functional and pedagogical**: operator surfaces also teach the concept they operate on
   ([Learning tool](/contributing/learning-tool/)).
5. **Primitive first**: build the reusable primitive, then consume it
   ([Primitive first](/contributing/primitive-first/)).

## Doing the work

- [Slice workflow](/contributing/slice-workflow/): define, build test-first, document, validate, review, ship.
- [Feature loops](/contributing/feature-loops/) and [autopilot](/contributing/autopilot/): a body of work
  as one PR, and the unattended daily slice.
- [Labels and the board](/contributing/labels/): the issue contract and how work is tracked.
- [Release and CI](/contributing/release-ci/): what a PR is checked by and how a release is cut.
- [Design system](/contributing/design-system/) and [demos](/contributing/demos/): the console's
  building blocks and the recording every PR carries.

## Your environment

- [The lab](/developer/lab/): a working Omniglass with a simulated fleet, on Docker.
- [PR previews](/developer/pr-previews/): a live copy of a pull request, opened by a label.

`make test-short` iterates and `make test` is the gate; `make preflight` runs what the PR's required
checks run.
