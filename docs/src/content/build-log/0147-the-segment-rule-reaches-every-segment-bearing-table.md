---
title: "The segment rule reaches every segment-bearing table"
date: 2026-08-04
refs: [552]
form: bullet
---

- **The segment rule reaches every segment-bearing table** (#552). `ValidateEntityName` guarded
  `component`, `system`, and `location` and nothing else, so seven registries accepted whatever they
  were handed. The rule now runs on `location_type`, `standard`, `vendor`, `driver`, `capability`,
  `product`, and `node`, and its vocabulary moved to the settled word: `ValidateSegment`,
  `ErrInvalidSegment`, `ErrSegmentIsUUID`, in `segment.go`. It also moved into the contract, where
  API-first wants it: the create bodies carry `pattern` and `maxLength`, so the generated spec,
  client, CLI, and JSONSchema enforce it too. The rule itself did not change and every seeded row
  already satisfied it.

  An adversarial review pass caught what the first attempt shipped. Five registries route their
  errors through one shared `mapTypeErr`, which never learned the new sentinels, so an operator typo
  returned **500** on `/vendors`, `/drivers`, `/capabilities`, `/standards`, and `/location-types`;
  only the node path had been taught. `product` had been missed outright, and a uuid-named product is
  unreachable by its own handle from the moment it is written, because `registryRefCol` routes a
  uuid-shaped reference to `where id = $1`. Two guards were added rather than two fixes: an API-tier
  test drives every affected route with nine illegal segments and asserts the operator receives a
  422, and `TestEveryNamedTableIsClassified` reads the generated schema so a new named table fails
  the build until somebody classifies it as a segment or a keyspace key. The API-tier test then found
  a **pre-existing** 500 the review had not: `ErrSegmentIsUUID` was mapped only in the advisory
  `:check-name` verb, never in the create mappers, so a pasted uuid 500'd on `/components`,
  `/systems`, and `/locations` before this slice existed.

  Two gaps are recorded rather than closed. No registry patch carries a name field, so the renameable
  handles ADR-0062 describes are unreachable through any update path ([#555](https://github.com/hyperscaleav/omniglass/issues/555)).
  And `principal_group` is guarded by a looser pattern (`^[a-z0-9][a-z0-9._-]*$`) that admits `.` and
  `_`, which the address grammar would read as token separators.
