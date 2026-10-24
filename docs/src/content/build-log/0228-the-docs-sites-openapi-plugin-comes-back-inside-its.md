---
title: "The docs site's OpenAPI plugin comes back inside its declared support range"
date: 2026-08-13
refs: []
form: bullet
---

- **The docs site's OpenAPI plugin comes back inside its declared support range**
  ([#704](https://github.com/hyperscaleav/omniglass/issues/704)). `starlight-openapi@0.26.0` declares
  `astro >=7.0.2` and `@astrojs/starlight >=0.41.0`; the site runs astro 6.4.6 and starlight 0.38.5.
  pnpm warns where npm refuses, so it installed and built anyway, and nothing on the 331 pages we
  publish today was broken by it. Running a plugin outside its support range is still a latent
  failure with a misleading shape: the first page to hit a starlight API that only exists in 0.41+
  would read as a content bug.

  What 0.25 gives up is worth stating exactly, because the ruling asked and the answer is checkable
  rather than assumed: **nothing**. 0.26.0's changelog carries one entry, "Adds support for Astro v7,
  drops support for Astro v6", and that is the whole release. It is not that 0.26's features are
  unused here, it is that 0.26 explicitly dropped the Astro major this project runs. `^0.25.0`
  resolves to 0.25.3, which carries every feature of the 0.25 line (operation snippets, request and
  response snippets, the overview operation list, on-demand rendering) plus its three fixes, and its
  peers (`astro >=6.0.0`, `@astrojs/starlight >=0.38.0`, `@astrojs/markdown-remark >=7.0.0`) are
  satisfied exactly. Moving to astro 7 remains its own slice whenever it is wanted; it is not owed to
  this plugin.
