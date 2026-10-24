---
title: "The declared-property contract and value bodies carry `property_id`"
date: 2026-07-23
refs: []
form: bullet
---

- **The declared-property contract and value bodies carry `property_id`** beside `property_name`, the last
  name-only registry references: the product / standard / location_type contract lines, the
  component / system / location value bodies, and the effective-property resolution. `property_name` joins
  the guard's reverse set, so a contract or value body can no longer name a property without its uuid. The
  **event body carries `property_id` beside its `key`** too (`key` is the datapoint-key vocabulary for the
  property name on the telemetry bodies): the event row already stored `property_id` exactly as metric and
  state do, so exposing it is just a projection. The guard accepts `key` as property_id's name pair on the
  telemetry body; `key` stays forward-covered rather than joining the reverse set, since it is also a
  tag-binding field and so ambiguous. Every registry reference in the API now carries both forms.
