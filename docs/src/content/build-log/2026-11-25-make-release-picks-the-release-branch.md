---
title: "make release picks the next release branch from the commits (#2711)"
date: 2026-11-25
refs: [2711, 2139]
---

`make release` reads the conventional commits on `main` since the newest release line branched, previews the
version, the commits that drove it and the release notes, and asks before creating `release/<major>.<minor>`.
Before 1.0 a breaking change bumps the minor. A patch creates nothing and lists the fixes to cherry-pick.
