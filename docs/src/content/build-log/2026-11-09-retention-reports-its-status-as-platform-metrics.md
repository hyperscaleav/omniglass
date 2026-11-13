---
title: "Retention and compaction report their status as platform metrics (#1629)"
date: 2026-11-09
refs: [1629, 1094, 1625, 1627, 1671]
---

Each retention pass now lands its figures as platform metrics written by the process: per lane, keyed
on the lane's own name so override and log band lanes report with no change, the rows it removed and
whether it stopped behind; a behind flag per compaction and the metric window; the pass's duration,
failure count and partitions created and dropped; how far the trend and block marks trail now; and,
from a new `RetentionStatus` gateway read after the prunes, the partition count, the oldest metric
day's age, each stored table's bytes and the database's. Thirteen keys join the platform writer class
and the seed, so no node, push or operator can write them, and a seeded `retention-behind` rule opens
a warning alarm naming a lane held behind for 5 minutes and clears it on the first pass that catches
up. The storage page says how to read behind against failed. The integration test drives a capped
pass over rows past the window against real Postgres through the alarm opening and clearing. The
figures land on a component carried in `bus.Config`, which production leaves empty until the server
has its platform component (#1627); wiring it at boot is #1671
([ADR-1629](/architecture/decisions/#adr-1629-a-retention-pass-reports-on-the-servers-platform-component-behind-a-seam-until-the-server-has-one)).
