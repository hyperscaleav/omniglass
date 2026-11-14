---
title: "Ingest lands on a lane per partition of the node's name, in order per node"
date: 2026-11-14
status: "Accepted"
summary: "The telemetry stream files each node's batches under a partition of its name (FNV-1a modulo K, the stream's own subject transform), and each partition has a durable of its own with one batch in flight, so the lanes land side by side while a node's batches still land in the order it sent them. K is OMNIGLASS_INGEST_PARTITIONS, 4 by default, 1 to 64 and held under the database pool less one. The order ADR-0311 point 3 holds is per node, not across the fleet. The lab's drain reads every live node's newest sample rather than the newest sample anywhere"
refs: [1896, 311, 1585, 1623]
---

- **Date:** 2026-11-14 | **Status:** Accepted | **Pages:** [messaging](/architecture/messaging/#streams-and-consumers), [workers](/architecture/workers/)
- **Decision:** (#1896, amending [ADR-0311](#adr-0311-a-telemetry-batch-lands-once-in-one-transaction-under-its-receipt) point 3.)
  1. **A lane per partition.** `OG_TELEMETRY` stores a node's batch published on
     `og.v1.telemetry.<node>` as `og.v1.telemetry.<p>.<node>`, where `p` is the stream's
     `partition(K)` transform of the node's name (FNV-1a modulo K). Each partition `p` has a durable,
     `og-telemetry-worker-<p>`, filtered to its subjects, with `MaxAckPending` 1. The durable
     `og-telemetry-worker` keeps the API push subject and any batch stored under a node's own subject
     before the stream filed by partition. The receipt keeps the subject the node published on, so a
     batch's identity does not depend on K.
  2. **Order per node.** A node's batches share a partition, and its durable holds one in flight, so
     they land in the order the node sent them, a transient failure redelivered before anything behind
     it, exactly as ADR-0311 point 3 requires of a publisher. Two nodes' batches on different lanes
     land in no fixed order with one another, which the transition guard never needed: a series has
     one owner node.
  3. **K is configured, with a default.** `OMNIGLASS_INGEST_PARTITIONS` (4) is read at boot, refused
     outside 1 to 64, and held under the database pool less one connection, since each lane holds a
     connection while it lands and more lanes would only queue on the pool. The server logs the count
     it runs and why. A durable past a smaller K is drained and then deleted. Changing K moves nodes
     between lanes, so it is changed with the backlog drained.
  4. **The lanes report as one and each alone.** Each lane keeps its own busy tally; `ingest-lane-busy`
     is the busiest lane's share of the window, since ingest keeps up only while every lane does. The
     backlog gauges sum over the durables, and the ack floor is the sequence below the stream's first
     held batch, because the lanes ack out of order with one another.
  5. **The lab's drain is per live node.** The harness waited for the newest sample anywhere to pass a
     mark, which proved every batch behind it landed only while one lane held them all. It now waits
     until every node that checked in after the mark and collects at least one series has landed a
     sample it probed after the mark.
- **Consequences:** Ingest throughput scales with K up to the database's write capacity rather than
  being one landing's latency for the whole fleet. Per-node order survives a later horizontal split:
  servers that share the durables share the partitions, and a work queue gives each batch to one of
  them. A node whose batches land slowly (a large batch, a failing write) slows only the nodes that
  share its lane.
- **Context:** On the corporate 3,000 lab one serial durable landed 37 to 41 batches a second against
  about 50 arriving, so the backlog grew without bound and every lag and loss figure behind it read
  the queue, not the platform (#1896). ADR-0311 named the way out: "a concurrency pool would need
  sequencing per publishing subject to keep the same order". Partitioning by the node's name is that
  sequencing, held by the stream rather than by the server.
- **Not chosen:**
  - **A worker pool behind one durable with per-subject sequencing in the server.** It keeps order in
    process memory, so a second server would break it, and a redelivery would have to re-enter the
    sequence by hand.
  - **A durable per node.** Thousands of durables, each with its own state on the stream, for an order
    a few partitions already give.
  - **Cheaper batches alone.** They raise one lane's ceiling but keep throughput tied to one landing's
    latency; the cheaper landing ships alongside (#1928) rather than instead.
