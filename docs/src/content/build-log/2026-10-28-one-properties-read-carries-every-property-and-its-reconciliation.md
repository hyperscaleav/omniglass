---
title: "One properties read carries every property and its reconciliation (#1297)"
date: 2026-10-28
refs: [1297]
---

`GET /components/{name}/properties` and `GET /systems/{name}/properties` now list every property the
owner has, declared, calculated, observed or told, and every row but a calculated one carries its
`reconciliation`: want, told, is (worst case across every live instance, naming `is_instance`) and
drift. The storage `Reconciliation` resolver folded into `EffectiveProperties`, which gained the
observed and told keys (one catalog probe per key on the owner's series and command indexes, a retired
observer's frozen reading left out, as before) and reads each key's series with the arc it already
resolved instead of re-resolving the owner per key. A key that is only observed was invisible before;
it now reads `want: null`, its `is`, and `drift: false`. `GET /components/{name}/reconciliation` and
`omniglass component reconciliation list` are removed
([ADR-1297](/architecture/decisions/#adr-1297-the-properties-read-carries-reconciliation-and-the-reconciliation-route-retires)).
The console's Reconciliation panel reads the same cached rows as the Properties panel, so the two
never disagree; it now sits on the system view too, and a key only observed or told stands there and
not among the declared properties. The `reconcile-instances` demo moved to the properties read and
gained the observed-only beat. Proven by `TestPropertiesCarryEveryKeyAndItsReconciliation` (storage:
observed-only, told-only, retired observer, a place, out of scope), `TestPropertiesCarryReconciliationAPI`
(HTTP: drift naming the instance, observed-only, a system's block, the 404 and 403, the retired route's
404), and the panel tests.
