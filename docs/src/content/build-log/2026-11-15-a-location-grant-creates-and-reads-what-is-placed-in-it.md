---
title: "A location grant creates and reads the systems and components placed in it (#1498)"
date: 2026-11-15
refs: [1498, 10, 714]
---

A field tech holding one `deploy` grant at a location could build out its rooms but not the systems
and components in them, not even to read one, so building a room out took a location, a system and a
component grant together. Now the one location grant reaches `system` and `component` for read and
create: `scope.Resolve` carries the location as a cross-tier root, and the Storage Gateway admits the
rows placed under it (a component through its own location or, with none, its primary system's).
A root create at a room in scope is a 201, at a room outside it the placement bind's non-disclosing
422, and with no location a 403. Edit, rename, move and delete stay on their own tier (#10).
`internal/api/location_grant_reach_e2e_test.go` failed before the change (4 of its 5 tests) and passes
after; `placement_scope_e2e_test.go` drops its three-grant workaround for the single location grant,
and the deploy role's description is rewritten from the reach matrix in
`internal/seed/role_reach_test.go`. The decision is ADR-1498.
