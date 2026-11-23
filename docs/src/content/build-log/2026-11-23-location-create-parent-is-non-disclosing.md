---
title: "A location create's parent no longer discloses a location outside read (#1343)"
date: 2026-11-23
refs: [1343, 627, 1328]
---

`POST /locations` resolved its parent under the caller's `location:create` scope alone, so an
existing parent outside it answered 403 and a missing one 422: any create holder could probe
location names it could not read. The create, its batch and the template plan now resolve the
parent through `resolveCreateParent`, which asks the read question only to choose the refusal: a
parent outside `location:read`, or nowhere, is the same `404 parent location not found`, and a
readable parent outside the create scope stays 403
([ADR-1343](/architecture/decisions/#adr-1343-a-location-create-resolves-its-parent-under-read-before-create)).
Tests: the three cases in `TestLocationScopeCRUD` (`internal/storage`) and the byte-identical
bodies in `TestALocationCreateParentOutsideReadIsNotDisclosed` (`internal/api`).
