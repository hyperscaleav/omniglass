---
title: "Two seed drivers are named for their product (#1483)"
date: 2026-11-15
refs: [1483, 1363]
---

`newtron-nvp` served only the Newtron Panel 7 once the Core 3 and Slate 5 moved to drivers of their
own, and `http-json` only the Tessellate Census, so they are renamed `newtron-panel-nvp` and
`tessellate-census-http`, matching `lyra-arc-snmp` and `newtron-core-nvp`. The products, the
simulated fleet's attach maps, the driver tests, the demo tapes and the docs that name them follow.

A driver's name is the seed's key and an endpoint holds it by uuid, so a migration
(`20261115120000_driver_product_names`) renames the existing rows in place: an install seeded before
the rename keeps its endpoints, their tasks and the products on the same driver, and the next boot's
seed updates it. `TestDriverRenameKeepsAttachedEndpointsCollecting` stands a pre-rename database,
runs the migration and the seed, and asserts the driver uuids, the attached endpoint and the
products survive. The choice of a migration over a seed alias is ADR-1483.
