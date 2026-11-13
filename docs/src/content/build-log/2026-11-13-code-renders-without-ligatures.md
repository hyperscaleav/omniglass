---
title: "Code renders without ligatures (#1013)"
date: 2026-11-13
refs: [1013]
---

Every code or expression rendering in the console now shows the characters the operator typed (`!=`, `==`, `>=`) instead of JetBrains Mono ligatures (≠, ═, ≥). This is fixed once in the stylesheet rather than site by site.

The fix adds `font-variant-ligatures: none` to the `.font-data, .mono, .tnum` rule in `app.css`, which applies to all monospace data text. This replaces redundant per-site utilities on rule expressions, calculation expressions, and expression editor inputs.

As a result, operators render as operators: unambiguous symbols that read correctly to screen readers and copy without ligature substitution.
