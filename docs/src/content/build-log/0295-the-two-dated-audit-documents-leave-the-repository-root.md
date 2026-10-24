---
title: "The two dated audit documents leave the repository root"
date: 2026-08-26
refs: []
form: bullet
---

- **The two dated audit documents leave the repository root**
  ([#497](https://github.com/hyperscaleav/omniglass/issues/497)). `AUDIT-2026-07-30.md` and
  `DRIFT-PLAN-2026-07-30.md` sat beside the README, outside every lint scan root, asserting
  whatever was true on 2026-07-30 with nothing to flag them, in a repository that is public
  from the first commit. Both are deleted: the audit's findings and the plan's 42 sub-issues
  are tracked in GitHub, which holds them authoritatively, and a dated write-up that has been
  fully converted into issues has no second life. The `docs-audit` skill keeps the six-section
  output shape it encoded and stops pointing at a file that is gone.
