---
title: "The docs corpus restructures around the design fence ([#434](https://github.com/hyperscaleav/omniglass/issues/434))"
date: 2026-08-02
refs: [434]
form: bullet
---

- **The docs corpus restructures around the design fence ([#434](https://github.com/hyperscaleav/omniglass/issues/434)).**
  A `:::design` container marks unbuilt prose structurally: it renders as a dashed
  target-design aside, must name the issue or ADR that tracks its gap (an unowned fence
  fails the build), and is machine-readable (`internal/docslint`'s `Regions`). The thirty
  hand-written "Still Design" summaries across fourteen pages migrated onto fences at the
  sections they described; the cascade and the two-lane data plane each consolidated to
  one home (cascade.md, messaging.md); the six entirely-unbuilt pages moved to a Design
  sketches sidebar group with page-spanning fences; this build log split off status.mdx
  (which dropped from 2,300 lines to 80); the glossary reconciled term-by-term against the
  generated schema; and a badge-fence lint now fails `make test` when any architecture
  page's badge disagrees with its fence census, so the badge is derivable, not asserted.
