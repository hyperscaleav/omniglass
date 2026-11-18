---
title: "The docs site runs on Astro 7 and Starlight 0.43 (#2154)"
date: 2026-11-18
refs: [2154, 2153]
---

The docs platform moves to Astro 7, Starlight 0.43, MDX 8, starlight-openapi 0.27 and astro-d2 0.14,
the first step of the docs rebuild (#2153), with no content change. Astro 7 defaults to its Sätteri
Markdown pipeline, so the config names the unified processor explicitly and the site's remark
plugins (fragments, screenshots, walkthroughs, the design fence) run there unchanged; MDX inherits
it. Two defaults are pinned back to keep the output as it was: `compressHTML: true` for whitespace,
and `scopedStyleStrategy: 'where'` so component styles keep the specificity `custom.css` was written
against. Every post-build check passes and the pages render pixel-identical, apart from code-block
token colors on the generated API reference.
