---
title: "Two console papercuts from the identity work close"
date: 2026-08-09
refs: []
form: bullet
---

- **Two console papercuts from the identity work close**
  ([#644](https://github.com/hyperscaleav/omniglass/issues/644),
  [#646](https://github.com/hyperscaleav/omniglass/issues/646)). The name-availability precheck on
  the component and location edit forms passed the row's parent and location as NAMES, which stopped
  being unique estate-wide when names scoped to placement. With a duplicate-named parent selected the
  request could not say which one was meant, and the `catch` swallowed the error into operator
  silence: no "taken", no "free", no explanation. Both rows already carry the uuid and `:checkName`
  dual-accepts one ([ADR-0062](/architecture/decisions/)), so the precheck now addresses placement the
  way every other path on those pages already did. Separately, an operator bounced off a filtered
  deep link by a session expiry lost their filter: `AuthGuard` captured `pathname + search` but
  `Login` resolved only the pathname, so `/components?system=<uuid>` came back as the unscoped list.
  The capture and the resolve were inverse operations living in two files that disagreed, so they now
  live in one module (`web/src/lib/next.ts`) where a change to either is visibly a change to the pair,
  and the origin check that blocks an open redirect sits with them.
