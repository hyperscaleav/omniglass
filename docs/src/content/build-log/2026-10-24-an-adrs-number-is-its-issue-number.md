---
title: "An ADR's number is its issue number (#1108)"
date: 2026-10-24
refs: [1108]
---

A decision now takes the number of the issue it was decided under ([ADR-1108](/architecture/decisions/#adr-1108-an-adrs-number-is-the-number-of-the-issue-it-was-decided-under)), so two PRs
recording decisions at once can no longer race for the same next number. ADR-0001 to ADR-0163 stay a
closed legacy range, still checked for gaps; a number above it must be one of the decision's own
frontmatter `refs`, and a second decision on one issue takes a suffix from .2 (ADR-NNNN.2), which the
heading renders with its dot and the anchor without (`#adr-NNNN2-<slug>`). Both readers changed
together: `fragments.mjs` orders the index by value (legacy, then issue numbers, a suffix after its
issue, five-digit numbers last) and refuses a mismatched refs, a mis-padded number, a suffix of one, a
suffix on a legacy number, and two files holding one number by name; `TestDecisionsFormat` adds the
suffix runs (no .3 without .2, no .2 without the plain number) and refuses two decisions whose anchors
would share a prefix. `make adr-renumber` takes suffixed numbers, rewriting mentions and file names in
the dot form and anchors in the dotless one while leaving the plain number alone, and survives only for
two PRs adding one issue's next suffix. `/record-decision`, docs with everything, the slice workflow,
`/autopilot` and CLAUDE.md teach the rule, the decision log explains its two ranges, and
[ADR-0162](/architecture/decisions/#adr-0162-the-append-only-logs-are-directories-of-one-file-per-entry-spliced-into-their-pages)
carries the amendment. Proven test-first: issue-numbered decisions with and without matching refs, a
gap above the legacy range passing and one below failing, a suffix's heading and anchor through the
renderer and the post-build check, suffix and anchor clashes, and the renumber tool moving a suffix
and a plain number to a suffix over a fixture tree.
