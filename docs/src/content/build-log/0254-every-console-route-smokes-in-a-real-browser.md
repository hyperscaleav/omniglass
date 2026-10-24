---
title: "Every console route smokes in a real browser"
date: 2026-08-14
refs: []
form: bullet
---

- **Every console route smokes in a real browser.** The router now renders from a route manifest
  (`web/src/lib/routemanifest.ts`, pure data; `web/src/routes.tsx` resolves each entry to its page),
  and the e2e smoke spec (`web/e2e/routes.spec.ts`) drives the same array: one browser test per
  route, failing on any page error or non-resource console error, with parametrized routes smoking
  their honest miss face through a well-formed-unknown uuid and the not-found face smoked like any
  other route. A guard suite pins the construction (every entry resolves to a registered page, the
  stub entries equal the nav's own lists, every entry carries a concrete smoke address), so a route
  cannot be added without the browser tier rendering it (#758, #760).
