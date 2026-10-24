---
title: "The `/types` umbrella is retired"
date: 2026-07-22
refs: []
form: bullet
---

- **The `/types` umbrella is retired**
  ([ADR-0060](/architecture/decisions/#adr-0060-a-resource-is-one-kebab-case-noun-nesting-means-ownership)):
  the location type registry was addressed two ways, `/types/location` for its CRUD and
  `/location-types/{id}/properties` for its contract, so one entity had two command groups and an
  operator had to know both spellings. The registry CRUD moves to `GET/POST /location-types`,
  `PATCH/DELETE /location-types/{id}`, and the secret shape registry to `GET /secret-types`. The rule
  is now stateable in one line: a resource is one kebab-case noun, and nesting means ownership. The
  `type` command group is gone; `location-type list` is the registry and `location-type property list`
  its contract. Addressing only: same handlers, same permission gates, same scope injection, no schema
  change.
