---
title: "A group name obeys the entity name rule, tightening a pattern the code had excused"
date: 2026-08-04
status: "Accepted"
summary: "principal_group.name moves to the entity name rule, retiring the looser API-layer pattern"
---

- **Date:** 2026-08-04 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** `principal_group.name` obeys the ordinary entity name rule (`^[a-z0-9][a-z0-9-]*$`, 100
  characters, a uuid refused), enforced by the Storage Gateway on create and rename and carried in the OpenAPI
  contract as a `pattern`. The previous rule (`^[a-z0-9][a-z0-9._-]*$`, 200 characters, checked only by the
  request schema) is retired.
- **Context:** this reverses a position that was written down in code. `KeyProvedElsewhere` excused
  `principal_group` from the behavioural validation sweep with the reason that tightening it "is a behaviour
  change for existing groups, tracked with the rename work". This is that work, so the excuse is spent. The
  looser pattern admitted `.` and `_`, the two characters the address grammar reads as separators, and the
  gateway itself validated nothing at all, so a group could be created with a uuid-shaped name through any
  caller that was not the HTTP route. There are no releases and no operator data, so the tightening costs
  nothing now and would cost a migration later. Shipped by [#567](https://github.com/hyperscaleav/omniglass/issues/567) in the [#545](https://github.com/hyperscaleav/omniglass/issues/545) loop.
