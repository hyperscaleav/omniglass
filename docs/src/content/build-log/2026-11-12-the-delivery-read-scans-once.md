---
title: "The delivery read scans the receipts once (#1841)"
date: 2026-11-12
refs: [1841, 1623]
---

The 72-hour corporate 3,000 soak stalled its own ingest within hours: `lab measure`'s delivery read
found each boot's first receipt in the window and its lower neighbor with correlated subqueries, which
Postgres ran once per receipt, so the read grew with the square of the receipts table and ran for hours.
A measure the timer killed left its query running, the next sample started another, and the stacked
scans starved the consumer the soak was measuring. The read is now window functions over a single scan,
with the same rows out: on the soak's 492,000 receipts it returns in under three seconds. Every harness
query also carries a `statement_timeout` taken from its context (capped at ten minutes) and its own
application name, and a cancelled context closes the read and cancels its backend, so an interrupted
measure leaves nothing running in the lab it measured. No index was added: the rewrite costs ingest
nothing.
