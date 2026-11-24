---
title: "A location health read names each system uniquely (#1775)"
date: 2026-11-23
refs: [1775, 961]
---

The systems drill-down in `GET /locations/{name}/health` carried only a short name and a verdict,
so a building with many rooms each holding a system called `meeting` read as indistinguishable
entries, and `GET /systems/meeting` did not open any of them. Each entry now also carries `id`,
`path` (the unique dotted address `GET /systems/{name}` resolves), `label` and `location` (the
dotted path of the room it is placed at). Regression test:
`TestLocationHealthSystemsAreAddressable` in `internal/storage`. The console reads only the
headline verdict of this read, so no surface needed a link change.
