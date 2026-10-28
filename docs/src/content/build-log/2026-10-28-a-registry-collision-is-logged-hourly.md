---
title: "A registry collision is logged when it is found, then hourly (#1298)"
date: 2026-10-28
refs: [1298, 1279]
---

A name in two catalogs, say a metric and a property both called `input`, resolves on no lane, so
every sample carrying it is refused. The ingest consumer warned about it on every batch that carried
samples: about 65 lines a second on the dev fleet at the lab's cadence, for as long as the collision
stood. Since #1279 the consumer keeps its registry until the catalogs change, which is the only time
a collision can appear or clear, so the warning now lives there. It is logged when the registry is
built with a collision and once an hour while it persists, and deleting either side stops it (these
catalogs cannot be renamed yet, #1188).
