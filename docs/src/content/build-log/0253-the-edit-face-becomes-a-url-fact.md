---
title: "The edit face becomes a URL fact"
date: 2026-08-14
refs: []
form: bullet
---

- **The edit face becomes a URL fact.** `?edit=1` beside an inventory detail address (or the users
  blade's `?u=<id>&edit=1`) lands the edit face directly, behind the same `<resource>:update` the
  footer Edit is behind; leaving edit strips the param, and the create-as-route and row-pencil
  handoffs carry the mode in the URL instead of the retired one-shot signals (`pendingedit`,
  `openPrincipalInEdit`). One hook (`web/src/lib/editurl.ts`) owns both directions
  ([ADR-0120](/architecture/decisions/#adr-0120-the-edit-face-is-a-url-fact)); the name-to-uuid
  redirect keeps its query string. Proven by page tests on the deep link, the permission fallback,
  the Cancel strip and the redirect, and at the e2e tier by the create handoff's own URL; the
  operator guide's edit-face screenshot is declared from frontmatter with no capture-side clicks,
  which is the pipeline this unlocks (#758, #759).
