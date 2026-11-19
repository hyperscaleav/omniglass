---
title: "Compatibility rule replaces the pre-deployment stance (#2186)"
date: 2026-11-18
refs: [2186, 2145]
---

CLAUDE.md's "Pre-deployment stance" section told agents that breaking changes were free
because Omniglass had no external deployment or installed base. The public demo instance
(#2145) and the 0.1.0 release end that period. The section is replaced by a "Compatibility"
rule that tells agents to signal every breaking change in the PR footer and the relevant
docs page, prefer a smooth upgrade path over a hard cut, and keep migrations forward-only.
A docslint check (`ScanClaudeMd`) now fails if the retired heading reappears in CLAUDE.md,
so the stance cannot be re-introduced without a test failure.
