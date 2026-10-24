---
title: "The zoom face becomes the default"
date: 2026-08-26
refs: []
form: bullet
---

- **The zoom face becomes the default** (ADR-0129, stage 1 of the reconciliation ruled on
  the #795 review). The identity routes render the fleet faces without any param;
  `?zoom=1` is tolerated for old links and never written; every in-app address is bare;
  the classic detail face survives at `?view=detail` (and under legacy `?edit=1`) until
  edit-in-blade lands. `create` stays a reserved segment. The remaining stages (one
  entity blade per kind, edit-in-blade, operate absorption, classic retirement) are the
  follow-up epic.
