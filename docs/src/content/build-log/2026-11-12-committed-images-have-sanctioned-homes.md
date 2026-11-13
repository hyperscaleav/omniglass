---
title: "Committed images have sanctioned homes (#922)"
date: 2026-11-12
refs: [922, 1204]
---

The eleven PNGs under `.prshots/`, PR evidence committed before `.github/screenshots/` was the home,
are removed from the tree. A guard test in `internal/build` now walks `git ls-files` and refuses a
committed image outside the six sanctioned homes, which
[docs with everything](/contributing/docs-with-everything/#where-committed-images-live) lists.
