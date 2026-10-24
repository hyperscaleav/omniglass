---
title: "A name is the address"
date: 2026-07-21
refs: []
form: bullet
---

- **A name is the address** ([ADR-0053](/architecture/decisions/#adr-0053-a-name-is-the-address-a-uuid-is-identity)):
  the API accepted names on write and returned uuids on read, so a component created with
  `{"parent": "rack"}` read back as `{"parent_id": "0198f..."}` and no response body could be replayed
  as the request that produced it. Every client had to fetch a second collection and join by uuid to
  render one label. Normalized on component, system, and location (`parent`, `location`) and on the tag,
  variable, and secret bodies, which already carried `owner_name` beside the redundant id. Enforced by
  `TestResponsesAddressEntitiesByName`, a guard over the generated OpenAPI that fails on any field
  naming another entity by uuid, and proven by an HTTP round-trip test on all three entities. **Breaking**
  at v0.0.0, deliberately. The schema stragglers (`secret`, `variable`, `tag_binding` owner arcs) keep
  their uuid keys for now and are called out in the ADR.
