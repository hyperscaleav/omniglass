---
title: "Ingest lands on a lane per partition of the node's name (#1896)"
date: 2026-11-14
refs: [1896, 311, 1623]
---

On the corporate 3,000 lab the one serial telemetry durable landed 37 to 41 batches a second against
about 50 arriving, so the backlog grew for the whole run and every lag figure read the queue. The
stream now files each node's batches under a partition of its name with its own `partition` subject
transform, and each partition has a durable with one batch in flight, so the lanes land side by side
while a node's batches still land in the order it sent them. The lane count is
`OMNIGLASS_INGEST_PARTITIONS`, 4 by default, refused outside 1 to 64 and held under the database pool
less one; the server logs the count it runs and why, drains and deletes a durable a smaller count left,
and keeps the push subject on `og-telemetry-worker`. Each lane logs its own tally, `ingest-lane-busy`
reads the busiest, the backlog gauges sum the durables, and the ack floor is read off the stream's
first held batch. The scale lab reports a busy row per lane and waits, at each mark, for every live
node to land a sample probed after it, since the newest sample anywhere no longer proves the rest
landed. Bus tests cover the partition hash agreeing with the stream's transform, a node's batches
landing in order on one lane, the lanes landing side by side, and a smaller count draining the
durables it left; the drain test failed against the old query with two live nodes. The decision is
ADR-1896, amending ADR-0311 point 3.
