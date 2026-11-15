---
title: "Code renders without ligatures (#1013)"
date: 2026-11-13
refs: [1013, 1000]
---

Every code and expression rendering in the console now shows the characters the operator typed
(`!=`, `==`, `>=`) instead of JetBrains Mono's ligatures (≠, one long bar, ≥). A rule with the fire
expression `value == "down" && instance != "aux"` read on `/rules` as a bar and a not-equals sign,
glyphs no keyboard has. The `.font-data` rule in `app.css` now sets `font-variant-ligatures: none`
once, so every surface that renders code inherits it, and the per-site opt-outs the expression
editor work (#1000) had added (the editor's CodeMirror theme, its token and caret rows, the
calculation rule's reduce expression and input aliases) are gone. A Rules page test loads the real
stylesheet and asserts the criteria's computed style, so a later edit that drops the rule fails.
