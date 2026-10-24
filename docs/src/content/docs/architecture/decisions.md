---
title: Decision log
description: "The dated history of architectural calls: reversals, settled open questions, and where the build currently diverges from the present-tense design."
# The index and the entries are not in this file. Each decision is one file under
# docs/src/content/decisions/, spliced in where ::decision-index and ::decision-log sit
# by docs/src/plugins/remark-fragments.mjs. Record one with /record-decision; internal/docslint
# fails an entry or an index row written here.
---

The architecture pages are written in the present tense as the **target design**, and each carries a
status badge that says how much of it is built ([implementation status](/architecture/status/)). Neither
axis carries **history**: why a call was made, when it was reversed, or why the shipped code differs from
the page that describes it. That is what this log is for.

A page tells you what the design **is** and how much is built. This log tells you how it **got there**:
the decisions that bind the design, the ones that were reversed in the open, and the points where the
implementation has deliberately (or accidentally) drifted from the prose. It is the project's
architecture decision record (ADR), kept lightweight and append-only.

## How it works

- **One entry per decision** that reverses a prior call, settles an [open question](/architecture/status/),
  or records a point where the build diverges from a page's present-tense design.
- Each entry carries a **date**, a **status** (`Proposed`, `Accepted`, `Resolved`, or `Superseded`), the **decision**
  in one line, the **context** that forced it, and the **page(s)** it touches.
- A **divergence** entry is the partner of a page's inline note: the page says what is true *now*, this
  log says *why* and *when* it diverged, and which issue tracks closing the gap.
- Entries are **never edited away**. A reversed decision is marked `Superseded` and points at the entry
  that replaced it, so the trail of reasoning survives. Nothing in this log is deleted when a page
  changes.
- New reversals and divergences are added **per slice**, as part of the
  [ship gate](/contributing/slice-workflow/): if a slice changes a settled call or ships something that
  differs from its architecture page, the entry lands in the same PR.

This log was seeded on 2026-06-30 from the first architecture-drift review, which backfilled the entries
below from the project's history. From here it grows one slice at a time.

## Index

::decision-index

## Entries

::decision-log
