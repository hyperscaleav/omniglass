---
title: "A shipped driver is named for its product, and a rename moves the row"
date: 2026-11-15
status: "Accepted"
summary: "a shared driver exists only for one product in different configurations; `newtron-nvp` and `http-json` rename to `newtron-panel-nvp` and `tessellate-census-http`, carried to existing installs by a migration that renames the driver row in place"
refs: [1483, 1363]
---

- **Date:** 2026-11-15 | **Status:** Accepted | **Pages:**
  [collection](/architecture/collection/), [drivers guide](/guides/admin/drivers/)
- **Decision:** a shipped driver is named for the product it serves (the ruling on #1363); a
  shared driver exists only for the same product in different configurations. The two seed
  drivers that broke the rule once their siblings moved to drivers of their own are renamed:
  `newtron-nvp` to `newtron-panel-nvp` (now the Newtron Panel 7's alone) and `http-json` to
  `tessellate-census-http` (the Tessellate Census's alone). An install upgrading across the
  rename keeps collecting because a dbmate migration renames the existing `og` driver rows in
  place; no seed alias is added.
- **Context:** the driver's name is the boot seed's upsert key, and an endpoint and a product hold
  the driver by uuid. Left to the seed alone, the new name would insert a second driver row and
  strand every attached endpoint on the old one, which no boot would update again. Renaming the
  row keeps its uuid, so endpoints, products and their derived tasks stay attached and the next
  seed updates the row in place. An alias would have kept the dead name resolvable in the seed
  forever for data that, before the first external deployment, does not exist outside dev, CI and
  the lab. The migration is a one-time backfill: it is a no-op on a fresh database and where the
  new name is already taken. Only the two drivers are renamed; the simulator's `newtron-nvp` API
  surface is a protocol surface shared by three Newtron devices and keeps its name.
