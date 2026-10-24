---
title: "A dotted address resolves to a uuid, beside the existing uuid-or-name dual accept"
date: 2026-08-08
refs: []
form: bullet
---

- **A dotted address resolves to a uuid, beside the existing uuid-or-name dual accept**
  ([#627](https://github.com/hyperscaleav/omniglass/issues/627) Task 12, `72147c1`, `87e170d`,
  `3773fe1`, `e09e7b0`). `boi.17c.415a.$comp.display-1` parses to a location-tree walk plus a plane
  switch (`$comp` / `$sys`, `$role` reserved but unresolved) plus a plane-local descent, every segment
  validated against the entity name rule before it reaches a query; a percent-encoded slash arrives at
  the handler already decoded, closing the smuggling path a preflight probe had found. A registry
  whose names are a single global token (`node`, `tag`, the lane types) refuses a dotted ref up front
  rather than 404ing silently (`ec12bf2`). A review pass found the resolved address's not-found case
  reaching a create's body-reference field as the wrong status (a `404` naming the entity being
  `PATCH`ed rather than the missing reference); fixed by folding it to the entity's own sentinel one
  hop upstream of the generic path-param mapper, closing the same defect shape at every other
  body-reference site for free (`a71a549`). The route census was hand-derived twice and wrong both
  times before the third count (82 operations, 57 tree-addressable, 25 single-token) was verified
  against the registration functions rather than a literal-string grep: the tag-binding routes are
  built by string concatenation, invisible to grep, the second time on this branch a route-builder
  helper defeated a coverage sweep the same way.
  [ADR-0089](/architecture/decisions/#adr-0089-a-uuid-is-the-address-a-dotted-path-is-a-positional-lookup)
  records the address grammar and its known edges; this slice is what makes it real.
