---
title: "A driver rename fails in its own PR (#1999)"
date: 2026-11-15
refs: [1999, 1996, 1951, 1959]
---

#1951 committed the measured driver cost table and #1959 renamed two seed drivers for their products,
each green alone. Together they left the table naming `http-json` and `newtron-nvp`, drivers that no
longer exist, and main went red until #1996 moved the rows. The table is a measurement nothing
regenerates, so nothing tied its names to the seed. `TestTheCommittedTableNamesOnlyShippedDrivers` now
fails on any row or not-measured entry naming no shipped driver, so the next rename has to move its row
in the same PR. It failed against the table before #1996 and passes after.
