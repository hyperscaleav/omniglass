---
title: "The telemetry spool tells its drops before drain returns (#1285)"
date: 2026-11-14
refs: [1285]
---

The spool's overflow notice was logged after the shipper stopped, and stopping is what wakes `drain()`,
so a node that drains on shutdown and exits could finish before the warning was written, and
`TestSpoolDropsABatchTheBusCanNeverTake` flaked on the same ordering. The shipper now takes the
count and logs it with the lock released before it stops; a batch queued during the log is shipped by
the next pass. Drain semantics are unchanged. A unit test holds the notice in a slow log sink and
fails if `drain()` returns first (red on the old ordering), and the integration test no longer
polls for the line.
