---
title: "A registry fork locks before it reads, in a statement of its own"
date: 2026-08-13
refs: []
form: bullet
---

- **A registry fork locks before it reads, in a statement of its own**
  ([#709](https://github.com/hyperscaleav/omniglass/issues/709)). A fork writes the whole mutable row
  back rather than patching columns, so it gives up the lost-update immunity a column-wise `coalesce`
  write has: two operators editing different fields of one shipped row at once both compute an image
  from the same starting point, and the second discards the first's field with no error and no audit
  anomaly, both writes in the log. `UpdateComponentType` and `RestoreComponentType` took no lock at
  all, and `UpdateLocationType` and `RestoreLocationType` took one that did not work.

  That second half is the finding, and it was found by writing the test rather than by reading the
  code. [#703](https://github.com/hyperscaleav/omniglass/issues/703) closed this on the location path
  with `for update of lt` on the read that resolves the shadow, and the issue named that as the model
  to copy. Driven by two concurrent forks of one shipped row, `location_type` lost an edit anyway. At
  READ COMMITTED the statement's snapshot is taken when the statement begins, before it blocks, and a
  waiter released by a row that was locked rather than updated gets no EvalPlanQual recheck, so the
  left join it already evaluated still reports the shadow as its stale snapshot saw it. The lock
  serialised the two transactions without making the second read what the first wrote.

  Both registries now take the row's lock through one shared `lockRegistryRow`, in its own statement,
  before the resolving read; the read is then a new statement with a new snapshot taken after the lock
  is held. `ADR-0095`'s adopter contract grows a fourth fact saying so, since this is the half of the
  primitive an adopter cannot infer from the shadow's shape. The test is a paired-fork loop over both
  registries, and it has teeth in both directions: it fails within two rounds against either unlocked
  form, including the one that shipped as the fix.
